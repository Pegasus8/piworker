package models

import (
	"errors"
	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/actions"
	"github.com/Pegasus8/piworker/processment/uservariables"
	"github.com/Pegasus8/piworker/utilities/typeconversion"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

const (
	// Action
	setLocalVariableID = "A5"

	// Args
	variableNameSetLocalVariableID    = setLocalVariableID + "-1"
	variableContentSetLocalVariableID = setLocalVariableID + "-2"
)

// SetLocalVariable - Action
var SetLocalVariable = actions.Action{
	ID:          setLocalVariableID,
	Name:        "Set Local Variable",
	Description: "Sets the content of a local variable. If the variable does not exist, it will be created.",
	Run:         setLocalVariableAction,
	Args: []actions.Arg{
		actions.Arg{
			ID:   variableNameSetLocalVariableID,
			Name: "Name",
			Description: "The name of the variable. Must be lowercase, without spaces or special characters. " +
				"The unique special character allowed is the underscore ('_'). Remind that local variables are only " +
				"valid on the task where are created. If you want share a variable between tasks use a global variable" +
				" instead. Example of variable: some_random_local_var",
			ContentType: "text",
		},
		actions.Arg{
			ID:   variableContentSetLocalVariableID,
			Name: "Variable content",
			Description: "The content of the variable. Optionally can be: a result of a previous action, " +
				"another variable or static content (setted by you).",
			ContentType: "text",
		},
	},
	ReturnedChainResultDescription: "The content setted to the variable.",
	ReturnedChainResultType:        reflect.String, // REVIEW This maybe is incorrect
	AcceptedChainResultDescription: "Any content.",
	AcceptedChainResultType:        reflect.String, // REVIEW This maybe is incorrect
}

func setLocalVariableAction(previousResult *actions.ChainedResult, parentAction *data.UserAction, parentTaskName string) (result bool, chainedResult *actions.ChainedResult, err error) {
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
					"action SetLocalVariable\n", parentTaskName, arg.ID)
				return false, &actions.ChainedResult{}, ErrUnrecognizedArgID
			}
		}
	}

	if parentAction.Chained {
		if reflect.ValueOf(previousResult.Result).IsNil() {
			log.Println(ErrEmptyChainedResult.Error())
		} else {
			if previousResult.ResultType == reflect.String {
				// Overwrite content of the variable
				variableContent = typeconversion.ConvertToString(previousResult.Result)
			} else {
				log.Printf("[%s] Type of previous ChainedResult (%s) differs with the required type (%s).\n", parentTaskName, previousResult.ResultType.String(), reflect.String.String())
			}
		}
	}

	if variableName == "" || variableContent == "" {
		return false, &actions.ChainedResult{}, errors.New("Error: variableName or variableContent empty")
	}

	// Try conversion of the variable
	var variableType int
	var pathRgx *regexp.Regexp
	_, err = strconv.ParseInt(variableContent, 10, 64)
	if err == nil {
		// If there is no error the type of the content is integer
		variableType = uservariables.TypeInt
		goto gvDefinition
	}
	_, err = strconv.ParseFloat(variableContent, 64)
	if err == nil {
		// If there is no error the type of the content is float
		variableType = uservariables.TypeFloat
		goto gvDefinition
	}
	_, err = strconv.ParseBool(variableContent)
	if err == nil {
		// If there is no error the type of the content is boolean
		variableType = uservariables.TypeBool
		goto gvDefinition
	}
	pathRgx = regexp.MustCompile(`^(:?\/)[\/+\w-?]+(\.[a-z]+)?$`)
	if pathRgx.MatchString(variableContent) {
		// If regex match the type of the content is a path.
		variableType = uservariables.TypePath
		goto gvDefinition
	}
	// If none of the other types match, then is a string.
	variableType = uservariables.TypeString

	gvDefinition:

	// We can't append directly to the LVS because the function append doesn't returns a pointer.
	newLVS := append(*uservariables.LocalVariablesSlice, uservariables.LocalVariable{
		Name:    variableName,
		Content: variableContent,
		Type:    variableType,
		ParentTaskName: parentTaskName,
	})
	uservariables.LocalVariablesSlice = &newLVS

	return true, &actions.ChainedResult{Result: variableContent, ResultType: reflect.String}, nil
}
