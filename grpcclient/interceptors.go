package grpcclient

import (
	"context"
	"log/slog"
	"time"

	"github.com/HockeyNights/hockey-libs/requestid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func RequestIDUnaryInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		conn *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		return invoker(withRequestID(ctx), method, req, reply, conn, opts...)
	}
}

func RequestIDStreamInterceptor() grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		conn *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		return streamer(withRequestID(ctx), desc, conn, method, opts...)
	}
}

func TimeoutUnaryInterceptor(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		conn *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		if _, hasDeadline := ctx.Deadline(); hasDeadline || timeout <= 0 {
			return invoker(ctx, method, req, reply, conn, opts...)
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return invoker(ctx, method, req, reply, conn, opts...)
	}
}

func LoggingUnaryInterceptor(log *slog.Logger) grpc.UnaryClientInterceptor {
	log = orDefaultLogger(log)

	return func(
		ctx context.Context,
		method string,
		req, reply any,
		conn *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, conn, opts...)
		logCall(ctx, log, method, conn.Target(), time.Since(start), err)

		return err
	}
}

func LoggingStreamInterceptor(log *slog.Logger) grpc.StreamClientInterceptor {
	log = orDefaultLogger(log)

	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		conn *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		start := time.Now()
		stream, err := streamer(ctx, desc, conn, method, opts...)
		logCall(ctx, log, method, conn.Target(), time.Since(start), err)

		return stream, err
	}
}

func StaticTokenInterceptor(token func(ctx context.Context) (string, error)) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		conn *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		value, err := token(ctx)
		if err != nil {
			return status.Errorf(codes.Unauthenticated, "get service token: %v", err)
		}

		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+value)

		return invoker(ctx, method, req, reply, conn, opts...)
	}
}

func withRequestID(ctx context.Context) context.Context {
	id, ok := requestid.FromContext(ctx)
	if !ok {
		return ctx
	}

	if md, exists := metadata.FromOutgoingContext(ctx); exists && len(md.Get(requestid.MetadataKey)) > 0 {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, requestid.MetadataKey, id)
}

func logCall(ctx context.Context, log *slog.Logger, method, target string, duration time.Duration, err error) {
	attrs := []slog.Attr{
		slog.String("method", method),
		slog.String("target", target),
		slog.String("code", status.Code(err).String()),
		slog.Int64("duration_ms", duration.Milliseconds()),
	}

	if id := requestid.Get(ctx); id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}

	if err == nil {
		log.LogAttrs(ctx, slog.LevelDebug, "grpc client call", attrs...)
		return
	}

	attrs = append(attrs, slog.String("error", err.Error()))
	log.LogAttrs(ctx, slog.LevelWarn, "grpc client call failed", attrs...)
}

func orDefaultLogger(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.Default()
}
