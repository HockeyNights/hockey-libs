package grpcserver_test

import (
	"context"
	"testing"

	"github.com/HockeyNights/hockey-libs/grpcserver"
	"github.com/HockeyNights/hockey-libs/requestid"
	"github.com/stretchr/testify/require"
)

func TestRequestIDInterceptorReusesIncomingID(t *testing.T) {
	interceptor := grpcserver.RequestIDUnaryInterceptor()

	ctx := incomingContext(requestid.MetadataKey, "req-from-gateway")

	_, err := interceptor(ctx, nil, unaryInfo(profileMethod), func(ctx context.Context, _ any) (any, error) {
		require.Equal(t, "req-from-gateway", requestid.Get(ctx))
		return "ok", nil
	})

	require.NoError(t, err)
}

func TestRequestIDInterceptorGeneratesWhenMissing(t *testing.T) {
	interceptor := grpcserver.RequestIDUnaryInterceptor()

	var generated string

	_, err := interceptor(context.Background(), nil, unaryInfo(profileMethod), func(ctx context.Context, _ any) (any, error) {
		generated = requestid.Get(ctx)
		return "ok", nil
	})

	require.NoError(t, err)
	require.NotEmpty(t, generated)
	require.Len(t, generated, 36)
}
