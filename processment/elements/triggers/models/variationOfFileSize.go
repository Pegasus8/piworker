package models

import (
	"os"
	"path/filepath"
	"log"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/triggers"
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
			ContentType: "text",
		},
	},
}

var previousFileSize int64

func variationOfFileSize(args *[]data.UserArg) (result bool, err error) {

	// Filepath
	var filePath string

	for _, arg := range *args {
		switch arg.ID {
		case filepathVariationOfFileSizeID:
			filePath = filepath.Clean(arg.Content)
		default:
			{
				log.Printf("Unrecognized argument with the ID '%s' on the "+
					"trigger VariationOfFileSize\n", arg.ID)
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
