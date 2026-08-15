package httpserver_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HockeyNights/hockey-libs/httpserver"
	"github.com/HockeyNights/hockey-libs/observability"
	"github.com/stretchr/testify/require"
)

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

	return recorder
}

func TestHealthzIgnoresDependencies(t *testing.T) {
	mux := httpserver.Ops(httpserver.OpsConfig{
		Readiness: map[string]httpserver.Check{
			"postgres": func(context.Context) error { return errors.New("connection refused") },
		},
	})

	require.Equal(t, http.StatusOK, get(t, mux, "/healthz").Code)
	require.Equal(t, http.StatusServiceUnavailable, get(t, mux, "/readyz").Code)
}

func TestReadyzPassesWhenAllChecksPass(t *testing.T) {
	mux := httpserver.Ops(httpserver.OpsConfig{
		Readiness: map[string]httpserver.Check{
			"postgres": func(context.Context) error { return nil },
			"redis":    func(context.Context) error { return nil },
		},
	})

	require.Equal(t, http.StatusOK, get(t, mux, "/readyz").Code)
}

func TestReadyzNamesTheFailedCheck(t *testing.T) {
	mux := httpserver.Ops(httpserver.OpsConfig{
		Readiness: map[string]httpserver.Check{
			"redis": func(context.Context) error { return errors.New("down") },
		},
	})

	response := get(t, mux, "/readyz")

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), "redis")
}

func TestReadyToggle(t *testing.T) {
	ready := httpserver.NewReady()

	mux := httpserver.Ops(httpserver.OpsConfig{
		Readiness: map[string]httpserver.Check{"service": ready.Check()},
	})

	require.Equal(t, http.StatusServiceUnavailable, get(t, mux, "/readyz").Code)

	ready.Set(true)
	require.Equal(t, http.StatusOK, get(t, mux, "/readyz").Code)

	ready.Set(false)
	require.Equal(t, http.StatusServiceUnavailable, get(t, mux, "/readyz").Code)
}

func TestMetricsEndpoint(t *testing.T) {
	mux := httpserver.Ops(httpserver.OpsConfig{Registry: observability.NewRegistry()})

	response := get(t, mux, "/metrics")

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "go_goroutines")
}

func TestPprofIsOptional(t *testing.T) {
	require.Equal(t, http.StatusNotFound, get(t, httpserver.Ops(httpserver.OpsConfig{}), "/debug/pprof/").Code)

	enabled := httpserver.Ops(httpserver.OpsConfig{EnablePprof: true})
	require.Equal(t, http.StatusOK, get(t, enabled, "/debug/pprof/").Code)
}

func TestMetricsAbsentWithoutRegistry(t *testing.T) {
	require.Equal(t, http.StatusNotFound, get(t, httpserver.Ops(httpserver.OpsConfig{}), "/metrics").Code)
}
