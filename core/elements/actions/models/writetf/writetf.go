package writetf

import (
	"errors"
	"os"
	"path/filepath"
	
	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/rs/zerolog/log"
)

// ID's
const (
	// Action
	actionID = "A1"

	// Args
	arg1ID  = actionID + "-1"
	arg2ID = actionID + "-2"
	arg3ID     = actionID + "-3"
	arg4ID     = actionID + "-4"
)

// WriteTextFile - Action
var WriteTextFile = shared.Action{
	ID:          actionID,
	Name:        "Write a Text File",
	Description: "",
	Run:         writeTextFileAction,
	Args: []shared.Arg{
		shared.Arg{
			ID:          arg1ID,
			Name:        "Content",
			Description: "Content to write into the text file.",
			// Content:     "",
			ContentType: types.Any,
		},
		shared.Arg{
			ID:          arg2ID,
			Name:        "File Name",
			Description: "Name of the file that will be written, without the extension.",
			// Content:     "",
			ContentType: types.Text,
		},
		shared.Arg{
			ID:   arg3ID,
			Name: "Writing Mode",
			Description: "Mode used to write the file. Can be: 'a' = append and 'w' = write" +
				", where the main difference is that append mode just add content if the file " +
				"already exists and the write mode overwrite the file if already exists." +
				"\nNote: just write the letter, not the quotation marks.",
			// Content:     "",
			ContentType: types.Text,
		},
		shared.Arg{
			ID:          arg4ID,
			Name:        "Path",
			Description: "Path where the file will be saved. Example: /home/pegasus8/Desktop/",
			// Content:     "",
			ContentType: types.Path,
		},
	},
	ReturnedChainResultDescription: "The path where will be writed the file.",
	ReturnedChainResultType:        types.Path,
}

func writeTextFileAction(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
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

	for _, arg := range *args {

		switch arg.ID {
		case arg1ID:
			content = arg.Content
		case arg2ID:
			filename = arg.Content + ".txt"
		case arg3ID:
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
		case arg4ID:
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

	bytesWritten, err := file.WriteString(content)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	log.Info().Str("taskID", parentTaskID).Msgf("File written by the action WriteTextFile. Bytes written: %d", bytesWritten)

	return true, &shared.ChainedResult{Result: fullpath, ResultType: types.Path}, nil
}
