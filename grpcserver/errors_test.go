package grpcserver_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/HockeyNights/hockey-libs/apperr"
	"github.com/HockeyNights/hockey-libs/grpcserver"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapErrorKinds(t *testing.T) {
	tests := map[string]struct {
		err  error
		code codes.Code
	}{
		"not found":      {apperr.NotFound("user_not_found", "user not found"), codes.NotFound},
		"invalid":        {apperr.InvalidArgument("bad_email", "invalid email"), codes.InvalidArgument},
		"exists":         {apperr.AlreadyExists("email_taken", "email is taken"), codes.AlreadyExists},
		"unauthed":       {apperr.Unauthenticated("bad_token", "invalid token"), codes.Unauthenticated},
		"forbidden":      {apperr.PermissionDenied("no_access", "no access"), codes.PermissionDenied},
		"conflict":       {apperr.Conflict("stale", "stale version"), codes.Aborted},
		"rate limited":   {apperr.RateLimited("too_many", "slow down"), codes.ResourceExhausted},
		"internal":       {apperr.Internal("boom", "boom"), codes.Internal},
		"plain error":    {errors.New("boom"), codes.Internal},
		"canceled":       {context.Canceled, codes.Canceled},
		"deadline":       {context.DeadlineExceeded, codes.DeadlineExceeded},
		"already stat":   {status.Error(codes.Unauthenticated, "invalid credentials"), codes.Unauthenticated},
		"wrapped apperr": {fmt.Errorf("service: %w", apperr.NotFound("x", "x")), codes.NotFound},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.code, status.Code(grpcserver.MapError(test.err)))
		})
	}

	require.NoError(t, grpcserver.MapError(nil))
}

func TestMapErrorHidesInternalDetails(t *testing.T) {
	err := apperr.Wrap(
		errors.New(`pq: relation "users" does not exist`),
		apperr.KindInternal,
		"database_error",
		"failed to query users table",
	)

	st, ok := status.FromError(grpcserver.MapError(err))
	require.True(t, ok)

	require.Equal(t, codes.Internal, st.Code())
	require.Equal(t, "internal server error", st.Message())
	require.NotContains(t, st.Message(), "users")
	require.Empty(t, st.Details())
}

func TestMapErrorPutsCodeInDetails(t *testing.T) {
	st, ok := status.FromError(grpcserver.MapError(apperr.AlreadyExists("email_taken", "email is taken")))
	require.True(t, ok)

	require.Equal(t, "email is taken", st.Message())
	require.Len(t, st.Details(), 1)

	info, ok := st.Details()[0].(*errdetails.ErrorInfo)
	require.True(t, ok)
	require.Equal(t, "email_taken", info.GetReason())
}

func TestErrorMappingUnaryInterceptor(t *testing.T) {
	interceptor := grpcserver.ErrorMappingUnaryInterceptor()

	_, err := interceptor(
		context.Background(),
		nil,
		unaryInfo("/test.Service/Method"),
		func(context.Context, any) (any, error) {
			return nil, apperr.NotFound("user_not_found", "user not found")
		},
	)

	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestErrorMappingPassesSuccessThrough(t *testing.T) {
	interceptor := grpcserver.ErrorMappingUnaryInterceptor()

	resp, err := interceptor(
		context.Background(),
		nil,
		unaryInfo("/test.Service/Method"),
		func(context.Context, any) (any, error) { return "ok", nil },
	)

	require.NoError(t, err)
	require.Equal(t, "ok", resp)
}
