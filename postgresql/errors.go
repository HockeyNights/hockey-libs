package postgresql

import (
	"errors"

	"github.com/HockeyNights/hockey-libs/apperr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	CodeUniqueViolation      = "23505"
	CodeForeignKeyViolation  = "23503"
	CodeNotNullViolation     = "23502"
	CodeCheckViolation       = "23514"
	CodeExclusionViolation   = "23P01"
	CodeSerializationFailure = "40001"
	CodeDeadlockDetected     = "40P01"
	CodeQueryCanceled        = "57014"
	CodeLockNotAvailable     = "55P03"
)

func MapError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.Wrap(err, apperr.KindNotFound, "not_found", "resource not found")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return apperr.Wrap(err, apperr.KindInternal, "database_error", "internal error")
	}

	mapped := func(kind apperr.Kind, code, message string) error {
		return apperr.Wrap(err, kind, code, message).WithField("pg_code", pgErr.Code).
			WithField("pg_constraint", pgErr.ConstraintName)
	}

	switch pgErr.Code {
	case CodeUniqueViolation:
		return mapped(apperr.KindAlreadyExists, "already_exists", "resource already exists")
	case CodeForeignKeyViolation:
		return mapped(apperr.KindFailedPrecondition, "foreign_key_violation", "related resource is missing")
	case CodeNotNullViolation, CodeCheckViolation, CodeExclusionViolation:
		return mapped(apperr.KindInvalidArgument, "constraint_violation", "request violates a data constraint")
	case CodeSerializationFailure, CodeDeadlockDetected, CodeLockNotAvailable:
		return mapped(apperr.KindConflict, "write_conflict", "concurrent update, please retry")
	case CodeQueryCanceled:
		return mapped(apperr.KindTimeout, "query_canceled", "request timed out")
	default:
		return mapped(apperr.KindInternal, "database_error", "internal error")
	}
}

func IsUniqueViolation(err error, constraints ...string) bool {
	pgErr, ok := asPgError(err)
	if !ok || pgErr.Code != CodeUniqueViolation {
		return false
	}

	if len(constraints) == 0 {
		return true
	}

	for _, constraint := range constraints {
		if pgErr.ConstraintName == constraint {
			return true
		}
	}

	return false
}

func IsForeignKeyViolation(err error) bool {
	pgErr, ok := asPgError(err)
	return ok && pgErr.Code == CodeForeignKeyViolation
}

func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func asPgError(err error) (*pgconn.PgError, bool) {
	var pgErr *pgconn.PgError
	ok := errors.As(err, &pgErr)
	return pgErr, ok
}
