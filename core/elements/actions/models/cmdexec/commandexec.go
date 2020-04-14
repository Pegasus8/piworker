package cmdexec

import (
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/utilities/files"
)

// ID's
const (
	// Action
	actionID = "A3"

	// Args
	arg1ID   = actionID + "-1"
	arg2ID = actionID + "-2"
)

// ExecuteCommand - Action
var ExecuteCommand = shared.Action{
	ID:          actionID,
	Name:        "",
	Description: "",
	Run:         executeCommand,
	Args: []shared.Arg{
		shared.Arg{
			ID:          arg1ID,
			Name:        "Command",
			Description: "The command to execute.",
			// Content:     "",
			ContentType: types.Text,
		},
		shared.Arg{
			ID:   arg2ID,
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

func executeCommand(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	var args *[]data.UserArg

	// Command
	var command string
	// Command args
	var commandArgs []string

	args = &parentAction.Args

	for _, arg := range *args {
		switch arg.ID {
		case arg1ID:
			command = strings.TrimSpace(arg.Content)
		case arg2ID:
			commandArgs = strings.Split(arg.Content, ",")
		default:
			return false, &shared.ChainedResult{}, shared.ErrUnrecognizedArgID
		}
	}

	if command == "" || len(commandArgs) == 0 {
		return false, &shared.ChainedResult{}, errors.New("Error: command or commandArgs empty")
	}

	cmd := exec.Command(command, commandArgs...)
	output, err := cmd.Output()
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	now := time.Now().String()
	now = strings.ReplaceAll(now, " ", "_")

	_, err = files.WriteFile(".", "cmd_"+now+".txt", output)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	return true, &shared.ChainedResult{Result: string(output), ResultType: types.Text}, nil
}
