package grpcserver_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/grpcserver"
	"github.com/HockeyNights/hockey-libs/token"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	loginMethod   = "/auth.v1.AuthService/Login"
	profileMethod = "/user.v1.UserService/GetProfile"
	testSecret    = "01234567890123456789012345678901"
)

func unaryInfo(method string) *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: method}
}

func incomingContext(pairs ...string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(pairs...))
}

func okHandler(context.Context, any) (any, error) { return "ok", nil }

func newTokenManager(t *testing.T) *token.Manager {
	t.Helper()

	manager, err := token.NewManager(token.Config{Secret: testSecret, Issuer: "hockeynights"})
	require.NoError(t, err)

	return manager
}

func TestAuthInterceptorAllowsPublicMethods(t *testing.T) {
	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate: func(context.Context, string) (context.Context, error) {
			t.Fatal("authenticator must not be called for a public method")
			return nil, nil
		},
		PublicMethods: []string{loginMethod},
	})

	resp, err := interceptor(context.Background(), nil, unaryInfo(loginMethod), okHandler)

	require.NoError(t, err)
	require.Equal(t, "ok", resp)
}

func TestAuthInterceptorRejectsUnlistedMethodWithoutToken(t *testing.T) {
	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate:  func(context.Context, string) (context.Context, error) { return nil, nil },
		PublicMethods: []string{loginMethod},
	})

	_, err := interceptor(context.Background(), nil, unaryInfo(profileMethod), okHandler)

	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptorRejectsBadHeaders(t *testing.T) {
	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate: func(_ context.Context, raw string) (context.Context, error) {
			require.NotEmpty(t, raw)
			return context.Background(), nil
		},
	})

	for name, ctx := range map[string]context.Context{
		"no metadata":    context.Background(),
		"empty header":   incomingContext("authorization", ""),
		"wrong scheme":   incomingContext("authorization", "Basic dXNlcjpwYXNz"),
		"bearer no body": incomingContext("authorization", "Bearer   "),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := interceptor(ctx, nil, unaryInfo(profileMethod), okHandler)
			require.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

func TestBearerTokenAcceptsAnyCase(t *testing.T) {
	for _, header := range []string{"Bearer abc", "bearer abc", "BEARER abc"} {
		raw, err := grpcserver.BearerToken(incomingContext("authorization", header))
		require.NoError(t, err, header)
		require.Equal(t, "abc", raw)
	}
}

func TestAuthInterceptorHidesFailureReason(t *testing.T) {
	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate: func(context.Context, string) (context.Context, error) {
			return nil, errors.New("token expired at 2026-01-01, signed by key kid-7")
		},
	})

	_, err := interceptor(incomingContext("authorization", "Bearer abc"), nil, unaryInfo(profileMethod), okHandler)

	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unauthenticated, st.Code())
	require.Equal(t, "invalid credentials", st.Message())
	require.NotContains(t, st.Message(), "kid-7")
}

func TestAuthInterceptorOptionalPassesThroughWithoutToken(t *testing.T) {
	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate: func(context.Context, string) (context.Context, error) { return nil, nil },
		Optional:     true,
	})

	resp, err := interceptor(context.Background(), nil, unaryInfo(profileMethod), okHandler)

	require.NoError(t, err)
	require.Equal(t, "ok", resp)
}

func TestAuthInterceptorOptionalStillValidatesPresentToken(t *testing.T) {
	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate: func(context.Context, string) (context.Context, error) {
			return nil, errors.New("invalid signature")
		},
		Optional: true,
	})

	_, err := interceptor(incomingContext("authorization", "Bearer abc"), nil, unaryInfo(profileMethod), okHandler)

	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestTokenAuthenticatorPutsClaimsIntoContext(t *testing.T) {
	manager := newTokenManager(t)

	raw, _, err := manager.IssueAccess("user-1", token.IssueOptions{Roles: []string{"admin"}})
	require.NoError(t, err)

	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate: grpcserver.TokenAuthenticator(manager),
	})

	_, err = interceptor(
		incomingContext("authorization", "Bearer "+raw),
		nil,
		unaryInfo(profileMethod),
		func(ctx context.Context, _ any) (any, error) {
			require.Equal(t, "user-1", token.UserID(ctx))
			require.True(t, token.HasRole(ctx, "admin"))
			return "ok", nil
		},
	)

	require.NoError(t, err)
}

func TestTokenAuthenticatorRejectsRefreshToken(t *testing.T) {
	manager := newTokenManager(t)

	raw, _, err := manager.Issue("user-1", token.TypeRefresh, token.IssueOptions{TTL: time.Hour})
	require.NoError(t, err)

	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate: grpcserver.TokenAuthenticator(manager),
	})

	_, err = interceptor(incomingContext("authorization", "Bearer "+raw), nil, unaryInfo(profileMethod), okHandler)

	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestRequireRole(t *testing.T) {
	interceptor := grpcserver.RequireRole("admin", "support")

	authenticated := token.NewContext(context.Background(), &token.Claims{Roles: []string{"support"}})
	resp, err := interceptor(authenticated, nil, unaryInfo(profileMethod), okHandler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)

	other := token.NewContext(context.Background(), &token.Claims{Roles: []string{"user"}})
	_, err = interceptor(other, nil, unaryInfo(profileMethod), okHandler)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	_, err = interceptor(context.Background(), nil, unaryInfo(profileMethod), okHandler)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptorFailsWithoutAuthenticator(t *testing.T) {
	interceptor := grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{})

	_, err := interceptor(incomingContext("authorization", "Bearer abc"), nil, unaryInfo(profileMethod), okHandler)

	require.Equal(t, codes.Internal, status.Code(err))
}
