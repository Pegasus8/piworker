package models

import (
	"errors"
	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/actions"
	"github.com/Pegasus8/piworker/processment/types"
	"github.com/Pegasus8/piworker/processment/uservariables"
	"log"
	"strings"
)

const (
	// Action
	getLocalVariableID = "A8"

	// Args
	variableNameGetLocalVariableID = "-1"
)

// GetLocalVariable - Action
var GetLocalVariable = actions.Action{
	ID:   getLocalVariableID,
	Name: "Get Local Variable",
	Description: "Obtains the content of a specific local variable and the same is passed to the next action. " +
		"Note: remind activate the 'Chained' option in the next action to receive this content.",
	Run: getLocalVariableAction,
	Args: []actions.Arg{
		actions.Arg{
			ID:          variableNameGetLocalVariableID,
			Name:        "Name",
			Description: "The name of the desired variable.",
			ContentType: types.Text,
		},
	},
	ReturnedChainResultDescription: "The content of the obtained variable.",
	ReturnedChainResultType:        types.Any,
}

func getLocalVariableAction(previousResult *actions.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *actions.ChainedResult, err error) {
	var args *[]data.UserArg

	// The name of the variable
	var variableName string

	args = &parentAction.Args

	for _, arg := range *args {
		switch arg.ID {
		case variableNameGetGlobalVariableID:
			{
				variableName = strings.TrimSpace(arg.Content)
			}
		default:
			{
				log.Println("[%s] Unrecognized argument with the ID '%s' on the "+
					"action GetLocalVariable\n", parentTaskID, arg.ID)
				return false, &actions.ChainedResult{}, ErrUnrecognizedArgID
			}
		}
	}

	if variableName == "" {
		return false, &actions.ChainedResult{}, errors.New("Error: variableName empty")
	}

	localVariable, err := uservariables.GetLocalVariable(variableName, parentTaskID)
	if err != nil {
		return false, &actions.ChainedResult{}, err
	}

	return true, &actions.ChainedResult{Result: localVariable.Content, ResultType: types.Any}, nil
}
