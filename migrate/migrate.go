package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/HockeyNights/hockey-libs/postgresql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

const TableName = "schema_migrations"

type Config struct {
	Dir    string
	Logger *slog.Logger

	Table string

	DisableSessionLock bool
}

func Up(ctx context.Context, dbCfg postgresql.Config, fsys fs.FS, cfg Config) (int64, error) {
	var version int64

	err := withProvider(ctx, dbCfg, fsys, cfg, func(provider *goose.Provider) error {
		results, err := provider.Up(ctx)
		if err != nil {
			return err
		}

		logger := loggerOf(cfg)
		for _, result := range results {
			logger.Info("migration applied",
				"version", result.Source.Version,
				"path", result.Source.Path,
				"duration", result.Duration.String(),
			)
		}

		version, err = provider.GetDBVersion(ctx)
		return err
	})

	return version, err
}

func UpTo(ctx context.Context, dbCfg postgresql.Config, fsys fs.FS, cfg Config, version int64) error {
	return withProvider(ctx, dbCfg, fsys, cfg, func(provider *goose.Provider) error {
		_, err := provider.UpTo(ctx, version)
		return err
	})
}

func Down(ctx context.Context, dbCfg postgresql.Config, fsys fs.FS, cfg Config) error {
	return withProvider(ctx, dbCfg, fsys, cfg, func(provider *goose.Provider) error {
		_, err := provider.Down(ctx)
		return err
	})
}

func Version(ctx context.Context, dbCfg postgresql.Config, fsys fs.FS, cfg Config) (int64, error) {
	var version int64

	err := withProvider(ctx, dbCfg, fsys, cfg, func(provider *goose.Provider) error {
		current, err := provider.GetDBVersion(ctx)
		version = current
		return err
	})

	return version, err
}

func HasPending(ctx context.Context, dbCfg postgresql.Config, fsys fs.FS, cfg Config) (bool, error) {
	var pending bool

	err := withProvider(ctx, dbCfg, fsys, cfg, func(provider *goose.Provider) error {
		result, err := provider.HasPending(ctx)
		pending = result
		return err
	})

	return pending, err
}

func Status(ctx context.Context, dbCfg postgresql.Config, fsys fs.FS, cfg Config) ([]*goose.MigrationStatus, error) {
	var statuses []*goose.MigrationStatus

	err := withProvider(ctx, dbCfg, fsys, cfg, func(provider *goose.Provider) error {
		result, err := provider.Status(ctx)
		statuses = result
		return err
	})

	return statuses, err
}

func withProvider(
	ctx context.Context,
	dbCfg postgresql.Config,
	fsys fs.FS,
	cfg Config,
	action func(*goose.Provider) error,
) error {
	if cfg.Dir == "" {
		return fmt.Errorf("migrate: dir is required")
	}
	if err := dbCfg.Validate(); err != nil {
		return err
	}

	migrationsFS, err := fs.Sub(fsys, cfg.Dir)
	if err != nil {
		return fmt.Errorf("migrate: open %s: %w", cfg.Dir, err)
	}

	connConfig, err := pgx.ParseConfig(dbCfg.DSNString())
	if err != nil {
		return fmt.Errorf("migrate: parse config for %s: %w", dbCfg.SafeDSNString(), err)
	}

	db := stdlib.OpenDB(*connConfig)
	defer func() { _ = db.Close() }()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("migrate: ping %s: %w", dbCfg.SafeDSNString(), err)
	}

	table := cfg.Table
	if table == "" {
		table = TableName
	}

	options := []goose.ProviderOption{
		goose.WithTableName(table),
		goose.WithSlog(loggerOf(cfg)),
	}

	if !cfg.DisableSessionLock {
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return fmt.Errorf("migrate: create session locker: %w", err)
		}
		options = append(options, goose.WithSessionLocker(locker))
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationsFS, options...)
	if err != nil {
		return fmt.Errorf("migrate: create provider: %w", err)
	}

	if err := action(provider); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	return nil
}

func loggerOf(cfg Config) *slog.Logger {
	if cfg.Logger != nil {
		return cfg.Logger
	}
	return slog.Default()
}
