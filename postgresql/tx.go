package postgresql

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type TxBeginner interface {
	Beginner
	BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error)
}

func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgresql: begin tx: %w", err)
	}

	return runTx(ctx, tx, fn)
}

func InTxOptions(ctx context.Context, db TxBeginner, options pgx.TxOptions, fn func(tx pgx.Tx) error) error {
	tx, err := db.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("postgresql: begin tx: %w", err)
	}

	return runTx(ctx, tx, fn)
}

func runTx(ctx context.Context, tx pgx.Tx, fn func(tx pgx.Tx) error) error {
	committed := false

	defer func() {
		if committed {
			return
		}
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgresql: commit tx: %w", err)
	}
	committed = true

	return nil
}

var (
	_ Beginner   = (*pgxpool.Pool)(nil)
	_ TxBeginner = (*pgxpool.Pool)(nil)
)
