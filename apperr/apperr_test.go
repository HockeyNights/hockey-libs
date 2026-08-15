package apperr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/HockeyNights/hockey-libs/apperr"
	"github.com/stretchr/testify/require"
)

func TestKindOfFindsWrappedError(t *testing.T) {
	base := apperr.NotFound("user_not_found", "user not found")
	wrapped := fmt.Errorf("load profile: %w", fmt.Errorf("get user: %w", base))

	require.Equal(t, apperr.KindNotFound, apperr.KindOf(wrapped))
	require.Equal(t, "user_not_found", apperr.CodeOf(wrapped))
	require.True(t, apperr.Is(wrapped, apperr.KindNotFound))
}

func TestKindOfPlainError(t *testing.T) {
	require.Equal(t, apperr.KindUnknown, apperr.KindOf(errors.New("boom")))
	require.Equal(t, apperr.KindUnknown, apperr.KindOf(nil))
	require.Empty(t, apperr.CodeOf(errors.New("boom")))
}

func TestWrapKeepsOriginal(t *testing.T) {
	original := errors.New("connection refused")
	wrapped := apperr.Wrap(original, apperr.KindUnavailable, "db_down", "service unavailable")

	require.ErrorIs(t, wrapped, original)
	require.Equal(t, apperr.KindUnavailable, apperr.KindOf(wrapped))
}

func TestWrapNilReturnsNil(t *testing.T) {
	require.Nil(t, apperr.Wrap(nil, apperr.KindInternal, "x", "y"))
}

func TestWithFieldDoesNotMutateOriginal(t *testing.T) {
	base := apperr.Conflict("version_conflict", "version conflict")

	first := base.WithField("entity", "user")
	second := base.WithField("entity", "session")

	require.Empty(t, base.Fields)
	require.Equal(t, "user", first.Fields["entity"])
	require.Equal(t, "session", second.Fields["entity"])
}

func TestKindString(t *testing.T) {
	require.Equal(t, "not_found", apperr.KindNotFound.String())
	require.Equal(t, "unknown", apperr.KindUnknown.String())
	require.Equal(t, "rate_limited", apperr.KindRateLimited.String())
}
