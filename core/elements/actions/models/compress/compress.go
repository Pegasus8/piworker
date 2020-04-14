package compress

import (
	"compress/gzip"
	"errors"
	"io/ioutil"
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
	actionID = "A2"

	// Args
	arg1ID = actionID + "-1"
	arg2ID    = actionID + "-2"
)

// CompressFilesOfDir - Action
var CompressFilesOfDir = shared.Action{
	ID:   actionID,
	Name: "Compress Files of a Directory",
	Description: "Compress the files of a directory in gzip format.\nNote: it won't " +
		"compress subdirectories, just files.",
	Run: compressFilesOfDir,
	Args: []shared.Arg{
		shared.Arg{
			ID:   arg1ID,
			Name: "Directory Target",
			Description: "The directory where the files to compress are located." +
				" Example: '/home/pegasus8/Images/'",
			// Content: "",
			ContentType: types.Path,
		},
		shared.Arg{
			ID:   arg2ID,
			Name: "Directory Where Save",
			Description: "Directory where save the compressed file, if not exists " +
				"it will be created. Example: '/home/'",
			// Content: "",
			ContentType: types.Path,
		},
	},
	ReturnedChainResultDescription: "The path of the compressed file.",
	ReturnedChainResultType:        types.Path,
}

func compressFilesOfDir(previousResult *shared.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *shared.ChainedResult, err error) {
	var args *[]data.UserArg

	// Directory of files
	var targetDir string
	// Output dir
	var outputDir string

	args = &parentAction.Args

	for _, arg := range *args {
		switch arg.ID {
		case arg1ID:
			targetDir = filepath.Clean(arg.Content)
		case arg2ID:
			outputDir = filepath.Clean(arg.Content)

		default:
			return false, &shared.ChainedResult{}, shared.ErrUnrecognizedArgID
		}
	}

	if targetDir == "" || outputDir == "" {
		return false, &shared.ChainedResult{}, errors.New("Error: targetDir or outputDir empty")
	}

	log.Info().Str("taskID", parentTaskID).Msgf("Creating the directory '%s' if it doesn't exists...", outputDir)
	err = os.MkdirAll(outputDir, 0700)
	if err != nil {
		return false, &shared.ChainedResult{}, nil
	}

	log.Info().Str("taskID", parentTaskID).Msgf("Getting the files of the directory '%s'", targetDir)
	files, err := ioutil.ReadDir(targetDir)
	if err != nil {
		return false, &shared.ChainedResult{}, err
	}
	log.Info().Str("taskID", parentTaskID).Msg("Files obtained")

	for _, file := range files {
		if file.IsDir() {
			log.Warn().Str("taskID", parentTaskID).Msgf("Skipping '%s' because it isn't a file", file.Name())
			continue
		}
		log.Info().Str("taskID", parentTaskID).Msgf("Starting the compression of the file '%s'...", file.Name())

		openedFile, err := os.Open(
			filepath.Join(targetDir, file.Name()),
		)
		if err != nil {
			return false, &shared.ChainedResult{}, err
		}
		defer openedFile.Close()

		content, err := ioutil.ReadAll(openedFile)
		if err != nil {
			return false, &shared.ChainedResult{}, err
		}

		newFilename := file.Name() + ".gz"
		newPath := filepath.Join(outputDir, newFilename)

		outputFile, err := os.Create(newPath)
		if err != nil {
			return false, &shared.ChainedResult{}, err
		}

		gzipWriter := gzip.NewWriter(outputFile)
		defer gzipWriter.Close()

		_, err = gzipWriter.Write(content)
		if err != nil {
			return false, &shared.ChainedResult{}, err
		}

		log.Info().Str("taskID", parentTaskID).Msgf("'%s' compressed by the action CompressFilesOfDir", newFilename)

	}

	log.Info().Str("taskID", parentTaskID).Msgf("Files compression finished into directory '%s'", outputDir)

	return true, &shared.ChainedResult{Result: outputDir, ResultType: types.Path}, nil
}
