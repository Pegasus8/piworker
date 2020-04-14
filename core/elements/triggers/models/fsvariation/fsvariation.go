package fsvariation

import (
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers/shared"
	"github.com/Pegasus8/piworker/core/types"
)

// ID's
const (
	// Trigger
	triggerID = "T5"

	// Args
	arg1ID = triggerID + "-1"
)

// VariationOfFileSize - Trigger
var VariationOfFileSize = shared.Trigger{
	ID:          triggerID,
	Name:        "Variation of a File's Size",
	Description: "",
	Run:         variationOfFileSize,
	Args: []shared.Arg{
		shared.Arg{
			ID:          arg1ID,
			Name:        "Path of the Objective File",
			Description: "Must be on the format 'path/of/the/file.txt'.",
			// Content:     "",
			ContentType: types.Path,
		},
	},
}

var previousFileSize int64

func variationOfFileSize(args *[]data.UserArg, parentTaskID string) (result bool, err error) {

	// Filepath
	var filePath string

	for _, arg := range *args {
		switch arg.ID {
		case arg1ID:
			filePath = filepath.Clean(arg.Content)
		default:
			{
				return false, shared.ErrUnrecognizedArgID
			}
		}
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return false, err
	}

	// First execution
	if previousFileSize == 0 {
		previousFileSize = info.Size()
		return false, nil
	}

	if info.Size() == previousFileSize {
		return true, nil
	}

	return false, nil
}
