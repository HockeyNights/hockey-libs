package postgresql

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Option func(*pgxpool.Config)

func WithTracing() Option {
	return func(cfg *pgxpool.Config) {
		cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	}
}

func WithAfterConnect(hook func(ctx context.Context, conn *pgx.Conn) error) Option {
	return func(cfg *pgxpool.Config) {
		previous := cfg.AfterConnect
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if previous != nil {
				if err := previous(ctx, conn); err != nil {
					return err
				}
			}
			return hook(ctx, conn)
		}
	}
}

func WithPoolConfig(mutate func(*pgxpool.Config)) Option {
	return Option(mutate)
}

func Open(ctx context.Context, cfg Config, opts ...Option) (*pgxpool.Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.withDefaults()

	poolConfig, err := pgxpool.ParseConfig(cfg.DSNString())
	if err != nil {
		return nil, fmt.Errorf("postgresql: parse config for %s: %w", cfg.SafeDSNString(), err)
	}

	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	if cfg.DisablePreparedStatements {
		poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		poolConfig.ConnConfig.StatementCacheCapacity = 0
		poolConfig.ConnConfig.DescriptionCacheCapacity = 0
	}

	for _, opt := range opts {
		opt(poolConfig)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("postgresql: open %s: %w", cfg.SafeDSNString(), err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgresql: ping %s: %w", cfg.SafeDSNString(), err)
	}

	return pool, nil
}
