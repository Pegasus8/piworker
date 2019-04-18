package models

import "errors"

var (
	// ErrUnrecognizedArgID is the error used when an Arg from the user data file
	// uses an unrecognized ID.
	ErrUnrecognizedArgID = errors.New(
		"Error with the ID of an argument: unrecognized ID",
	)
)