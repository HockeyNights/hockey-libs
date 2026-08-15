package postgresql

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

type Config struct {
	DSN      string `env:"DATABASE_URL"`
	Host     string `env:"POSTGRES_HOST" envDefault:"localhost"`
	Port     int    `env:"POSTGRES_PORT" envDefault:"5432"`
	User     string `env:"POSTGRES_USER" envDefault:"postgres"`
	Password string `env:"POSTGRES_PASSWORD"`
	Database string `env:"POSTGRES_DB" envDefault:"postgres"`
	SSLMode  string `env:"POSTGRES_SSLMODE" envDefault:"disable"`

	MinConns          int32         `env:"POSTGRES_MIN_CONNS" envDefault:"2"`
	MaxConns          int32         `env:"POSTGRES_MAX_CONNS" envDefault:"20"`
	MaxConnLifetime   time.Duration `env:"POSTGRES_MAX_CONN_LIFETIME" envDefault:"30m"`
	MaxConnIdleTime   time.Duration `env:"POSTGRES_MAX_CONN_IDLE_TIME" envDefault:"5m"`
	HealthCheckPeriod time.Duration `env:"POSTGRES_HEALTH_CHECK_PERIOD" envDefault:"1m"`
	ConnectTimeout    time.Duration `env:"POSTGRES_CONNECT_TIMEOUT" envDefault:"10s"`

	DisablePreparedStatements bool `env:"POSTGRES_DISABLE_PREPARED_STATEMENTS"`
}

func DefaultConfig() Config {
	return Config{
		Host:              "localhost",
		Port:              5432,
		User:              "postgres",
		Database:          "postgres",
		SSLMode:           "disable",
		MinConns:          2,
		MaxConns:          20,
		MaxConnLifetime:   30 * time.Minute,
		MaxConnIdleTime:   5 * time.Minute,
		HealthCheckPeriod: time.Minute,
		ConnectTimeout:    10 * time.Second,
	}
}

func (c Config) withDefaults() Config {
	defaults := DefaultConfig()

	if c.Host == "" {
		c.Host = defaults.Host
	}
	if c.Port == 0 {
		c.Port = defaults.Port
	}
	if c.User == "" {
		c.User = defaults.User
	}
	if c.Database == "" {
		c.Database = defaults.Database
	}
	if c.SSLMode == "" {
		c.SSLMode = defaults.SSLMode
	}
	if c.MinConns == 0 {
		c.MinConns = defaults.MinConns
	}
	if c.MaxConns == 0 {
		c.MaxConns = defaults.MaxConns
	}
	if c.MaxConnLifetime == 0 {
		c.MaxConnLifetime = defaults.MaxConnLifetime
	}
	if c.MaxConnIdleTime == 0 {
		c.MaxConnIdleTime = defaults.MaxConnIdleTime
	}
	if c.HealthCheckPeriod == 0 {
		c.HealthCheckPeriod = defaults.HealthCheckPeriod
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = defaults.ConnectTimeout
	}

	return c
}

func (c Config) Validate() error {
	cfg := c.withDefaults()

	if cfg.MinConns < 0 {
		return fmt.Errorf("postgresql: min conns must be >= 0, got %d", cfg.MinConns)
	}
	if cfg.MaxConns <= 0 {
		return fmt.Errorf("postgresql: max conns must be > 0, got %d", cfg.MaxConns)
	}
	if cfg.MinConns > cfg.MaxConns {
		return fmt.Errorf("postgresql: min conns (%d) must not exceed max conns (%d)", cfg.MinConns, cfg.MaxConns)
	}

	return nil
}

func (c Config) DSNString() string {
	if c.DSN != "" {
		return c.DSN
	}

	cfg := c.withDefaults()

	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   cfg.Database,
	}

	query := dsn.Query()
	query.Set("sslmode", cfg.SSLMode)
	dsn.RawQuery = query.Encode()

	return dsn.String()
}

func (c Config) SafeDSNString() string {
	parsed, err := url.Parse(c.DSNString())
	if err != nil {
		return "postgres://<unparsable dsn>"
	}

	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "xxxxx")
		}
	}

	return parsed.String()
}
