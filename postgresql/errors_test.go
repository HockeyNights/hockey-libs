package postgresql_test

import (
	"errors"
	"testing"

	"github.com/HockeyNights/hockey-libs/apperr"
	"github.com/HockeyNights/hockey-libs/postgresql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestMapError(t *testing.T) {
	tests := map[string]struct {
		err  error
		kind apperr.Kind
	}{
		"nil":         {err: nil},
		"no rows":     {err: pgx.ErrNoRows, kind: apperr.KindNotFound},
		"wrapped":     {err: errors.Join(errors.New("query users"), pgx.ErrNoRows), kind: apperr.KindNotFound},
		"unique":      {err: &pgconn.PgError{Code: postgresql.CodeUniqueViolation}, kind: apperr.KindAlreadyExists},
		"foreign key": {err: &pgconn.PgError{Code: postgresql.CodeForeignKeyViolation}, kind: apperr.KindFailedPrecondition},
		"check":       {err: &pgconn.PgError{Code: postgresql.CodeCheckViolation}, kind: apperr.KindInvalidArgument},
		"deadlock":    {err: &pgconn.PgError{Code: postgresql.CodeDeadlockDetected}, kind: apperr.KindConflict},
		"canceled":    {err: &pgconn.PgError{Code: postgresql.CodeQueryCanceled}, kind: apperr.KindTimeout},
		"other":       {err: &pgconn.PgError{Code: "42601"}, kind: apperr.KindInternal},
		"plain":       {err: errors.New("boom"), kind: apperr.KindInternal},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			mapped := postgresql.MapError(test.err)

			if test.err == nil {
				require.NoError(t, mapped)
				return
			}

			require.Equal(t, test.kind, apperr.KindOf(mapped))
			require.ErrorIs(t, mapped, test.err)
		})
	}
}

func TestMapErrorHidesDatabaseDetailsFromMessage(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           postgresql.CodeUniqueViolation,
		Message:        `duplicate key value violates unique constraint "users_email_key"`,
		Detail:         "Key (email)=(victim@example.com) already exists.",
		ConstraintName: "users_email_key",
	}

	mapped := postgresql.MapError(pgErr)

	appErr, ok := apperr.As(mapped)
	require.True(t, ok)

	require.NotContains(t, appErr.Message, "victim@example.com")
	require.NotContains(t, appErr.Message, "users_email_key")
	require.Equal(t, "users_email_key", appErr.Fields["pg_constraint"])
}

func TestIsUniqueViolation(t *testing.T) {
	err := &pgconn.PgError{Code: postgresql.CodeUniqueViolation, ConstraintName: "users_email_key"}

	require.True(t, postgresql.IsUniqueViolation(err))
	require.True(t, postgresql.IsUniqueViolation(err, "users_email_key"))
	require.False(t, postgresql.IsUniqueViolation(err, "users_phone_key"))
	require.False(t, postgresql.IsUniqueViolation(errors.New("boom")))
}
