package grpcserver

import (
	"context"
	"log/slog"

	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/HockeyNights/hockey-libs/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InternalAuthConfig struct {
	Manager *token.Manager

	Methods []string

	Role string
}

func InternalAuthUnaryInterceptor(cfg InternalAuthConfig) grpc.UnaryServerInterceptor {
	internal := toSet(cfg.Methods)

	role := cfg.Role
	if role == "" {
		role = token.RoleService
	}

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if _, ok := internal[info.FullMethod]; !ok {
			return handler(ctx, req)
		}

		if cfg.Manager == nil {
			return nil, status.Error(codes.Internal, "internal authenticator is not configured")
		}

		raw, err := BearerToken(ctx)
		if err != nil {
			return nil, err
		}

		claims, err := cfg.Manager.ParseAccess(raw)
		if err != nil {
			logger.From(ctx).LogAttrs(ctx, slog.LevelWarn, "internal authentication failed",
				slog.String("method", info.FullMethod),
				slog.String("error", err.Error()),
			)

			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}

		if !claims.HasRole(role) {
			logger.From(ctx).LogAttrs(ctx, slog.LevelWarn, "internal call without required role",
				slog.String("method", info.FullMethod),
				slog.String("subject", claims.Subject),
			)

			return nil, status.Error(codes.PermissionDenied, "insufficient permissions")
		}

		ctx = token.NewContext(ctx, claims)
		ctx = logger.Attrs(ctx, slog.String("caller_service", claims.Subject))

		return handler(ctx, req)
	}
}
