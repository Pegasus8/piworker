package models

import (
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/actions"
	"github.com/Pegasus8/piworker/processment/types"
)

// ID's
const (
	// Action
	writeTextFileID = "A1"

	// Args
	contentWriteTextFileArgID  = "A1-1"
	filenameWriteTextFileArgID = "A1-2"
	modeWriteTextFileArgID     = "A1-3"
	pathWriteTextFileArgID     = "A1-4"
)

// WriteTextFile - Action
var WriteTextFile = actions.Action{
	ID:          writeTextFileID,
	Name:        "Write a Text File",
	Description: "",
	Run:         writeTextFileAction,
	Args: []actions.Arg{
		actions.Arg{
			ID:          contentWriteTextFileArgID,
			Name:        "Content",
			Description: "Content to write into the text file.",
			// Content:     "",
			ContentType: "text",
		},
		actions.Arg{
			ID:          filenameWriteTextFileArgID,
			Name:        "File Name",
			Description: "Name of the file that will be written, without the extension.",
			// Content:     "",
			ContentType: "text",
		},
		actions.Arg{
			ID:   modeWriteTextFileArgID,
			Name: "Writing Mode",
			Description: "Mode used to write the file. Can be: 'a' = append and 'w' = write" +
				", where the main difference is that append mode just add content if the file " +
				"already exists and the write mode overwrite the file if already exists." +
				"\nNote: just write the letter, not the quotation marks.",
			// Content:     "",
			ContentType: "text",
		},
		actions.Arg{
			ID:          pathWriteTextFileArgID,
			Name:        "Path",
			Description: "Path where the file will be saved. Example: /home/pegasus8/Desktop/",
			// Content:     "",
			ContentType: "text",
		},
	},
	ReturnedChainResultDescription: "The path where will be writed the file.",
	ReturnedChainResultType:        types.Path,
}

func writeTextFileAction(previousResult *actions.ChainedResult, parentAction *data.UserAction, parentTaskName string) (result bool, chainedResult *actions.ChainedResult, err error) {
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
		case contentWriteTextFileArgID:
			content = arg.Content
		case filenameWriteTextFileArgID:
			filename = arg.Content + ".txt"
		case modeWriteTextFileArgID:
			{
				switch arg.Content {
				case "a":
					writingMode = arg.Content
				case "w":
					writingMode = arg.Content
				default:
					return false, &actions.ChainedResult{}, ErrUnrecognizedWritingMode
				}
			}
		case pathWriteTextFileArgID:
			path = filepath.Clean(arg.Content)
		default:
			{
				log.Println("[%s] Unrecongnized argument with the ID '%s' on the "+
					"action WriteTextFile\n", parentTaskName, arg.ID)
				return false, &actions.ChainedResult{}, ErrUnrecognizedArgID
			}
		}

	}

	if parentAction.Chained {
		if previousResult.Result == "" {
			log.Println(ErrEmptyChainedResult.Error())
		} else {
			if previousResult.ResultType == types.Path {
				// Overwrite path
				path = previousResult.Result
			} else {
				log.Printf("[%s] Type of previous ChainedResult ('%s') differs with the required type ('%s').\n", parentTaskName, previousResult.ResultType, types.Path)
			}
		}
	}

	if path == "" || filename == "" || writingMode == "" {
		return false, &actions.ChainedResult{}, errors.New("Error: path, filename or writingMode empty")
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
		return false, &actions.ChainedResult{}, err
	}
	defer file.Close()

	bytesWritten, err := file.WriteString(content)
	if err != nil {
		return false, &actions.ChainedResult{}, err
	}

	log.Printf("[%s] File written by the action WriteTextFile. Bytes written: %d\n", parentTaskName, bytesWritten)

	return true, &actions.ChainedResult{Result: fullpath, ResultType: types.Path}, nil
}
