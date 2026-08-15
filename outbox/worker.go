package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
)

type Config struct {
	PollInterval time.Duration `env:"POLL_INTERVAL" envDefault:"2s"`
	BatchSize    int32         `env:"BATCH_SIZE" envDefault:"20"`

	Concurrency int `env:"CONCURRENCY" envDefault:"8"`

	VisibilityTimeout time.Duration `env:"VISIBILITY_TIMEOUT" envDefault:"2m"`
	SendTimeout       time.Duration `env:"SEND_TIMEOUT" envDefault:"30s"`
	BaseBackoff       time.Duration `env:"BASE_BACKOFF" envDefault:"30s"`
	MaxBackoff        time.Duration `env:"MAX_BACKOFF" envDefault:"30m"`

	ConflictBackoff time.Duration `env:"CONFLICT_BACKOFF" envDefault:"15s"`

	FailedRetention time.Duration `env:"FAILED_RETENTION" envDefault:"168h"`
	CleanupInterval time.Duration `env:"CLEANUP_INTERVAL" envDefault:"1h"`
}

type Sender interface {
	Send(ctx context.Context, message Message) error
}

type Queue interface {
	Claim(ctx context.Context, batchSize int32, visibility time.Duration) ([]Message, error)
	Complete(ctx context.Context, id uuid.UUID) error
	Retry(ctx context.Context, id uuid.UUID, backoff time.Duration, reason string) error
	Defer(ctx context.Context, id uuid.UUID, delay time.Duration) error
	Fail(ctx context.Context, id uuid.UUID, reason string) error
	DeleteOldFailed(ctx context.Context, retention time.Duration) (int64, error)
}

type Counter interface {
	CountPending(ctx context.Context) (int64, error)
	CountFailed(ctx context.Context) (int64, error)
}

type Worker struct {
	cfg     Config
	queue   Queue
	sender  Sender
	logger  *slog.Logger
	metrics *Metrics
}

type Option func(*Worker)

func WithMetrics(registry *prometheus.Registry) Option {
	return func(w *Worker) {
		if registry != nil {
			w.metrics = NewMetrics(registry)
		}
	}
}

func NewWorker(cfg Config, queue Queue, sender Sender, logger *slog.Logger, opts ...Option) (*Worker, error) {
	if logger == nil {
		logger = slog.Default()
	}

	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	worker := &Worker{cfg: cfg, queue: queue, sender: sender, logger: logger}

	for _, opt := range opts {
		opt(worker)
	}

	return worker, nil
}

