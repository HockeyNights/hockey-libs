package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Limit  int64         `env:"LIMIT" envDefault:"20"`
	Window time.Duration `env:"WINDOW" envDefault:"1m"`
}

type Result struct {
	Allowed    bool
	Count      int64
	RetryAfter time.Duration
}

type Limiter struct {
	client *redis.Client
	prefix string
	cfg    Config
}

func New(client *redis.Client, prefix string, cfg Config) *Limiter {
	if cfg.Limit <= 0 {
		cfg.Limit = 20
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}

	return &Limiter{client: client, prefix: prefix, cfg: cfg}
}

func (l *Limiter) Allow(ctx context.Context, key string) (Result, error) {
	redisKey := l.key(key)

	pipe := l.client.TxPipeline()
	incr := pipe.Incr(ctx, redisKey)
	pipe.ExpireNX(ctx, redisKey, l.cfg.Window)
	ttl := pipe.PTTL(ctx, redisKey)

	if _, err := pipe.Exec(ctx); err != nil {
		return Result{}, fmt.Errorf("ratelimit: incr %s: %w", redisKey, err)
	}

	count := incr.Val()
	result := Result{Allowed: count <= l.cfg.Limit, Count: count}
	if !result.Allowed {
		if remaining := ttl.Val(); remaining > 0 {
			result.RetryAfter = remaining
		} else {
			result.RetryAfter = l.cfg.Window
		}
	}

	return result, nil
}

func (l *Limiter) Reset(ctx context.Context, key string) error {
	if err := l.client.Del(ctx, l.key(key)).Err(); err != nil {
		return fmt.Errorf("ratelimit: del %s: %w", l.key(key), err)
	}

	return nil
}

func (l *Limiter) key(key string) string {
	return fmt.Sprintf("ratelimit:%s:%s", l.prefix, key)
}
