package outbox

import "errors"

func Terminal(err error) error {
	if err == nil {
		return nil
	}

	return terminalError{err}
}

func IsTerminal(err error) bool {
	var t terminalError

	return errors.As(err, &t)
}

type terminalError struct{ err error }

func (e terminalError) Error() string { return e.err.Error() }
func (e terminalError) Unwrap() error { return e.err }

func Conflict(err error) error {
	if err == nil {
		return nil
	}

	return conflictError{err}
}

func IsConflict(err error) bool {
	var c conflictError

	return errors.As(err, &c)
}

type conflictError struct{ err error }

func (e conflictError) Error() string { return e.err.Error() }
func (e conflictError) Unwrap() error { return e.err }
