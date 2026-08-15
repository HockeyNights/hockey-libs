package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type Config struct {
	Address string       `env:"HTTP_ADDRESS" envDefault:":8080"`
	Logger  *slog.Logger `env:"-"`
	Handler http.Handler `env:"-"`

	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" envDefault:"5s"`
	ReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"30s"`
	WriteTimeout      time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
	ShutdownTimeout   time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"10s"`
}

type Server struct {
	cfg Config
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg.withDefaults()}
}

func (s *Server) Run(ctx context.Context) error {
	if s.cfg.Handler == nil {
		return errors.New("httpserver: handler is required")
	}

	server := &http.Server{
		Addr:              s.cfg.Address,
		Handler:           s.cfg.Handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}

	listener, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		s.cfg.Logger.Info("http server started", "address", listener.Addr().String())
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		s.cfg.Logger.Warn("http server forced to stop", "error", err)
		_ = server.Close()
	} else {
		s.cfg.Logger.Info("http server stopped")
	}

	return <-errCh
}

func (c Config) withDefaults() Config {
	if c.Address == "" {
		c.Address = ":8080"
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	c.ReadHeaderTimeout = timeoutOrDefault(c.ReadHeaderTimeout, 5*time.Second)
	c.ReadTimeout = timeoutOrDefault(c.ReadTimeout, 30*time.Second)
	c.WriteTimeout = timeoutOrDefault(c.WriteTimeout, 30*time.Second)
	c.IdleTimeout = timeoutOrDefault(c.IdleTimeout, 60*time.Second)
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	return c
}

func timeoutOrDefault(value, fallback time.Duration) time.Duration {
	switch {
	case value == 0:
		return fallback
	case value < 0:
		return 0
	default:
		return value
	}
}