func (w *Worker) Run(ctx context.Context) error {
	cleanup := time.NewTicker(w.cfg.CleanupInterval)
	defer cleanup.Stop()

	poll := time.NewTimer(w.cfg.PollInterval)
	defer poll.Stop()

	for {
		processed, err := w.processBatch(ctx)
		switch {
		case err != nil && ctx.Err() != nil:
			return nil
		case err != nil:
			w.logger.ErrorContext(ctx, "outbox batch failed", "error", err)
		}

		select {
		case <-cleanup.C:
			w.cleanupFailed(ctx)
			w.observeDepth(ctx)
		default:
		}

		if err == nil && processed >= int(w.cfg.BatchSize) {
			continue
		}

		poll.Reset(w.cfg.PollInterval)
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) (int, error) {
	messages, err := w.queue.Claim(ctx, w.cfg.BatchSize, w.cfg.VisibilityTimeout)
	if err != nil {
		return 0, err
	}
	if len(messages) == 0 {
		return 0, nil
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(w.cfg.Concurrency)

	for _, message := range messages {
		group.Go(func() error {
			w.deliver(groupCtx, message)

			return nil
		})
	}

	_ = group.Wait()

	return len(messages), nil
}

func (w *Worker) deliver(ctx context.Context, message Message) {
	log := w.logger.With(
		"outbox_id", message.ID,
		"kind", message.Kind,
		"attempt", message.Attempts,
	)

	sendCtx, cancel := context.WithTimeout(ctx, w.cfg.SendTimeout)
	defer cancel()

	err := w.sender.Send(sendCtx, message)
	if err == nil {
		if err := w.queue.Complete(ctx, message.ID); err != nil {
			log.ErrorContext(ctx, "outbox complete failed, message may be sent twice", "error", err)
			return
		}

		w.metrics.observe(message.Kind, resultSent)
		log.DebugContext(ctx, "outbox message sent")
		return
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}

	switch {
	case IsTerminal(err):
		log.ErrorContext(ctx, "outbox message rejected permanently, it will never be delivered", "error", err)

		if failErr := w.queue.Fail(ctx, message.ID, err.Error()); failErr != nil {
			log.ErrorContext(ctx, "outbox fail failed", "error", failErr)
		}
		w.metrics.observe(message.Kind, resultFailed)

	case IsConflict(err):

		log.InfoContext(ctx, "outbox message is already being delivered elsewhere", "retry_in", w.cfg.ConflictBackoff.String())

		if deferErr := w.queue.Defer(ctx, message.ID, w.cfg.ConflictBackoff); deferErr != nil {
			log.ErrorContext(ctx, "outbox defer failed", "error", deferErr)
		}
		w.metrics.observe(message.Kind, resultDeferred)

	default:
		backoff := w.backoffFor(message.Attempts)

		if int(message.Attempts) >= message.MaxAttempts {
			log.ErrorContext(ctx, "outbox message gave up after last attempt, it will never be delivered", "error", err)
			w.metrics.observe(message.Kind, resultFailed)
		} else {
			log.WarnContext(ctx, "outbox send failed", "error", err, "retry_in", backoff.String())
			w.metrics.observe(message.Kind, resultRetried)
		}

		if retryErr := w.queue.Retry(ctx, message.ID, backoff, err.Error()); retryErr != nil {
			log.ErrorContext(ctx, "outbox reschedule failed", "error", retryErr)
		}
	}
}

func (w *Worker) backoffFor(attempts int16) time.Duration {
	backoff := w.cfg.BaseBackoff
	for range max(int(attempts)-1, 0) {
		backoff *= 2
		if backoff >= w.cfg.MaxBackoff {
			return w.cfg.MaxBackoff
		}
	}

	return backoff
}

func (w *Worker) cleanupFailed(ctx context.Context) {
	deleted, err := w.queue.DeleteOldFailed(ctx, w.cfg.FailedRetention)
	if err != nil {
		w.logger.ErrorContext(ctx, "outbox cleanup failed", "error", err)
		return
	}
	if deleted > 0 {

		w.logger.WarnContext(ctx, "outbox removed undelivered messages past retention", "deleted", deleted)
	}
}

func (w *Worker) observeDepth(ctx context.Context) {
	counter, ok := w.queue.(Counter)
	if !ok || w.metrics == nil {
		return
	}

	pending, err := counter.CountPending(ctx)
	if err != nil {
		w.logger.WarnContext(ctx, "outbox depth unavailable", "error", err)
		return
	}

	failed, err := counter.CountFailed(ctx)
	if err != nil {
		w.logger.WarnContext(ctx, "outbox depth unavailable", "error", err)
		return
	}

	w.metrics.depth(pending, failed)
}

func (c Config) validate() error {
	waves := (int(c.BatchSize) + c.Concurrency - 1) / c.Concurrency
	needed := time.Duration(waves) * c.SendTimeout

	if needed > c.VisibilityTimeout {
		return fmt.Errorf(
			"outbox: аренда %s короче обработки пачки (%d сообщений по %d за раз = %s): "+
				"увеличь VISIBILITY_TIMEOUT либо уменьши BATCH_SIZE, либо подними CONCURRENCY",
			c.VisibilityTimeout, c.BatchSize, c.Concurrency, needed,
		)
	}

	return nil
}

func (c Config) withDefaults() Config {
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 20
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 8
	}
	if c.VisibilityTimeout <= 0 {
		c.VisibilityTimeout = 2 * time.Minute
	}
	if c.SendTimeout <= 0 {
		c.SendTimeout = 30 * time.Second
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 30 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Minute
	}
	if c.ConflictBackoff <= 0 {
		c.ConflictBackoff = 15 * time.Second
	}
	if c.FailedRetention <= 0 {
		c.FailedRetention = 7 * 24 * time.Hour
	}
	if c.CleanupInterval <= 0 {
		c.CleanupInterval = time.Hour
	}

	return c
}
