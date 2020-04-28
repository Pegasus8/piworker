package getgv

import (
	"errors"
	"strings"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/core/uservariables"
)

const actionID = "A6"

var actionArgs = []shared.Arg{
	{
		ID:          actionID + "-1",
		Name:        "Name",
		Description: "The name of the desired variable.",
		ContentType: types.Text,
	},
}

// GetGlobalVariable - Action
var GetGlobalVariable = shared.Action{
	ID:   actionID,
	Name: "Get Global Variable",
	Description: "Obtains the content of a specific global variable and the same is passed to the next action. " +
		"Note: remind activate the 'Chained' option in the next action to receive this content.",
	Run:                            action,
	Args:                           actionArgs,
	ReturnedChainResultDescription: "The content of the obtained variable.",
	ReturnedChainResultType:        types.Any,
}

func action(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	var args *[]data.UserArg

	// The name of the variable
	var variableName string

	args = &parentAction.Args

	err = shared.HandleCR(parentAction, actionArgs, previousResult)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	for _, arg := range *args {
		switch arg.ID {
		case actionArgs[0].ID:
			variableName = strings.TrimSpace(arg.Content)
		default:
			return false, &shared.ChainedResult{}, shared.ErrUnrecognizedArgID
		}
	}

	if variableName == "" {
		return false, &shared.ChainedResult{}, errors.New("Error: variableName empty")
	}

	globalVariable, err := uservariables.GetGlobalVariable(variableName)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	return true, &shared.ChainedResult{Result: globalVariable.Content, ResultType: types.Any}, nil
}
