package shared

import "errors"

var (
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

	// ErrEmptyCRResult is the error used when the field `ChainedResult.Result` is empty.
	ErrEmptyCRResult = errors.New(
		"Data from received chained result empty",
	)
	// ErrEmptyCRResultType is the error used when the field `ChainedResult.ResultType` is empty.
	ErrEmptyCRResultType = errors.New(
		"The type of the chained result is empty",
	)
	// ErrEmptyArgToReplace is the error that represents an empty `UserAction.ArgumentToReplaceByCR` field.
	ErrEmptyArgToReplace = errors.New(
		"The argument to replace by the ChainedResult is empty",
	)
	// ErrCRTypeDiffers represents an incompatibility between a ChainedResult and an action argument.
	ErrCRTypeDiffers = errors.New(
		"The type of the arg is not compatible with the ChainedResult",
	)

	// ErrWrongUVFormat is the error that represents an incorrect format on the name of a user variable.
	ErrWrongUVFormat = errors.New(
		"The format of the user variable is incorrect",
	)
)
