package uservariables

import "errors"

var (
	// ErrInvalidVariable is the error used when a user variable does not exist
	ErrInvalidVariable = errors.New("Error: unrecognized variable")

	// ErrInvalidParent is the error used when a user local variable is used
	// on a different task that the task who creates it.
	ErrInvalidParent = errors.New("Error: the local variable used does not belong to this task")
)
