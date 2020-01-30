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
	setGlobalVariableID = "A4"

	// Args
	variableNameSetGlobalVariableID    = setGlobalVariableID + "-1"
	variableContentSetGlobalVariableID = setGlobalVariableID + "-2"
)

// SetGlobalVariable - Action
var SetGlobalVariable = actions.Action{
	ID:          setGlobalVariableID,
	Name:        "Set Global Variable",
	Description: "Sets the content of a global variable. If the variable does not exist, it will be created.",
	Run:         setGlobalVariableAction,
	Args: []actions.Arg{
		actions.Arg{
			ID:   variableNameSetGlobalVariableID,
			Name: "Name",
			Description: "The name of the variable. Must be uppercase, without spaces or special characters. " +
				"The unique special character allowed is the underscore ('_'). Example: THIS_IS_AN_EXAMPLE",
			ContentType: types.Text,
		},
		actions.Arg{
			ID:   variableContentSetGlobalVariableID,
			Name: "Variable content",
			Description: "The content of the variable. Optionally can be: a result of a previous action, " +
				"another variable or static content (setted by you).",
			ContentType: types.Any,
		},
	},
	ReturnedChainResultDescription: "The content setted to the variable.",
	ReturnedChainResultType:        types.Any,
}

func setGlobalVariableAction(previousResult *actions.ChainedResult, parentAction *data.UserAction, parentTaskName string) (result bool, chainedResult *actions.ChainedResult, err error) {
	var args *[]data.UserArg

	// The name of the variable
	var variableName string
	// The content of the variable
	var variableContent string

	args = &parentAction.Args

	for _, arg := range *args {
		switch arg.ID {
		case variableNameSetGlobalVariableID:
			{
				variableName = strings.TrimSpace(arg.Content)
			}
		case variableContentSetGlobalVariableID:
			variableContent = arg.Content
		default:
			{
				log.Println("[%s] Unrecongnized argument with the ID '%s' on the "+
					"action SetGlobalVariable\n", parentTaskName, arg.ID)
				return false, &actions.ChainedResult{}, ErrUnrecognizedArgID
			}
		}
	}

	if variableName == "" || variableContent == "" {
		return false, &actions.ChainedResult{}, errors.New("Error: variableName or variableContent empty")
	}

	variableType := types.GetType(variableContent)

	gv := &uservariables.GlobalVariable{
		Name:    variableName,
		Content: variableContent,
		Type:    variableType,
	}
	err = gv.WriteToFile()
	if err != nil {
		return false, &actions.ChainedResult{}, err
	}

	var varExists bool
	var index int
	for i, variable := range *uservariables.GlobalVariablesSlice {
		if variable.Name == variableName {
			varExists = true
			index = i
		}
	}
	if varExists {
		// If the variable already exists, replace it.
		(*uservariables.GlobalVariablesSlice)[index] = *gv
	} else {
		// If the variable does not exists, add it.
		// We can't append directly to the GVS because the function append doesn't returns a pointer.
		newGVS := append(*uservariables.GlobalVariablesSlice, *gv)
		uservariables.GlobalVariablesSlice = &newGVS
	}


	return true, &actions.ChainedResult{Result: variableContent, ResultType: variableType}, nil
}
