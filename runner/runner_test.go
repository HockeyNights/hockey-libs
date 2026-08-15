package runner_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/HockeyNights/hockey-libs/runner"
	"github.com/stretchr/testify/require"
)

func blockUntilDone(started *atomic.Bool) runner.Component {
	return func(ctx context.Context) error {
		started.Store(true)
		<-ctx.Done()
		return nil
	}
}

func TestRunStopsAllComponentsOnContextCancel(t *testing.T) {
	var first, second atomic.Bool

	r := runner.New(logger.Discard()).
		Add("first", blockUntilDone(&first)).
		Add("second", blockUntilDone(&second))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	require.Eventually(t, func() bool { return first.Load() && second.Load() }, time.Second, time.Millisecond)

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop after context cancel")
	}
}

func TestRunStopsEverythingWhenOneComponentFails(t *testing.T) {
	var sibling atomic.Bool
	failure := errors.New("listen: address already in use")

	r := runner.New(logger.Discard()).
		Add("healthy", blockUntilDone(&sibling)).
		Add("broken", func(context.Context) error { return failure })

	err := r.Run(context.Background())

	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "broken")
}

func TestRunStopsWhenComponentReturnsNil(t *testing.T) {
	var sibling atomic.Bool

	r := runner.New(logger.Discard()).
		Add("short-lived", func(context.Context) error { return nil }).
		Add("long-lived", blockUntilDone(&sibling))

	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background()) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop after a component returned")
	}
}

func TestRunTimesOutOnStuckComponent(t *testing.T) {
	r := runner.New(logger.Discard()).
		WithShutdownTimeout(100*time.Millisecond).
		Add("stuck", func(context.Context) error {
			select {}
		})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := r.Run(ctx)

	require.ErrorContains(t, err, "shutdown timed out")
}

func TestRunRequiresComponents(t *testing.T) {
	require.Error(t, runner.New(logger.Discard()).Run(context.Background()))
}

func TestSignalContextStopIsIdempotent(t *testing.T) {
	ctx, stop := runner.SignalContext(context.Background())

	stop()
	stop()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled by stop")
	}
}
