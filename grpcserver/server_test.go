package grpcserver_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/grpcclient"
	"github.com/HockeyNights/hockey-libs/grpcserver"
	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/HockeyNights/hockey-libs/observability"
	"github.com/HockeyNights/hockey-libs/requestid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

func freeAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	return address
}

func startServer(t *testing.T, cfg grpcserver.Config, registers ...grpcserver.RegisterFunc) *grpc.ClientConn {
	t.Helper()

	cfg.Address = freeAddress(t)
	cfg.Logger = logger.Discard()
	if cfg.DrainDelay == 0 {
		cfg.DrainDelay = time.Millisecond
	}

	server := grpcserver.New(cfg, registers...)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()

	conn, err := grpcclient.New(context.Background(), grpcclient.Config{
		Target:            cfg.Address,
		Logger:            logger.Discard(),
		WaitForConnection: true,
		ConnectTimeout:    2 * time.Second,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = conn.Close()
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not stop")
		}
	})

	return conn
}

func TestServerServesHealthCheck(t *testing.T) {
	conn := startServer(t, grpcserver.Config{EnableHealth: true})

	response, err := healthv1.NewHealthClient(conn).Check(
		context.Background(),
		&healthv1.HealthCheckRequest{},
	)
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_SERVING, response.GetStatus())
}

func TestServerReturnsRequestIDHeader(t *testing.T) {
	conn := startServer(t, grpcserver.Config{EnableHealth: true})

	var header metadata.MD

	_, err := healthv1.NewHealthClient(conn).Check(
		context.Background(),
		&healthv1.HealthCheckRequest{},
		grpc.Header(&header),
	)
	require.NoError(t, err)

	require.Len(t, header.Get(requestid.MetadataKey), 1)
	require.NotEmpty(t, header.Get(requestid.MetadataKey)[0])
}

func TestServerPropagatesIncomingRequestID(t *testing.T) {
	conn := startServer(t, grpcserver.Config{EnableHealth: true})

	ctx := requestid.NewContext(context.Background(), "req-from-gateway")

	var header metadata.MD

	_, err := healthv1.NewHealthClient(conn).Check(ctx, &healthv1.HealthCheckRequest{}, grpc.Header(&header))
	require.NoError(t, err)

	require.Equal(t, []string{"req-from-gateway"}, header.Get(requestid.MetadataKey))
}

func TestServerExposesMetrics(t *testing.T) {
	registry := observability.NewRegistry()

	conn := startServer(t, grpcserver.Config{EnableHealth: true, Registry: registry})

	_, err := healthv1.NewHealthClient(conn).Check(context.Background(), &healthv1.HealthCheckRequest{})
	require.NoError(t, err)

	families, err := registry.Gather()
	require.NoError(t, err)

	found := false
	for _, family := range families {
		if family.GetName() == "grpc_server_requests_total" {
			found = true
		}
	}
	require.True(t, found, "grpc_server_requests_total was not collected")
}

func TestServerFailsOnBusyPort(t *testing.T) {
	address := freeAddress(t)

	listener, err := net.Listen("tcp", address)
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	server := grpcserver.New(grpcserver.Config{Address: address, Logger: logger.Discard()})

	require.Error(t, server.Run(context.Background()))
}
