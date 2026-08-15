package grpcserver

import (
	"context"
	"log/slog"
	"strings"

	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/HockeyNights/hockey-libs/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const AuthorizationKey = "authorization"

const bearerPrefix = "bearer "

type Authenticator func(ctx context.Context, rawToken string) (context.Context, error)

type AuthConfig struct {
	Authenticate  Authenticator
	PublicMethods []string
	Optional      bool
}

func AuthUnaryInterceptor(cfg AuthConfig) grpc.UnaryServerInterceptor {
	public := toSet(cfg.PublicMethods)

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if _, isPublic := public[info.FullMethod]; isPublic {
			return handler(ctx, req)
		}

		ctx, err := authenticate(ctx, cfg)
		if err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

func AuthStreamInterceptor(cfg AuthConfig) grpc.StreamServerInterceptor {
	public := toSet(cfg.PublicMethods)

	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if _, isPublic := public[info.FullMethod]; isPublic {
			return handler(srv, stream)
		}

		ctx, err := authenticate(stream.Context(), cfg)
		if err != nil {
			return err
		}

		return handler(srv, &wrappedStream{ServerStream: stream, ctx: ctx})
	}
}

func authenticate(ctx context.Context, cfg AuthConfig) (context.Context, error) {
	if cfg.Authenticate == nil {
		return nil, status.Error(codes.Internal, "authenticator is not configured")
	}

	raw, err := BearerToken(ctx)
	if err != nil {
		if cfg.Optional {
			return ctx, nil
		}
		return nil, err
	}

	authenticated, err := cfg.Authenticate(ctx, raw)
	if err != nil {
		logger.From(ctx).LogAttrs(ctx, slog.LevelWarn, "authentication failed",
			slog.String("error", err.Error()),
		)
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	return authenticated, nil
}

func BearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing credentials")
	}

	header := metadataValue(md, AuthorizationKey)
	if header == "" {
		return "", status.Error(codes.Unauthenticated, "missing credentials")
	}

	if len(header) < len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", status.Error(codes.Unauthenticated, "invalid authorization scheme")
	}

	raw := strings.TrimSpace(header[len(bearerPrefix):])
	if raw == "" {
		return "", status.Error(codes.Unauthenticated, "missing credentials")
	}

	return raw, nil
}

func TokenAuthenticator(manager *token.Manager) Authenticator {
	return func(ctx context.Context, raw string) (context.Context, error) {
		claims, err := manager.ParseAccess(raw)
		if err != nil {
			return nil, err
		}

		ctx = token.NewContext(ctx, claims)
		ctx = logger.Attrs(ctx, slog.String("user_id", claims.Subject))

		return ctx, nil
	}
}

func RequireRole(roles ...string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		claims, ok := token.FromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing credentials")
		}

		for _, role := range roles {
			if claims.HasRole(role) {
				return handler(ctx, req)
			}
		}

		return nil, status.Error(codes.PermissionDenied, "insufficient permissions")
	}
}
