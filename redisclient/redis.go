package redisclient

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	URL      string `env:"REDIS_URL"`
	Host     string `env:"REDIS_HOST" envDefault:"localhost"`
	Port     int    `env:"REDIS_PORT" envDefault:"6379"`
	Username string `env:"REDIS_USERNAME"`
	Password string `env:"REDIS_PASSWORD"`
	DB       int    `env:"REDIS_DB"`

	PoolSize        int           `env:"REDIS_POOL_SIZE" envDefault:"10"`
	MinIdleConns    int           `env:"REDIS_MIN_IDLE_CONNS" envDefault:"2"`
	DialTimeout     time.Duration `env:"REDIS_DIAL_TIMEOUT" envDefault:"5s"`
	ReadTimeout     time.Duration `env:"REDIS_READ_TIMEOUT" envDefault:"3s"`
	WriteTimeout    time.Duration `env:"REDIS_WRITE_TIMEOUT" envDefault:"3s"`
	MaxRetries      int           `env:"REDIS_MAX_RETRIES" envDefault:"3"`
	ConnMaxLifetime time.Duration `env:"REDIS_CONN_MAX_LIFETIME" envDefault:"30m"`
}

func (c Config) Options() (*redis.Options, error) {
	var options *redis.Options

	if c.URL != "" {
		parsed, err := redis.ParseURL(c.URL)
		if err != nil {
			return nil, fmt.Errorf("redisclient: parse REDIS_URL: %w", err)
		}
		options = parsed
	} else {
		options = &redis.Options{
			Addr:     fmt.Sprintf("%s:%d", orDefault(c.Host, "localhost"), orDefaultInt(c.Port, 6379)),
			Username: c.Username,
			Password: c.Password,
			DB:       c.DB,
		}
	}

	options.PoolSize = orDefaultInt(c.PoolSize, 10)
	options.MinIdleConns = orDefaultInt(c.MinIdleConns, 2)
	options.DialTimeout = orDefaultDuration(c.DialTimeout, 5*time.Second)
	options.ReadTimeout = orDefaultDuration(c.ReadTimeout, 3*time.Second)
	options.WriteTimeout = orDefaultDuration(c.WriteTimeout, 3*time.Second)
	options.MaxRetries = orDefaultInt(c.MaxRetries, 3)
	options.ConnMaxLifetime = orDefaultDuration(c.ConnMaxLifetime, 30*time.Minute)

	return options, nil
}

func Open(ctx context.Context, cfg Config, hooks ...redis.Hook) (*redis.Client, error) {
	options, err := cfg.Options()
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(options)
	for _, hook := range hooks {
		client.AddHook(hook)
	}

	pingCtx, cancel := context.WithTimeout(ctx, options.DialTimeout)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redisclient: ping %s: %w", options.Addr, err)
	}

	return client, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func orDefaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func orDefaultDuration(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	return value
}
