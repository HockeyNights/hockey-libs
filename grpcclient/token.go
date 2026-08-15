package grpcclient

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const authorizationKey = "authorization"

const bearerPrefix = "bearer "

type tokenContextKey struct{}

func WithToken(ctx context.Context, rawToken string) context.Context {
	if rawToken == "" {
		return ctx
	}

	return context.WithValue(ctx, tokenContextKey{}, rawToken)
}

func TokenFromContext(ctx context.Context) (string, bool) {
	raw, ok := ctx.Value(tokenContextKey{}).(string)
	return raw, ok && raw != ""
}

func ForwardTokenUnaryInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		conn *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		return invoker(withForwardedToken(ctx), method, req, reply, conn, opts...)
	}
}

func ForwardTokenStreamInterceptor() grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		conn *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		return streamer(withForwardedToken(ctx), desc, conn, method, opts...)
	}
}

func withForwardedToken(ctx context.Context) context.Context {
	if outgoing, ok := metadata.FromOutgoingContext(ctx); ok {
		if len(outgoing.Get(authorizationKey)) > 0 {
			return ctx
		}
	}

	raw, ok := TokenFromContext(ctx)
	if !ok {
		raw = incomingBearer(ctx)
	}
	if raw == "" {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, authorizationKey, "Bearer "+raw)
}

func incomingBearer(ctx context.Context) string {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	values := incoming.Get(authorizationKey)
	if len(values) == 0 {
		return ""
	}

	header := values[0]
	if len(header) < len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}

	return strings.TrimSpace(header[len(bearerPrefix):])
}
