package grpcserver

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/HockeyNights/hockey-libs/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type RateLimiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error)
}

type RateLimitConfig struct {
	Limiter RateLimiter

	Methods []string

	TrustedProxies TrustedProxies

	Logger *slog.Logger
}

func RateLimitUnaryInterceptor(cfg RateLimitConfig) grpc.UnaryServerInterceptor {
	log := orDefaultLogger(cfg.Logger)
	limited := toSet(cfg.Methods)
	all := len(cfg.Methods) == 0

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if cfg.Limiter == nil {
			return handler(ctx, req)
		}
		if _, ok := limited[info.FullMethod]; !ok && !all {
			return handler(ctx, req)
		}

		key := ClientIP(ctx, cfg.TrustedProxies)
		if key == "" {
			return handler(ctx, req)
		}

		allowed, retryAfter, err := cfg.Limiter.Allow(ctx, key)
		if err != nil {
			log.WarnContext(ctx, "rate limiter unavailable", "error", err, "method", info.FullMethod)

			return handler(ctx, req)
		}

		if !allowed {
			logger.From(ctx).LogAttrs(ctx, slog.LevelWarn, "rate limit exceeded",
				slog.String("method", info.FullMethod),
				slog.String("retry_after", retryAfter.String()),
			)

			if retryAfter > 0 {
				_ = grpc.SetHeader(ctx, metadata.Pairs(
					"retry-after", strconv.Itoa(int(retryAfter.Seconds()+0.5)),
				))
			}

			return nil, status.Error(codes.ResourceExhausted, "too many requests")
		}

		return handler(ctx, req)
	}
}
