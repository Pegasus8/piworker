package models

import (
	"errors"
	"github.com/Pegasus8/piworker/processment/types"
	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/actions"
	"github.com/Pegasus8/piworker/processment/uservariables"
	"log"
	"strings"
)

const (
	// Action
	getGlobalVariableID = "A7"

	// Args
	variableNameGetGlobalVariableID = "-1"
)

// GetGlobalVariable - Action
var GetGlobalVariable = actions.Action{
	ID:   getGlobalVariableID,
	Name: "Get Global Variable",
	Description: "Obtains the content of a specific global variable and the same is passed to the next action. " +
		"Note: remind activate the 'Chained' option in the next action to receive this content.",
	Run: getGlobalVariableAction,
	Args: []actions.Arg{
		actions.Arg{
			ID:          variableNameGetGlobalVariableID,
			Name:        "Name",
			Description: "The name of the desired variable.",
			ContentType: "text",
		},
	},
	ReturnedChainResultDescription: "The content of the obtained variable.",
	ReturnedChainResultType:        types.Any,
}

func getGlobalVariableAction(previousResult *actions.ChainedResult, parentAction *data.UserAction, parentTaskName string) (result bool, chainedResult *actions.ChainedResult, err error) {
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
				log.Println("[%s] Unrecongnized argument with the ID '%s' on the "+
					"action GetGlobalVariable\n", parentTaskName, arg.ID)
				return false, &actions.ChainedResult{}, ErrUnrecognizedArgID
			}
		}
	}

	if variableName == "" {
		return false, &actions.ChainedResult{}, errors.New("Error: variableName empty")
	}

	globalVariable, err := uservariables.GetGlobalVariable(variableName)
	if err != nil {
		return false, &actions.ChainedResult{}, err
	}

	return true, &actions.ChainedResult{Result: globalVariable.Content, ResultType: types.Any}, nil
}
