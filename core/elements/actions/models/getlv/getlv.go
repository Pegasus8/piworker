package getlv

import (
	"errors"
	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/core/uservariables"
	"strings"
)

const (
	// Action
	actionID = "A8"

	// Args
	arg1ID = actionID + "-1"
)

// GetLocalVariable - Action
var GetLocalVariable = shared.Action{
	ID:   actionID,
	Name: "Get Local Variable",
	Description: "Obtains the content of a specific local variable and the same is passed to the next action. " +
		"Note: remind activate the 'Chained' option in the next action to receive this content.",
	Run: getLocalVariableAction,
	Args: []shared.Arg{
		shared.Arg{
			ID:          arg1ID,
			Name:        "Name",
			Description: "The name of the desired variable.",
			ContentType: types.Text,
		},
	},
	ReturnedChainResultDescription: "The content of the obtained variable.",
	ReturnedChainResultType:        types.Any,
}

func getLocalVariableAction(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	var args *[]data.UserArg

	// The name of the variable
	var variableName string

	args = &parentAction.Args

	for _, arg := range *args {
		switch arg.ID {
		case arg1ID:
			{
				variableName = strings.TrimSpace(arg.Content)
			}
		default:
			{
				return false, &shared.ChainedResult{}, shared.ErrUnrecognizedArgID
			}
		}
	}

	if variableName == "" {
		return false, &shared.ChainedResult{}, errors.New("Error: variableName empty")
	}

	localVariable, err := uservariables.GetLocalVariable(variableName, parentTaskID)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	return true, &shared.ChainedResult{Result: localVariable.Content, ResultType: types.Any}, nil
}
