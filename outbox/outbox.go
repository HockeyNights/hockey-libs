package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Message struct {
	ID       uuid.UUID
	Kind     string
	Payload  []byte
	Attempts int16

	MaxAttempts int
}

const defaultMaxAttempts = 5

func Enqueue(ctx context.Context, db DB, kind string, payload []byte, maxAttempts int16) (uuid.UUID, error) {
	if kind == "" {
		return uuid.Nil, errors.New("outbox: kind is required")
	}
	if len(payload) == 0 {
		return uuid.Nil, errors.New("outbox: payload is required")
	}
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}

	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: generate id: %w", err)
	}

	const query = `
		INSERT INTO outbox (id, kind, payload, max_attempts)
		VALUES ($1, $2, $3, $4)`

	if _, err := db.Exec(ctx, query, id, kind, payload, maxAttempts); err != nil {
		return uuid.Nil, fmt.Errorf("outbox: enqueue: %w", err)
	}

	return id, nil
}

type Repository struct {
	db DB
}

func NewRepository(db DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Claim(ctx context.Context, batchSize int32, visibility time.Duration) ([]Message, error) {
	const query = `
		UPDATE outbox
		SET attempts        = attempts + 1,
		    next_attempt_at = now() + make_interval(secs => $1::int)
		WHERE id IN (
		    SELECT id FROM outbox
		    WHERE failed_at IS NULL AND next_attempt_at <= now()
		    ORDER BY next_attempt_at
		    FOR UPDATE SKIP LOCKED
		    LIMIT $2
		)
		RETURNING id, kind, payload, attempts, max_attempts`

	rows, err := r.db.Query(ctx, query, int32(visibility.Seconds()), batchSize)
	if err != nil {
		return nil, fmt.Errorf("outbox: claim: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.Kind, &message.Payload, &message.Attempts, &message.MaxAttempts); err != nil {
			return nil, fmt.Errorf("outbox: scan: %w", err)
		}
		messages = append(messages, message)
	}

	return messages, rows.Err()
}

func (r *Repository) Complete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM outbox WHERE id = $1`, id); err != nil {
		return fmt.Errorf("outbox: complete: %w", err)
	}

	return nil
}

func (r *Repository) Retry(ctx context.Context, id uuid.UUID, backoff time.Duration, reason string) error {
	const query = `
		UPDATE outbox
		SET next_attempt_at = now() + make_interval(secs => $2::int),
		    last_error      = $3,
		    failed_at       = CASE WHEN attempts >= max_attempts THEN now() ELSE NULL END
		WHERE id = $1`

	if _, err := r.db.Exec(ctx, query, id, int32(backoff.Seconds()), truncate(reason)); err != nil {
		return fmt.Errorf("outbox: retry: %w", err)
	}

	return nil
}

func (r *Repository) Defer(ctx context.Context, id uuid.UUID, delay time.Duration) error {
	const query = `
		UPDATE outbox
		SET next_attempt_at = now() + make_interval(secs => $2::int),
		    attempts        = GREATEST(attempts - 1, 0)
		WHERE id = $1`

	if _, err := r.db.Exec(ctx, query, id, int32(delay.Seconds())); err != nil {
		return fmt.Errorf("outbox: defer: %w", err)
	}

	return nil
}

func (r *Repository) Fail(ctx context.Context, id uuid.UUID, reason string) error {
	const query = `
		UPDATE outbox
		SET failed_at  = now(),
		    last_error = $2
		WHERE id = $1 AND failed_at IS NULL`

	if _, err := r.db.Exec(ctx, query, id, truncate(reason)); err != nil {
		return fmt.Errorf("outbox: fail: %w", err)
	}

	return nil
}

func (r *Repository) CountPending(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE failed_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("outbox: count pending: %w", err)
	}

	return count, nil
}

func (r *Repository) CountFailed(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE failed_at IS NOT NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("outbox: count failed: %w", err)
	}

	return count, nil
}

func (r *Repository) DeleteOldFailed(ctx context.Context, retention time.Duration) (int64, error) {
	const query = `
		DELETE FROM outbox
		WHERE failed_at IS NOT NULL
		  AND failed_at < now() - make_interval(secs => $1::int)`

	tag, err := r.db.Exec(ctx, query, int32(retention.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("outbox: cleanup: %w", err)
	}

	return tag.RowsAffected(), nil
}

const maxErrorLength = 512

func truncate(reason string) *string {
	if reason == "" {
		return nil
	}

	runes := []rune(reason)
	if len(runes) > maxErrorLength {
		reason = string(runes[:maxErrorLength])
	}

	return &reason
}
