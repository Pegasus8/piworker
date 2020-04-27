package setlv

import (
	"errors"
	"strings"
	"sync"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/core/uservariables"
)

const actionID = "A5"

var actionArgs = []shared.Arg{
	shared.Arg{
		ID:   actionID + "-1",
		Name: "Name",
		Description: "The name of the variable. Must be lowercase, without spaces or special characters. " +
			"The unique special character allowed is the underscore ('_'). Remind that local variables are only " +
			"valid on the task where are created. If you want share a variable between tasks use a global variable" +
			" instead. Example of variable: some_random_local_var",
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

// SetLocalVariable - Action
var SetLocalVariable = shared.Action{
	ID:                             actionID,
	Name:                           "Set Local Variable",
	Description:                    "Sets the content of a local variable. If the variable does not exist, it will be created.",
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
			{
				return false, &shared.ChainedResult{}, shared.ErrUnrecognizedArgID
			}
		}
	}

	if variableName == "" || variableContent == "" {
		return false, &shared.ChainedResult{}, errors.New("Error: variableName or variableContent empty")
	}

	variableType := types.GetType(variableContent)

	lv := &uservariables.LocalVariable{
		Name:         variableName,
		Content:      variableContent,
		Type:         variableType,
		ParentTaskID: parentTaskID,
		RWMutex:      &sync.RWMutex{},
	}
	err = lv.WriteToFile()
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	var varExists bool
	var index int
	for i, variable := range *uservariables.LocalVariablesSlice {
		if variable.Name == variableName {
			varExists = true
			index = i
		}
	}
	if varExists {
		// If the variable already exists, replace it.
		(*uservariables.LocalVariablesSlice)[index] = *lv
	} else {
		// If the variable does not exists, add it.
		// We can't append directly to the LVS because the function append doesn't returns a pointer.
		newLVS := append(*uservariables.LocalVariablesSlice, *lv)
		uservariables.LocalVariablesSlice = &newLVS
	}

	return true, &shared.ChainedResult{Result: variableContent, ResultType: variableType}, nil
}
