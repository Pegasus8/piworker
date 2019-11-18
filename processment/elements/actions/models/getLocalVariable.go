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
			ContentType: "text",
		},
	},
	ReturnedChainResultDescription: "The content of the obtained variable.",
	ReturnedChainResultType:        types.TypeAny,
	AcceptedChainResultDescription: "Name of the variable",
	AcceptedChainResultType:        types.TypeString,
}

func getLocalVariableAction(previousResult *actions.ChainedResult, parentAction *data.UserAction, parentTaskName string) (result bool, chainedResult *actions.ChainedResult, err error) {
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
					"action GetLocalVariable\n", parentTaskName, arg.ID)
				return false, &actions.ChainedResult{}, ErrUnrecognizedArgID
			}
		}
	}

	if parentAction.Chained {
		if previousResult.Result == "" {
			log.Println(ErrEmptyChainedResult.Error())
		} else {
			if previousResult.ResultType == types.TypeString {
				// Overwrite name of the variable
				variableName = previousResult.Result
			} else {
				log.Printf("[%s] Type of previous ChainedResult (%d) differs with the required type (%s).\n", parentTaskName, previousResult.ResultType, types.TypeString)
			}
		}
	}

	if variableName == "" {
		return false, &actions.ChainedResult{}, errors.New("Error: variableName empty")
	}

	localVariable, err := uservariables.GetLocalVariable(variableName, parentTaskName)
	if err != nil {
		return false, &actions.ChainedResult{}, err
	}

	return true, &actions.ChainedResult{Result: localVariable.Content, ResultType: types.TypeAny}, nil
}
