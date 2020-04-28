package shared

import (
	"github.com/Pegasus8/piworker/core/data"
)

// HandleCR checks if the usage of the `ChainedResult` it's enabled and, if it is,
// the content of the argument chained will be replaced with the content of the `ChainedResult`.
func HandleCR(userAction *data.UserAction, actionArgs []Arg, cr *ChainedResult) error {
	if !userAction.Chained {
		return nil
	}
	if cr.Result == "" {
		return ErrEmptyCRResult
	}
	if cr.ResultType == "" {
		return ErrEmptyCRResultType
	}
	if userAction.ArgumentToReplaceByCR == "" {
		return ErrEmptyArgToReplace
	}

	var argFound bool
	for i, arg := range userAction.Args {
		if arg.ID == userAction.ArgumentToReplaceByCR {
			argFound = true
			for _, pwArg := range actionArgs {
				if arg.ID == pwArg.ID {
					// Only use the content of the CR if the type is compatible.
					if pwArg.ContentType == cr.ResultType {
						userAction.Args[i].Content = cr.Result
					} else {
						return ErrCRTypeDiffers
					}

					break
				}
			}

			break
		}
	}

	if !argFound {
		return ErrUnrecognizedArgID
	}

	return nil
}
