package token_test

import (
	"strings"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/token"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testSecret = "01234567890123456789012345678901"

func newManager(t *testing.T) *token.Manager {
	t.Helper()

	manager, err := token.NewManager(token.Config{
		Secret:   testSecret,
		Issuer:   "hockeynights",
		Audience: []string{"mobile"},
	})
	require.NoError(t, err)

	return manager
}

func TestIssueAndParse(t *testing.T) {
	manager := newManager(t)

	raw, issued, err := manager.IssueAccess("user-1", token.IssueOptions{
		SessionID: "session-1",
		Roles:     []string{"admin"},
	})
	require.NoError(t, err)
	require.Equal(t, token.TypeAccess, issued.Type)

	claims, err := manager.ParseAccess(raw)
	require.NoError(t, err)

	require.Equal(t, "user-1", claims.UserID())
	require.Equal(t, "session-1", claims.SessionID)
	require.True(t, claims.HasRole("admin"))
	require.False(t, claims.HasRole("support"))
}

func TestParseRejectsWrongTokenType(t *testing.T) {
	manager := newManager(t)

	refresh, _, err := manager.Issue("user-1", token.TypeRefresh, token.IssueOptions{TTL: time.Hour})
	require.NoError(t, err)

	_, err = manager.ParseAccess(refresh)
	require.ErrorIs(t, err, token.ErrWrongType)

	claims, err := manager.Parse(refresh, token.TypeRefresh)
	require.NoError(t, err)
	require.Equal(t, token.TypeRefresh, claims.Type)
}

func TestParseRejectsExpiredToken(t *testing.T) {
	manager := newManager(t)

	raw, _, err := manager.IssueAccess("user-1", token.IssueOptions{TTL: -time.Hour})
	require.NoError(t, err)

	_, err = manager.ParseAccess(raw)
	require.ErrorIs(t, err, token.ErrInvalidToken)
}

func TestParseRejectsTamperedSignature(t *testing.T) {
	manager := newManager(t)

	raw, _, err := manager.IssueAccess("user-1", token.IssueOptions{})
	require.NoError(t, err)

	parts := strings.Split(raw, ".")
	require.Len(t, parts, 3)

	tampered := parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2]))

	_, err = manager.ParseAccess(tampered)
	require.ErrorIs(t, err, token.ErrInvalidToken)
}

func TestParseRejectsAlgNone(t *testing.T) {
	manager := newManager(t)

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, &token.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "attacker",
			Issuer:    "hockeynights",
			Audience:  []string{"mobile"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Type: token.TypeAccess,
	})

	raw, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = manager.ParseAccess(raw)
	require.ErrorIs(t, err, token.ErrInvalidToken)
}

func TestParseRejectsAlgorithmConfusion(t *testing.T) {
	privatePEM, publicPEM, err := token.GenerateEd25519Keys()
	require.NoError(t, err)

	verifier, err := token.NewManager(token.Config{
		Algorithm:    token.AlgEdDSA,
		PublicKeyPEM: publicPEM,
		Issuer:       "hockeynights",
	})
	require.NoError(t, err)

	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, &token.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "attacker",
			Issuer:    "hockeynights",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Type: token.TypeAccess,
	})

	raw, err := forged.SignedString([]byte(publicPEM))
	require.NoError(t, err)

	_, err = verifier.ParseAccess(raw)
	require.ErrorIs(t, err, token.ErrInvalidToken)

	issuer, err := token.NewManager(token.Config{
		Algorithm:     token.AlgEdDSA,
		PrivateKeyPEM: privatePEM,
		Issuer:        "hockeynights",
	})
	require.NoError(t, err)

	valid, _, err := issuer.IssueAccess("user-1", token.IssueOptions{})
	require.NoError(t, err)

	claims, err := verifier.ParseAccess(valid)
	require.NoError(t, err)
	require.Equal(t, "user-1", claims.UserID())
}

func TestParseRejectsForeignIssuerAndAudience(t *testing.T) {
	manager := newManager(t)

	foreign, err := token.NewManager(token.Config{
		Secret:   testSecret,
		Issuer:   "someone-else",
		Audience: []string{"mobile"},
	})
	require.NoError(t, err)

	raw, _, err := foreign.IssueAccess("user-1", token.IssueOptions{})
	require.NoError(t, err)

	_, err = manager.ParseAccess(raw)
	require.ErrorIs(t, err, token.ErrInvalidToken)
}

func TestNewManagerRejectsShortSecret(t *testing.T) {
	_, err := token.NewManager(token.Config{Secret: "short"})
	require.ErrorContains(t, err, "at least 32 bytes")
}

func TestNewManagerRejectsUnknownAlgorithm(t *testing.T) {
	_, err := token.NewManager(token.Config{Algorithm: "RS256", Secret: testSecret})
	require.ErrorContains(t, err, "unsupported algorithm")
}

func TestVerifyOnlyManagerCannotIssue(t *testing.T) {
	_, publicPEM, err := token.GenerateEd25519Keys()
	require.NoError(t, err)

	verifier, err := token.NewManager(token.Config{
		Algorithm:    token.AlgEdDSA,
		PublicKeyPEM: publicPEM,
	})
	require.NoError(t, err)

	_, _, err = verifier.IssueAccess("user-1", token.IssueOptions{})
	require.ErrorContains(t, err, "signing key is not configured")
}

func TestEdDSARequiresAKey(t *testing.T) {
	_, err := token.NewManager(token.Config{Algorithm: token.AlgEdDSA})
	require.ErrorContains(t, err, "requires JWT_PRIVATE_KEY or JWT_PUBLIC_KEY")
}

func TestIssueRequiresUserID(t *testing.T) {
	manager := newManager(t)

	_, _, err := manager.IssueAccess("", token.IssueOptions{})
	require.ErrorContains(t, err, "user id is required")
}

func TestTokenWithoutTTLIsRejectedForNonAccessTypes(t *testing.T) {
	manager := newManager(t)

	_, _, err := manager.Issue("user-1", token.TypeRefresh, token.IssueOptions{})

	require.ErrorContains(t, err, "TTL is required")
}

func TestDefaultTTLs(t *testing.T) {
	manager := newManager(t)

	require.Equal(t, 15*time.Minute, manager.AccessTTL())

	_, claims, err := manager.IssueAccess("user-1", token.IssueOptions{})
	require.NoError(t, err)

	require.WithinDuration(t, time.Now().Add(15*time.Minute), claims.ExpiresAt.Time, time.Minute)
}
