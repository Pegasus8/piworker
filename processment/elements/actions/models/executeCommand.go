package models

import (
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/actions"
	"github.com/Pegasus8/piworker/processment/types"
	"github.com/Pegasus8/piworker/utilities/files"
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
			ContentType: types.Text,
		},
		actions.Arg{
			ID:   argumentsExecuteCommandID,
			Name: "Arguments",
			Description: "The arguments of the command provided, separated" +
				" by a comma.",
			// Content:     "",
			ContentType: types.Text,
		},
	},
	ReturnedChainResultDescription: "The command to execute.",
	ReturnedChainResultType:        types.Text,
}

func executeCommand(previousResult *actions.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *actions.ChainedResult, err error) {
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

	_, err = files.WriteFile(".", "cmd_"+now+".txt", output)
	if err != nil {
		return false, &actions.ChainedResult{}, err
	}

	return true, &actions.ChainedResult{Result: string(output), ResultType: types.Text}, nil
}
