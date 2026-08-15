package grpcserver

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/HockeyNights/hockey-libs/requestid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func LoggingUnaryInterceptor(log *slog.Logger, skipMethods ...string) grpc.UnaryServerInterceptor {
	log = orDefaultLogger(log)
	skip := toSet(skipMethods)

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if _, skipped := skip[info.FullMethod]; skipped {
			return handler(ctx, req)
		}

		ctx = logger.Into(ctx, log)

		start := time.Now()
		resp, err := handler(ctx, req)
		logRequest(ctx, log, info.FullMethod, time.Since(start), err)

		return resp, err
	}
}

func LoggingStreamInterceptor(log *slog.Logger, skipMethods ...string) grpc.StreamServerInterceptor {
	log = orDefaultLogger(log)
	skip := toSet(skipMethods)

	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if _, skipped := skip[info.FullMethod]; skipped {
			return handler(srv, stream)
		}

		ctx := logger.Into(stream.Context(), log)

		start := time.Now()
		err := handler(srv, &wrappedStream{ServerStream: stream, ctx: ctx})
		logRequest(ctx, log, info.FullMethod, time.Since(start), err)

		return err
	}
}

func RecoveryUnaryInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	log = orDefaultLogger(log)

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logPanic(ctx, log, info.FullMethod, recovered)
				err = status.Error(codes.Internal, "internal server error")
				resp = nil
			}
		}()

		return handler(ctx, req)
	}
}

func RecoveryStreamInterceptor(log *slog.Logger) grpc.StreamServerInterceptor {
	log = orDefaultLogger(log)

	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logPanic(stream.Context(), log, info.FullMethod, recovered)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()

		return handler(srv, stream)
	}
}

func TimeoutUnaryInterceptor(timeout time.Duration) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if _, hasDeadline := ctx.Deadline(); hasDeadline || timeout <= 0 {
			return handler(ctx, req)
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return handler(ctx, req)
	}
}

func logRequest(ctx context.Context, log *slog.Logger, method string, duration time.Duration, err error) {
	code := status.Code(err)

	attrs := []any{
		"method", method,
		"code", code.String(),
		"duration_ms", duration.Milliseconds(),
	}

	if id := requestid.Get(ctx); id != "" {
		attrs = append(attrs, "request_id", id)
	}
	if address := peerAddress(ctx); address != "" {
		attrs = append(attrs, "peer", address)
	}

	if err == nil {
		log.LogAttrs(ctx, slog.LevelInfo, "grpc request handled", toAttrs(attrs)...)
		return
	}

	attrs = append(attrs, "error", err.Error())

	level := slog.LevelWarn
	if isServerFault(code) {
		level = slog.LevelError
	}

	log.LogAttrs(ctx, level, "grpc request failed", toAttrs(attrs)...)
}

func logPanic(ctx context.Context, log *slog.Logger, method string, recovered any) {
	log.ErrorContext(ctx, "grpc panic recovered",
		"method", method,
		"panic", recovered,
		"request_id", requestid.Get(ctx),
		"stack", string(debug.Stack()),
	)
}

func isServerFault(code codes.Code) bool {
	switch code {
	case codes.Unknown, codes.Internal, codes.DataLoss, codes.Unimplemented:
		return true
	default:
		return false
	}
}

func peerAddress(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return ""
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *wrappedStream) Context() context.Context { return s.ctx }

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func toAttrs(args []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		attrs = append(attrs, slog.Any(key, args[i+1]))
	}
	return attrs
}

func orDefaultLogger(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.Default()
}

func metadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
