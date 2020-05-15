package cmdexec

import (
	"errors"
	"fmt"
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
	{
		ID:          actionID + "-1",
		Name:        "Command",
		Description: "The command to execute. For example: 'touch'.",
		ContentType: types.Text,
	},
	{
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
	Description:                    "Execute a command with the given arguments.",
	Run:                            action,
	Args:                           actionArgs,
	ReturnedChainResultDescription: "The command to execute.",
	ReturnedChainResultType:        types.Text,
}

var outputPath = "."

func action(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	if len(parentAction.Args) != len(actionArgs) {
		return false, &shared.ChainedResult{}, fmt.Errorf("%d arguments were expected and %d were obtained", len(actionArgs), len(parentAction.Args))
	}

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

	for i, arg := range *args {
		if arg.Content == "" {
			return false, &shared.ChainedResult{}, fmt.Errorf("argument %d (ID: %s) is empty", i, arg.ID)
		}

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

	now := time.Now().Format("2006-01-02_15:04")

	_, err = files.WriteFile(outputPath, "cmd_"+command+"_"+now+".txt", output)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	return true, &shared.ChainedResult{Result: string(output), ResultType: types.Text}, nil
}
