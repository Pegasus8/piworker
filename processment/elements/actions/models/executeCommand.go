package models

import (
	"os/exec"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/actions"
	// "github.com/Pegasus8/piworker/utilities/log"
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
			ContentType: "string",
		},
		actions.Arg{
			ID:   argumentsExecuteCommandID,
			Name: "Arguments",
			Description: "The arguments of the command provided, separated" +
				" by a comma.",
			// Content:     "",
			ContentType: "string",
		},
	},
}

func executeCommand(args *[]data.UserArg) (result bool, err error) {

	// Command
	var command string
	// Command args
	var commandArgs []string

	for _, arg := range *args {
		switch arg.ID {
		case commandExecuteCommandID:
				command = strings.TrimSpace(arg.Content)
		case argumentsExecuteCommandID:
				commandArgs = strings.Split(arg.Content, ",")
		default:
			return false, ErrUnrecognizedArgID
		}
	}

	cmd := exec.Command(command, commandArgs...)
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	now := time.Now().String()
	now = strings.ReplaceAll(now, " ", "_")

	_, err = files.WriteFile(".", "cmd_" + now + ".txt", output)
	if err != nil {
		return false, err
	}

	return true, nil
}
