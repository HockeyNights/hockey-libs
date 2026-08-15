package grpcserver

import (
	"context"
	"log/slog"

	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/HockeyNights/hockey-libs/requestid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func RequestIDUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		ctx, id := withRequestID(ctx)

		_ = grpc.SetHeader(ctx, metadata.Pairs(requestid.MetadataKey, id))

		return handler(ctx, req)
	}
}

func RequestIDStreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		stream grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx, id := withRequestID(stream.Context())

		_ = stream.SetHeader(metadata.Pairs(requestid.MetadataKey, id))

		return handler(srv, &wrappedStream{ServerStream: stream, ctx: ctx})
	}
}

func withRequestID(ctx context.Context) (context.Context, string) {
	id := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		id = metadataValue(md, requestid.MetadataKey)
	}
	if id == "" {
		id = requestid.Generate()
	}

	ctx = requestid.NewContext(ctx, id)
	ctx = logger.Attrs(ctx, slog.String("request_id", id))

	return ctx, id
}
