package grpcclient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/requestid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func noopInvoker(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
	return nil
}

func TestLoggingUnaryInterceptorReturnsInvokerError(t *testing.T) {
	expectedErr := errors.New("failed")
	interceptor := LoggingUnaryInterceptor(testLogger())

	err := interceptor(
		context.Background(),
		"/test.Service/Method",
		nil,
		nil,
		&grpc.ClientConn{},
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			return expectedErr
		},
	)

	require.ErrorIs(t, err, expectedErr)
}

func TestRequestIDInterceptorSendsIDFromContext(t *testing.T) {
	interceptor := RequestIDUnaryInterceptor()

	ctx := requestid.NewContext(context.Background(), "req-1")

	err := interceptor(ctx, "/test.Service/Method", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, ok := metadata.FromOutgoingContext(ctx)
			require.True(t, ok)
			require.Equal(t, []string{"req-1"}, md.Get(requestid.MetadataKey))
			return nil
		},
	)

	require.NoError(t, err)
}

func TestRequestIDInterceptorDoesNotDuplicate(t *testing.T) {
	interceptor := RequestIDUnaryInterceptor()

	ctx := requestid.NewContext(context.Background(), "req-1")
	ctx = metadata.AppendToOutgoingContext(ctx, requestid.MetadataKey, "req-1")

	err := interceptor(ctx, "/test.Service/Method", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, _ := metadata.FromOutgoingContext(ctx)
			require.Len(t, md.Get(requestid.MetadataKey), 1)
			return nil
		},
	)

	require.NoError(t, err)
}

func TestRequestIDInterceptorSkipsWhenAbsent(t *testing.T) {
	interceptor := RequestIDUnaryInterceptor()

	err := interceptor(context.Background(), "/test.Service/Method", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, ok := metadata.FromOutgoingContext(ctx)
			require.False(t, ok && len(md.Get(requestid.MetadataKey)) > 0)
			return nil
		},
	)

	require.NoError(t, err)
}

func TestTimeoutInterceptorSetsDeadline(t *testing.T) {
	interceptor := TimeoutUnaryInterceptor(50 * time.Millisecond)

	err := interceptor(context.Background(), "/test.Service/Method", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.WithinDuration(t, time.Now().Add(50*time.Millisecond), deadline, 20*time.Millisecond)
			return nil
		},
	)

	require.NoError(t, err)
}

func TestTimeoutInterceptorKeepsCallerDeadline(t *testing.T) {
	interceptor := TimeoutUnaryInterceptor(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := interceptor(ctx, "/test.Service/Method", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.WithinDuration(t, time.Now().Add(30*time.Millisecond), deadline, 20*time.Millisecond)
			return nil
		},
	)

	require.NoError(t, err)
}

func TestStaticTokenInterceptorAddsAuthorization(t *testing.T) {
	interceptor := StaticTokenInterceptor(func(context.Context) (string, error) {
		return "service-token", nil
	})

	err := interceptor(context.Background(), "/test.Service/Method", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, ok := metadata.FromOutgoingContext(ctx)
			require.True(t, ok)
			require.Equal(t, []string{"Bearer service-token"}, md.Get("authorization"))
			return nil
		},
	)

	require.NoError(t, err)
}

func TestStaticTokenInterceptorFailsWithoutToken(t *testing.T) {
	interceptor := StaticTokenInterceptor(func(context.Context) (string, error) {
		return "", errors.New("token endpoint unavailable")
	})

	err := interceptor(context.Background(), "/test.Service/Method", nil, nil, nil, noopInvoker)

	require.Equal(t, codes.Unauthenticated, status.Code(err))
}
