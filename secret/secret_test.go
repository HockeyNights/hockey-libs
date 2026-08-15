package secret_test

import (
	"encoding/base64"
	"testing"

	"github.com/HockeyNights/hockey-libs/secret"
	"github.com/stretchr/testify/require"
)

func TestTokenIsUniqueAndURLSafe(t *testing.T) {
	seen := make(map[string]struct{}, 1000)

	for range 1000 {
		token, err := secret.Token(32)
		require.NoError(t, err)

		_, duplicate := seen[token]
		require.False(t, duplicate, "duplicate token generated")
		seen[token] = struct{}{}

		decoded, err := base64.RawURLEncoding.DecodeString(token)
		require.NoError(t, err)
		require.Len(t, decoded, 32)
	}
}

func TestTokenFallsBackToDefaultSize(t *testing.T) {
	token, err := secret.Token(0)
	require.NoError(t, err)

	decoded, err := base64.RawURLEncoding.DecodeString(token)
	require.NoError(t, err)
	require.Len(t, decoded, secret.DefaultTokenBytes)
}

func TestMatches(t *testing.T) {
	token := secret.MustToken(32)
	hash := secret.Hash(token)

	require.True(t, secret.Matches(token, hash))
	require.False(t, secret.Matches(secret.MustToken(32), hash))

	require.Equal(t, hash, secret.Hash(token))
	require.Len(t, hash, 64)
}

func TestNumericCode(t *testing.T) {
	code, err := secret.NumericCode(6)
	require.NoError(t, err)
	require.Len(t, code, 6)

	for _, char := range code {
		require.True(t, char >= '0' && char <= '9', "unexpected character %q", char)
	}

	_, err = secret.NumericCode(0)
	require.Error(t, err)
}

func TestNumericCodeUsesFullAlphabet(t *testing.T) {
	seen := make(map[rune]struct{}, 10)

	for range 200 {
		code, err := secret.NumericCode(6)
		require.NoError(t, err)

		for _, char := range code {
			seen[char] = struct{}{}
		}
	}

	require.Len(t, seen, 10)
}
