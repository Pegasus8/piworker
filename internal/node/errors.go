package node

import "errors"

// Retryable is implemented by errors that the flow runtime is allowed to retry.
//
// By default an error is NOT retryable: the runtime only re-executes a node
// when its error explicitly reports Retryable() == true. This makes retries
// opt-in so that side-effecting actions (HTTP POST, shell commands, etc.) are
// never blindly re-run — preventing duplicate side effects — while genuinely
// transient failures (network blips, 5xx on idempotent requests) can still be
// retried by wrapping them with Transient.
type Retryable interface {
	Retryable() bool
}

// TransientError wraps an error to mark it as retryable by the runtime.
type TransientError struct {
	Err error
}

// Error implements the error interface.
func (e *TransientError) Error() string {
	if e.Err == nil {
		return "transient error"
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped error for errors.Is/errors.As.
func (e *TransientError) Unwrap() error {
	return e.Err
}

// Retryable reports that this error is safe to retry.
func (e *TransientError) Retryable() bool {
	return true
}

// Transient marks err as a transient, retryable failure. It returns nil when
// err is nil so it can be used inline (e.g. return node.Transient(err)).
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return &TransientError{Err: err}
}

// IsRetryable reports whether err (or any error it wraps) is marked retryable.
// Unmarked errors are treated as permanent and are never retried.
func IsRetryable(err error) bool {
	var r Retryable
	if errors.As(err, &r) {
		return r.Retryable()
	}
	return false
}
