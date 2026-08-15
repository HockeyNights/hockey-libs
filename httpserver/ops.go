package httpserver

import (
	"context"
	"net/http"
	"net/http/pprof"
	"sync/atomic"

	"github.com/HockeyNights/hockey-libs/observability"
	"github.com/prometheus/client_golang/prometheus"
)

type Check func(ctx context.Context) error

type OpsConfig struct {
	Registry    *prometheus.Registry
	Readiness   map[string]Check
	EnablePprof bool
}

func Ops(cfg OpsConfig) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writePlain(w, http.StatusOK, "ok")
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		for name, check := range cfg.Readiness {
			if err := check(r.Context()); err != nil {
				writePlain(w, http.StatusServiceUnavailable, "not ready: "+name)
				return
			}
		}
		writePlain(w, http.StatusOK, "ok")
	})

	if cfg.Registry != nil {
		mux.Handle("GET /metrics", observability.MetricsHandler(cfg.Registry))
	}

	if cfg.EnablePprof {
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	}

	return mux
}

type Ready struct {
	ready atomic.Bool
}

func NewReady() *Ready {
	return &Ready{}
}

func (r *Ready) Set(ready bool) { r.ready.Store(ready) }

func (r *Ready) Check() Check {
	return func(context.Context) error {
		if r.ready.Load() {
			return nil
		}
		return errNotReady
	}
}

type notReadyError struct{}

func (notReadyError) Error() string { return "not ready" }

var errNotReady = notReadyError{}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
