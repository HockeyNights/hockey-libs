package httpserver_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/httpserver"
	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/stretchr/testify/require"
)

func TestRunStopsOnContextCancel(t *testing.T) {
	server := httpserver.New(httpserver.Config{
		Address: "127.0.0.1:0",
		Logger:  logger.Discard(),
		Handler: http.NewServeMux(),
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after context cancel")
	}
}

func TestRunRequiresHandler(t *testing.T) {
	server := httpserver.New(httpserver.Config{Address: "127.0.0.1:0", Logger: logger.Discard()})

	require.ErrorContains(t, server.Run(context.Background()), "handler is required")
}

func TestRunFailsOnBusyPort(t *testing.T) {
	first := httpserver.New(httpserver.Config{
		Address: "127.0.0.1:38119",
		Logger:  logger.Discard(),
		Handler: http.NewServeMux(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = first.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	second := httpserver.New(httpserver.Config{
		Address: "127.0.0.1:38119",
		Logger:  logger.Discard(),
		Handler: http.NewServeMux(),
	})

	require.Error(t, second.Run(context.Background()))
}
