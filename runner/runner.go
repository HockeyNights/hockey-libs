package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

const DefaultShutdownTimeout = 30 * time.Second

type Component func(ctx context.Context) error

type namedComponent struct {
	name string
	run  Component
}

type Runner struct {
	logger          *slog.Logger
	components      []namedComponent
	shutdownTimeout time.Duration
}

func New(logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}

	return &Runner{
		logger:          logger,
		shutdownTimeout: DefaultShutdownTimeout,
	}
}

func (r *Runner) WithShutdownTimeout(timeout time.Duration) *Runner {
	if timeout > 0 {
		r.shutdownTimeout = timeout
	}
	return r
}

func (r *Runner) Add(name string, run Component) *Runner {
	r.components = append(r.components, namedComponent{name: name, run: run})
	return r
}

func (r *Runner) Run(ctx context.Context) error {
	if len(r.components) == 0 {
		return fmt.Errorf("runner: no components registered")
	}

	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(groupCtx)

	for _, component := range r.components {
		group.Go(func() error {
			defer cancel()

			r.logger.Info("component started", "component", component.name)

			if err := component.run(groupCtx); err != nil {
				r.logger.Error("component failed", "component", component.name, "error", err)
				return fmt.Errorf("runner: component %s: %w", component.name, err)
			}

			r.logger.Info("component stopped", "component", component.name)
			return nil
		})
	}

	done := make(chan error, 1)
	go func() { done <- group.Wait() }()

	select {
	case err := <-done:
		return err
	case <-groupCtx.Done():
	}

	timer := time.NewTimer(r.shutdownTimeout)
	defer timer.Stop()

	select {
	case err := <-done:
		return err
	case <-timer.C:
		r.logger.Error("shutdown timed out, exiting", "timeout", r.shutdownTimeout.String())
		return fmt.Errorf("runner: shutdown timed out after %s", r.shutdownTimeout)
	}
}

const exitCodeInterrupted = 130

func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	stopped := make(chan struct{})

	go func() {
		defer signal.Stop(signals)

		select {
		case <-signals:
			cancel()
		case <-stopped:
			return
		}

		select {
		case <-signals:
			os.Exit(exitCodeInterrupted)
		case <-stopped:
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(stopped)
			cancel()
		})
	}

	return ctx, stop
}
