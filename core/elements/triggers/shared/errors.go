package shared

import "errors"

var (
	// ErrUnrecognizedArgID is the error used when an Arg from the user data file
	// uses an unrecognized ID.
	ErrUnrecognizedArgID = errors.New(
		"Error with the ID of an argument: unrecognized ID",
	)

	// ErrUnrecognizedTimeFormat is the error used when the content of the Arg
	// ´T4-1´ (EveryXTime trigger) uses an unrecognized string.
	ErrUnrecognizedTimeFormat = errors.New(
		"Error with the time unit used: unrecognized unit",
	)
)
