package writetf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
)

const actionID = "A1"

var actionArgs = []shared.Arg{
	{
		ID:          actionID + "-1",
		Name:        "Content",
		Description: "Content to write into the file.",
		ContentType: types.Any,
	},
	{
		ID:          actionID + "-2",
		Name:        "File Name",
		Description: "Name of the file that will be written. Remember to add the extension of the file, for example: 'my_file.txt'.",
		ContentType: types.Text,
	},
	{
		ID:   actionID + "-3",
		Name: "Writing Mode",
		Description: "Mode used to write the file. Can be: 'a' = append and 'w' = write" +
			", where the main difference is that append mode just adds content if the file " +
			"already exists and the write mode overwrites the file if already exists." +
			"\nNote: just write the letter, not the quotation marks.",
		ContentType: types.Text,
	},
	{
		ID:          actionID + "-4",
		Name:        "Path",
		Description: "Path where the file will be saved. Example: /home/pegasus8/Desktop/",
		ContentType: types.Path,
	},
}

// WriteTextFile - Action
var WriteTextFile = shared.Action{
	ID:                             actionID,
	Name:                           "Write a Text File",
	Description:                    "",
	Run:                            action,
	Args:                           actionArgs,
	ReturnedChainResultDescription: "The path where the file will be written.",
	ReturnedChainResultType:        types.Path,
}

func action(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	if len(parentAction.Args) != len(actionArgs) {
		return false, &shared.ChainedResult{}, fmt.Errorf("%d arguments were expected and %d were obtained", len(actionArgs), len(parentAction.Args))
	}

	var args *[]data.UserArg

	// Content of the file
	var content string
	// File Name
	var filename string
	// Writing mode
	var writingMode string
	// Path
	var path string

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
			content = arg.Content
		case actionArgs[1].ID:
			filename = arg.Content
		case actionArgs[2].ID:
			{
				switch arg.Content {
				case "a":
					writingMode = arg.Content
				case "w":
					writingMode = arg.Content
				default:
					return false, &shared.ChainedResult{}, shared.ErrUnrecognizedWritingMode
				}
			}
		case actionArgs[3].ID:
			path = filepath.Clean(arg.Content)
		default:
			{
				return false, &shared.ChainedResult{}, shared.ErrUnrecognizedArgID
			}
		}
	}

	if path == "" || filename == "" || writingMode == "" {
		return false, &shared.ChainedResult{}, errors.New("Error: path, filename or writingMode empty")
	}

	fullpath := filepath.Join(path, filename)

	var flags int
	sharedFlags := os.O_WRONLY | os.O_CREATE
	if writingMode == "a" {
		flags = sharedFlags | os.O_APPEND
	} else {
		flags = sharedFlags | os.O_TRUNC
	}

	file, err := os.OpenFile(fullpath, flags, 0666)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}
	defer file.Close()

	if writingMode == "a" {
		content = content + "\n"
	}

	_, err = file.WriteString(content)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	return true, &shared.ChainedResult{Result: fullpath, ResultType: types.Path}, nil
}
