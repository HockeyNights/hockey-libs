package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/outbox"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type call struct {
	id      uuid.UUID
	backoff time.Duration
	reason  string
}

type fakeQueue struct {
	mu sync.Mutex

	pending   []outbox.Message
	completed []uuid.UUID
	retried   []call
	deferred  []call
	failed    []call
	cleaned   int
}

func (q *fakeQueue) Claim(_ context.Context, batchSize int32, _ time.Duration) ([]outbox.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		return nil, nil
	}

	size := min(int(batchSize), len(q.pending))
	batch := q.pending[:size]
	q.pending = q.pending[size:]

	return batch, nil
}

func (q *fakeQueue) Complete(_ context.Context, id uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completed = append(q.completed, id)

	return nil
}

func (q *fakeQueue) Retry(_ context.Context, id uuid.UUID, backoff time.Duration, reason string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.retried = append(q.retried, call{id: id, backoff: backoff, reason: reason})

	return nil
}

func (q *fakeQueue) Defer(_ context.Context, id uuid.UUID, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.deferred = append(q.deferred, call{id: id, backoff: delay})

	return nil
}

func (q *fakeQueue) Fail(_ context.Context, id uuid.UUID, reason string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed = append(q.failed, call{id: id, reason: reason})

	return nil
}

func (q *fakeQueue) DeleteOldFailed(_ context.Context, _ time.Duration) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cleaned++

	return 0, nil
}

type senderFunc func(context.Context, outbox.Message) error

func (f senderFunc) Send(ctx context.Context, m outbox.Message) error { return f(ctx, m) }

func runUntil(t *testing.T, queue *fakeQueue, sender outbox.Sender, cfg outbox.Config, done func() bool) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker, err := outbox.NewWorker(cfg, queue, sender, nil)
	require.NoError(t, err)

	finished := make(chan error, 1)
	go func() { finished <- worker.Run(ctx) }()

	deadline := time.After(2 * time.Second)
	for !done() {
		select {
		case <-deadline:
			t.Fatal("воркер не успел обработать очередь")
		case <-time.After(time.Millisecond):
		}
	}

	cancel()
	require.NoError(t, <-finished)
}

func message(kind string) outbox.Message {
	return outbox.Message{ID: uuid.Must(uuid.NewV7()), Kind: kind, Payload: []byte(`{}`), Attempts: 1, MaxAttempts: 5}
}

func fastConfig() outbox.Config {
	return outbox.Config{PollInterval: time.Millisecond, BatchSize: 10, CleanupInterval: time.Hour}
}

func TestSentMessageIsCompleted(t *testing.T) {
	queue := &fakeQueue{pending: []outbox.Message{message("email")}}
	sender := senderFunc(func(context.Context, outbox.Message) error { return nil })

	runUntil(t, queue, sender, fastConfig(), func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()

		return len(queue.completed) == 1
	})

	require.Empty(t, queue.retried)
	require.Empty(t, queue.failed)
}

func TestTemporaryErrorIsRetried(t *testing.T) {
	queue := &fakeQueue{pending: []outbox.Message{message("email")}}
	sender := senderFunc(func(context.Context, outbox.Message) error {
		return errors.New("connection refused")
	})

	runUntil(t, queue, sender, fastConfig(), func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()

		return len(queue.retried) == 1
	})

	require.Empty(t, queue.completed)
	require.Empty(t, queue.failed)
	require.Equal(t, "connection refused", queue.retried[0].reason)
}

func TestTerminalErrorFailsImmediately(t *testing.T) {
	queue := &fakeQueue{pending: []outbox.Message{message("nonsense")}}
	sender := senderFunc(func(context.Context, outbox.Message) error {
		return outbox.Terminal(errors.New("unknown kind"))
	})

	runUntil(t, queue, sender, fastConfig(), func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()

		return len(queue.failed) == 1
	})

	require.Empty(t, queue.completed)
	require.Empty(t, queue.retried, "терминальная ошибка не должна попадать в повторы")
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	cfg := fastConfig()
	cfg.BaseBackoff = time.Second
	cfg.MaxBackoff = 4 * time.Second

	for _, tc := range []struct {
		attempts int16
		expected time.Duration
	}{
		{attempts: 1, expected: time.Second},
		{attempts: 2, expected: 2 * time.Second},
		{attempts: 3, expected: 4 * time.Second},
		{attempts: 9, expected: 4 * time.Second},
	} {
		msg := message("email")
		msg.Attempts = tc.attempts

		queue := &fakeQueue{pending: []outbox.Message{msg}}
		sender := senderFunc(func(context.Context, outbox.Message) error {
			return errors.New("boom")
		})

		runUntil(t, queue, sender, cfg, func() bool {
			queue.mu.Lock()
			defer queue.mu.Unlock()

			return len(queue.retried) == 1
		})

		require.Equal(t, tc.expected, queue.retried[0].backoff, "attempts=%d", tc.attempts)
	}
}

func TestTerminalUnwrapsToOriginal(t *testing.T) {
	sentinel := errors.New("bad recipient")

	wrapped := outbox.Terminal(sentinel)

	require.True(t, outbox.IsTerminal(wrapped))
	require.ErrorIs(t, wrapped, sentinel, "Terminal не должен прятать исходную ошибку от errors.Is")
	require.False(t, outbox.IsTerminal(sentinel))
	require.NoError(t, outbox.Terminal(nil))
}

func TestConflictIsDeferredWithoutSpendingAttempt(t *testing.T) {

	queue := &fakeQueue{pending: []outbox.Message{message("email")}}
	sender := senderFunc(func(context.Context, outbox.Message) error {
		return outbox.Conflict(errors.New("delivery is already in progress"))
	})

	runUntil(t, queue, sender, fastConfig(), func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()

		return len(queue.deferred) == 1
	})

	require.Empty(t, queue.retried)
	require.Empty(t, queue.failed)
	require.Empty(t, queue.completed)
}

func TestWorkerRefusesLeaseShorterThanBatch(t *testing.T) {

	_, err := outbox.NewWorker(outbox.Config{
		BatchSize:         20,
		Concurrency:       1,
		SendTimeout:       30 * time.Second,
		VisibilityTimeout: 2 * time.Minute,
	}, &fakeQueue{}, senderFunc(nil), nil)

	require.ErrorContains(t, err, "аренда")
}

func TestWorkerAcceptsLeaseThatCoversBatch(t *testing.T) {
	_, err := outbox.NewWorker(outbox.Config{
		BatchSize:         20,
		Concurrency:       8,
		SendTimeout:       30 * time.Second,
		VisibilityTimeout: 2 * time.Minute,
	}, &fakeQueue{}, senderFunc(nil), nil)

	require.NoError(t, err)
}

func TestBatchIsDeliveredConcurrently(t *testing.T) {

	const size = 8

	pending := make([]outbox.Message, 0, size)
	for range size {
		pending = append(pending, message("email"))
	}
	queue := &fakeQueue{pending: pending}

	started := make(chan struct{}, size)
	release := make(chan struct{})
	sender := senderFunc(func(ctx context.Context, _ outbox.Message) error {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}

		return nil
	})

	cfg := fastConfig()
	cfg.Concurrency = size

	done := make(chan struct{})
	go func() {
		defer close(done)
		runUntil(t, queue, sender, cfg, func() bool {
			queue.mu.Lock()
			defer queue.mu.Unlock()

			return len(queue.completed) == size
		})
	}()

	for range size {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("отправки не пошли параллельно")
		}
	}
	close(release)
	<-done
}
