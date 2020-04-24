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

const actionID = "A3"

var actionArgs = []shared.Arg{
	shared.Arg{
		ID:          actionID + "-1",
		Name:        "Command",
		Description: "The command to execute. For example: 'touch'.",
		ContentType: types.Text,
	},
	shared.Arg{
		ID:   actionID + "-2",
		Name: "Arguments",
		Description: "The arguments of the command provided, separated" +
			" by a comma. For example (arg to command 'touch'): 'test.txt'.",
		ContentType: types.Text,
	},
}

// ExecuteCommand - Action
var ExecuteCommand = shared.Action{
	ID:                             actionID,
	Name:                           "Execute a command",
	Description:                    "",
	Run:                            action,
	Args:                           actionArgs,
	ReturnedChainResultDescription: "The command to execute.",
	ReturnedChainResultType:        types.Text,
}

func action(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	var args *[]data.UserArg

	// Command
	var command string
	// Command args
	var commandArgs []string

	args = &parentAction.Args

	err = shared.HandleCR(parentAction, actionArgs, previousResult)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	for _, arg := range *args {
		switch arg.ID {
		case actionArgs[0].ID:
			command = strings.TrimSpace(arg.Content)
		case actionArgs[1].ID:
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
