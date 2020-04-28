package compress

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
)

const actionID = "A2"

var actionArgs = []shared.Arg{
	{
		ID:   actionID + "-1",
		Name: "Directory/file target",
		Description: "The directory where the files to compress are located." +
			" Example: '/home/pegasus8/Images/'",
		ContentType: types.Path,
	},
	{
		ID:   actionID + "-2",
		Name: "Directory where to store the compressed file",
		Description: "Directory where save the compressed file, if not exists " +
			"it will be created. Example: '/home/'",
		ContentType: types.Path,
	},
	{
		ID:          actionID + "-3",
		Name:        "Name of the output file",
		Description: "The name of the zip file (without the extension). For example: 'my_files'",
		ContentType: types.Text,
	},
}

// CompressFilesOfDir - Action
var CompressFilesOfDir = shared.Action{
	ID:                             actionID,
	Name:                           "Compress Files of a Directory",
	Description:                    "",
	Run:                            action,
	Args:                           actionArgs,
	ReturnedChainResultDescription: "The path to the compressed file.",
	ReturnedChainResultType:        types.Path,
}

func action(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	var args *[]data.UserArg

	var targetDir string
	var outputDir string
	var outputFilename string

	args = &parentAction.Args

	err = shared.HandleCR(parentAction, actionArgs, previousResult)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	for _, arg := range *args {
		switch arg.ID {
		case actionArgs[0].ID:
			targetDir = filepath.Clean(arg.Content)
		case actionArgs[1].ID:
			outputDir = filepath.Clean(arg.Content)
		case actionArgs[2].ID:
			outputFilename = strings.ReplaceAll(strings.ReplaceAll(arg.Content, " ", "_"), "/", "-")
		default:
			return false, &shared.ChainedResult{}, shared.ErrUnrecognizedArgID
		}
	}

	if targetDir == "" || outputDir == "" || outputFilename == "" {
		return false, &shared.ChainedResult{}, errors.New("Error: targetDir, outputDir or outputFilename empty")
	}

	outputDir = filepath.Join(outputDir, outputFilename+".zip")

	err = zipWriter(targetDir, outputDir)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}

	return true, &shared.ChainedResult{Result: outputDir, ResultType: types.Path}, nil
}
