package password_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/password"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fastParams() password.Params {
	return password.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1}
}

func TestHashAndVerify(t *testing.T) {
	hasher := password.NewHasher(fastParams())

	encoded, err := hasher.Hash(t.Context(), "correct horse battery staple")
	require.NoError(t, err)

	require.NoError(t, password.Verify("correct horse battery staple", encoded))
	require.ErrorIs(t, password.Verify("wrong password", encoded), password.ErrMismatch)
}

func TestHashIsSaltedPerCall(t *testing.T) {
	hasher := password.NewHasher(fastParams())

	first, err := hasher.Hash(t.Context(), "same")
	require.NoError(t, err)

	second, err := hasher.Hash(t.Context(), "same")
	require.NoError(t, err)

	require.NotEqual(t, first, second)
	require.NoError(t, password.Verify("same", first))
	require.NoError(t, password.Verify("same", second))
}

func TestHashFormat(t *testing.T) {
	hasher := password.NewHasher(password.Params{Memory: 8 * 1024, Iterations: 2, Parallelism: 1})

	encoded, err := hasher.Hash(t.Context(), "secret")
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(encoded, "$argon2id$v=19$m=8192,t=2,p=1$"), encoded)
	require.Len(t, strings.Split(encoded, "$"), 6)
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	for name, encoded := range map[string]string{
		"empty":            "",
		"plain text":       "not-a-hash",
		"bcrypt":           "$2a$10$abcdefghijklmnopqrstuv",
		"missing sections": "$argon2id$v=19$m=8192,t=1,p=1$c2FsdA",
		"bad base64":       "$argon2id$v=19$m=8192,t=1,p=1$!!!$!!!",
		"zero memory":      "$argon2id$v=19$m=0,t=1,p=1$c2FsdHNhbHQ$aGFzaGhhc2g",
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, password.Verify("secret", encoded), password.ErrInvalidHash)
		})
	}
}

func TestVerifyRejectsUnsupportedVersion(t *testing.T) {
	err := password.Verify("secret", "$argon2id$v=16$m=8192,t=1,p=1$c2FsdHNhbHQ$aGFzaGhhc2g")
	require.ErrorIs(t, err, password.ErrUnsupportedVersion)
}

func TestVerifyWorksAcrossParameterChange(t *testing.T) {
	weak := password.NewHasher(password.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1})

	encoded, err := weak.Hash(t.Context(), "secret")
	require.NoError(t, err)

	require.NoError(t, password.Verify("secret", encoded))

	strong := password.NewHasher(password.Params{Memory: 16 * 1024, Iterations: 2, Parallelism: 1})

	needsRehash, err := strong.NeedsRehash(encoded)
	require.NoError(t, err)
	require.True(t, needsRehash)

	sameStrength, err := weak.NeedsRehash(encoded)
	require.NoError(t, err)
	require.False(t, sameStrength)
}

func TestNewHasherFillsZeroParams(t *testing.T) {
	hasher := password.NewHasher(password.Params{})

	encoded, err := hasher.Hash(t.Context(), "secret")
	require.NoError(t, err)
	require.NoError(t, password.Verify("secret", encoded))
}

func TestHasherLimitsConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("тест меряет время")
	}

	params := password.Params{Memory: 64 * 1024, Iterations: 4, Parallelism: 1}
	hasher := password.NewHasher(params, password.WithMaxConcurrent(1))

	single := time.Hour
	for range 3 {
		start := time.Now()
		_, err := hasher.Hash(context.Background(), "measure")
		require.NoError(t, err)

		single = min(single, time.Since(start))
	}

	const calls = 4

	var group sync.WaitGroup
	start := time.Now()

	for range calls {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := hasher.Hash(context.Background(), "concurrent")
			assert.NoError(t, err)
		}()
	}
	group.Wait()

	total := time.Since(start)

	want := single * calls * 3 / 4
	require.GreaterOrEqual(t, total, want,
		"четыре вычисления при одном слоте прошли за %s, одиночное — %s: похоже, они шли параллельно",
		total, single)
}

func TestHasherGivesUpOnDeadContext(t *testing.T) {
	hasher := password.NewHasher(fastParams(), password.WithMaxConcurrent(1))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := hasher.Hash(ctx, "should not wait forever")
	require.ErrorIs(t, err, password.ErrBusy)
}

func TestHasherVerifyRespectsCancelledContext(t *testing.T) {
	hasher := password.NewHasher(fastParams())

	encoded, err := hasher.Hash(t.Context(), "secret")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, hasher.Verify(ctx, "secret", encoded), password.ErrBusy)
}
