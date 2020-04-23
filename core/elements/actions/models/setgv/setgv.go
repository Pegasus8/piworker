package setgv

import (
	"errors"
	"strings"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/core/uservariables"
)

const actionID = "A4"

var actionArgs = []shared.Arg{
	shared.Arg{
		ID:   actionID + "-1",
		Name: "Name",
		Description: "The name of the variable. Must be uppercase, without spaces or special characters. " +
			"The unique special character allowed is the underscore ('_'). Example: THIS_IS_AN_EXAMPLE",
		ContentType: types.Text,
	},
	shared.Arg{
		ID:   actionID + "-2",
		Name: "Variable content",
		Description: "The content of the variable. Optionally can be: a result of a previous action, " +
			"another variable or static content (setted by you).",
		ContentType: types.Any,
	},
}

// SetGlobalVariable - Action
var SetGlobalVariable = shared.Action{
	ID:                             actionID,
	Name:                           "Set Global Variable",
	Description:                    "Sets the content of a global variable. If the variable does not exist, it will be created.",
	Run:                            action,
	Args:                           actionArgs,
	ReturnedChainResultDescription: "The content setted to the variable.",
	ReturnedChainResultType:        types.Any,
}

func action(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	var args *[]data.UserArg

	// The name of the variable
	var variableName string
	// The content of the variable
	var variableContent string

	args = &parentAction.Args

	err = shared.HandleCR(parentAction, actionArgs, previousResult)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	for _, arg := range *args {
		switch arg.ID {
		case actionArgs[0].ID:
			variableName = strings.TrimSpace(arg.Content)
		case actionArgs[1].ID:
			variableContent = arg.Content
		default:
			return false, &shared.ChainedResult{}, shared.ErrUnrecognizedArgID
		}
	}

	if variableName == "" || variableContent == "" {
		return false, &shared.ChainedResult{}, errors.New("Error: variableName or variableContent empty")
	}

	variableType := types.GetType(variableContent)

	gv := &uservariables.GlobalVariable{
		Name:    variableName,
		Content: variableContent,
		Type:    variableType,
	}
	err = gv.WriteToFile()
	if err != nil {
		return false, &shared.ChainedResult{}, err
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

	return true, &shared.ChainedResult{Result: variableContent, ResultType: variableType}, nil
}
