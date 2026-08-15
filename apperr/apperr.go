package apperr

import (
	"errors"
	"fmt"
	"maps"
)

type Kind uint8

const (
	KindUnknown Kind = iota
	KindInvalidArgument
	KindUnauthenticated
	KindPermissionDenied
	KindNotFound
	KindAlreadyExists
	KindConflict
	KindFailedPrecondition
	KindRateLimited
	KindTimeout
	KindUnavailable
	KindInternal
)

func (k Kind) String() string {
	switch k {
	case KindInvalidArgument:
		return "invalid_argument"
	case KindUnauthenticated:
		return "unauthenticated"
	case KindPermissionDenied:
		return "permission_denied"
	case KindNotFound:
		return "not_found"
	case KindAlreadyExists:
		return "already_exists"
	case KindConflict:
		return "conflict"
	case KindFailedPrecondition:
		return "failed_precondition"
	case KindRateLimited:
		return "rate_limited"
	case KindTimeout:
		return "timeout"
	case KindUnavailable:
		return "unavailable"
	case KindInternal:
		return "internal"
	default:
		return "unknown"
	}
}

type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  map[string]string

	err error
}

func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func Wrap(err error, kind Kind, code, message string) *Error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Code: code, Message: message, err: err}
}

func (e *Error) Error() string {
	switch {
	case e.err != nil && e.Message != "":
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.err)
	case e.err != nil:
		return fmt.Sprintf("%s: %v", e.Code, e.err)
	case e.Message != "":
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	default:
		return e.Code
	}
}

func (e *Error) Unwrap() error { return e.err }

func (e *Error) WithField(key, value string) *Error {
	clone := *e
	clone.Fields = make(map[string]string, len(e.Fields)+1)
	maps.Copy(clone.Fields, e.Fields)
	clone.Fields[key] = value
	return &clone
}

func KindOf(err error) Kind {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Kind
	}
	return KindUnknown
}

func CodeOf(err error) string {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return ""
}

func As(err error) (*Error, bool) {
	var appErr *Error
	ok := errors.As(err, &appErr)
	return appErr, ok
}

func Is(err error, kind Kind) bool {
	return KindOf(err) == kind
}

func InvalidArgument(code, message string) *Error {
	return New(KindInvalidArgument, code, message)
}

func Unauthenticated(code, message string) *Error {
	return New(KindUnauthenticated, code, message)
}

func PermissionDenied(code, message string) *Error {
	return New(KindPermissionDenied, code, message)
}

func NotFound(code, message string) *Error {
	return New(KindNotFound, code, message)
}

func AlreadyExists(code, message string) *Error {
	return New(KindAlreadyExists, code, message)
}

func Conflict(code, message string) *Error {
	return New(KindConflict, code, message)
}

func FailedPrecondition(code, message string) *Error {
	return New(KindFailedPrecondition, code, message)
}

func RateLimited(code, message string) *Error {
	return New(KindRateLimited, code, message)
}

func Internal(code, message string) *Error {
	return New(KindInternal, code, message)
}
