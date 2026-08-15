package grpcserver_test

import (
	"context"
	"testing"

	"github.com/HockeyNights/hockey-libs/grpcserver"
	"github.com/HockeyNights/hockey-libs/token"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const internalMethod = "/notification.v1.NotificationService/SendEmail"

func internalManager(t *testing.T) *token.Manager {
	t.Helper()

	manager, err := token.NewManager(token.Config{
		Secret: "секрет-длиной-заведомо-больше-32-байт",
		Issuer: "hockeynights",
	})
	require.NoError(t, err)

	return manager
}

func callInternal(t *testing.T, manager *token.Manager, method, rawToken string) error {
	t.Helper()

	interceptor := grpcserver.InternalAuthUnaryInterceptor(grpcserver.InternalAuthConfig{
		Manager: manager,
		Methods: []string{internalMethod},
	})

	ctx := context.Background()
	if rawToken != "" {
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+rawToken))
	}

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method},
		func(context.Context, any) (any, error) { return "ok", nil })

	return err
}

func TestInternalMethodRejectsCallWithoutToken(t *testing.T) {
	err := callInternal(t, internalManager(t), internalMethod, "")

	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestInternalMethodRejectsUserToken(t *testing.T) {

	manager := internalManager(t)

	raw, _, err := manager.IssueAccess("11111111-1111-1111-1111-111111111111", token.IssueOptions{})
	require.NoError(t, err)

	err = callInternal(t, manager, internalMethod, raw)

	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestInternalMethodAcceptsServiceToken(t *testing.T) {
	manager := internalManager(t)

	raw, _, err := manager.IssueAccess("auth-service", token.IssueOptions{
		Roles: []string{token.RoleService},
	})
	require.NoError(t, err)

	require.NoError(t, callInternal(t, manager, internalMethod, raw))
}

func TestInternalMethodRejectsTokenSignedByAnotherSecret(t *testing.T) {
	foreign, err := token.NewManager(token.Config{
		Secret: "совершенно-другой-секрет-тоже-длиннее-32",
		Issuer: "hockeynights",
	})
	require.NoError(t, err)

	raw, _, err := foreign.IssueAccess("auth-service", token.IssueOptions{
		Roles: []string{token.RoleService},
	})
	require.NoError(t, err)

	err = callInternal(t, internalManager(t), internalMethod, raw)

	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestOtherMethodsPassThroughUntouched(t *testing.T) {

	require.NoError(t, callInternal(t, internalManager(t), "/auth.v1.AuthService/Login", ""))
}
