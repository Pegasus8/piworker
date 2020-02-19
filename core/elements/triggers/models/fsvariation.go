package models

import (
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers"
	"github.com/Pegasus8/piworker/core/types"
)

// ID's
const (
	// Trigger
	variationOfFileSizeID = "T5"

	// Args
	filepathVariationOfFileSizeID = "T5-1"
)

// VariationOfFileSize - Trigger
var VariationOfFileSize = triggers.Trigger{
	ID:          variationOfFileSizeID,
	Name:        "Variation of a File's Size",
	Description: "",
	Run:         variationOfFileSize,
	Args: []triggers.Arg{
		triggers.Arg{
			ID:          filepathVariationOfFileSizeID,
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
		case filepathVariationOfFileSizeID:
			filePath = filepath.Clean(arg.Content)
		default:
			{
				return false, ErrUnrecognizedArgID
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
