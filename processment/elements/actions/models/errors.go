package models

import "errors"

var  (
	// ErrUnrecognizedArgID is the error used when an Arg from the user data file
	// uses an unrecognized ID.
	ErrUnrecognizedArgID = errors.New(
		"Error with the ID of an argument: unrecognized ID",
	)

	// ErrUnrecognizedWritingMode is the error used when the mode inserted by the user 
	// (on the action `WriteTextFile`) is out of the list of modes.
	ErrUnrecognizedWritingMode = errors.New(
		"Error with the writing mode: unrecognized mode",
	)

	// ErrEmptyChainedResult is the error used when a `ChainedResult.Result` is empty.
	ErrEmptyChainedResult = errors.New(
		"Data from received chained result empty",
	)
)