package uservariables

import "errors"

var (
	// ErrInvalidVariable is the error used when a user variable does not exist
	ErrInvalidVariable = errors.New("Error: unrecognized variable")
)
