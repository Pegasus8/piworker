package models

import (
	"os/exec"
	"strings"
	"time"
	"reflect"
	"log"
	"errors"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/actions"
	"github.com/Pegasus8/piworker/utilities/files"
	"github.com/Pegasus8/piworker/utilities/typeconversion"
)

// ID's
const (
	// Action
	executeCommandID = "A3"

	// Args
	commandExecuteCommandID   = "A3-1"
	argumentsExecuteCommandID = "A3-2"
)

// ExecuteCommand - Action
var ExecuteCommand = actions.Action{
	ID:          executeCommandID,
	Name:        "",
	Description: "",
	Run:         executeCommand,
	Args: []actions.Arg{
		actions.Arg{
			ID:          commandExecuteCommandID,
			Name:        "Command",
			Description: "The command to execute.",
			// Content:     "",
			ContentType: "text",
		},
		actions.Arg{
			ID:   argumentsExecuteCommandID,
			Name: "Arguments",
			Description: "The arguments of the command provided, separated" +
				" by a comma.",
			// Content:     "",
			ContentType: "text",
		},
	},
	ReturnedChainResultDescription: "The command to execute.",
	ReturnedChainResultType: reflect.String,
	AcceptedChainResultDescription: "The output of the command executed.",
	AcceptedChainResultType: reflect.String,
}

func executeCommand(previousResult *actions.ChainedResult, parentAction *data.UserAction) (result bool, chainedResult *actions.ChainedResult, err error) {
	var args *[]data.UserArg

	// Command
	var command string
	// Command args
	var commandArgs []string

	args = &parentAction.Args

	for _, arg := range *args {
		switch arg.ID {
		case commandExecuteCommandID:
				command = strings.TrimSpace(arg.Content)
		case argumentsExecuteCommandID:
				commandArgs = strings.Split(arg.Content, ",")
		default:
			return false, &actions.ChainedResult{}, ErrUnrecognizedArgID
		}
	}

	if parentAction.Chained {
		if reflect.ValueOf(previousResult.Result).IsNil() {
			log.Println(ErrEmptyChainedResult.Error())
		} else {
			if previousResult.ResultType == reflect.String {
				// Overwrite command
				command = typeconversion.ConvertToString(previousResult.Result)
			} else {
				log.Printf("Type of previous ChainedResult (%s) differs with the required type (%s).\n", previousResult.ResultType.String(), reflect.String.String())
			}
		}
	}

	if command == "" || len(commandArgs) == 0 {
		return false, &actions.ChainedResult{}, errors.New("Error: command or commandArgs empty")
	}

	cmd := exec.Command(command, commandArgs...)
	output, err := cmd.Output()
	if err != nil {
		return false, &actions.ChainedResult{}, err
	}

	now := time.Now().String()
	now = strings.ReplaceAll(now, " ", "_")

	_, err = files.WriteFile(".", "cmd_" + now + ".txt", output)
	if err != nil {
		return false, &actions.ChainedResult{}, err
	}

	return true, &actions.ChainedResult{Result: string(output), ResultType: reflect.String}, nil
}
