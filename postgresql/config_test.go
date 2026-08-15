package postgresql_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/config"
	"github.com/HockeyNights/hockey-libs/postgresql"
	"github.com/stretchr/testify/require"
)

func TestDSNString(t *testing.T) {
	cfg := postgresql.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "p@ss word/1",
		Database: "together",
		SSLMode:  "disable",
	}

	dsn, err := url.Parse(cfg.DSNString())
	require.NoError(t, err)

	require.Equal(t, "localhost:5432", dsn.Host)
	require.Equal(t, "/together", dsn.Path)
	require.Equal(t, "disable", dsn.Query().Get("sslmode"))

	password, hasPassword := dsn.User.Password()
	require.True(t, hasPassword)
	require.Equal(t, "p@ss word/1", password)
}

func TestDSNStringPrefersExplicitDSN(t *testing.T) {
	cfg := postgresql.Config{
		DSN:  "postgres://user:pass@db:5432/app?sslmode=require",
		Host: "ignored",
	}

	require.Equal(t, "postgres://user:pass@db:5432/app?sslmode=require", cfg.DSNString())
}

func TestSafeDSNStringHidesPassword(t *testing.T) {
	cfg := postgresql.Config{
		Host:     "db",
		Port:     5432,
		User:     "app",
		Password: "super-secret",
		Database: "together",
	}

	safe := cfg.SafeDSNString()

	require.NotContains(t, safe, "super-secret")
	require.Contains(t, safe, "app")
	require.Contains(t, safe, "db:5432")
}

func TestValidate(t *testing.T) {
	tests := map[string]struct {
		cfg     postgresql.Config
		wantErr string
	}{
		"defaults are valid":  {cfg: postgresql.Config{}},
		"min above max":       {cfg: postgresql.Config{MinConns: 10, MaxConns: 5}, wantErr: "must not exceed"},
		"negative max":        {cfg: postgresql.Config{MaxConns: -1}, wantErr: "must be > 0"},
		"explicit valid pair": {cfg: postgresql.Config{MinConns: 1, MaxConns: 4}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.cfg.Validate()

			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), test.wantErr), "got: %v", err)
		})
	}
}

func TestConfigFromEnvironment(t *testing.T) {
	t.Setenv("POSTGRES_HOST", "database")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_USER", "app")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("POSTGRES_DB", "together")
	t.Setenv("POSTGRES_SSLMODE", "require")
	t.Setenv("POSTGRES_MAX_CONNS", "50")
	t.Setenv("POSTGRES_MAX_CONN_LIFETIME", "1h")

	cfg, err := config.Load[postgresql.Config]("testdata/does-not-exist.env")
	require.NoError(t, err)

	require.Equal(t, "database", cfg.Host)
	require.Equal(t, 5433, cfg.Port)
	require.Equal(t, "together", cfg.Database)
	require.Equal(t, "require", cfg.SSLMode)
	require.Equal(t, int32(50), cfg.MaxConns)
	require.Equal(t, time.Hour, cfg.MaxConnLifetime)
}
