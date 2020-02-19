package models

import (
	"compress/gzip"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/rs/zerolog/log"
)

// ID's
const (
	// Action
	compressFilesOfDirID = "A2"

	// Args
	directoryCompressFilesOfDirArgID = "A2-1"
	savetoCompressFilesOfDirArgID    = "A2-2"
)

// CompressFilesOfDir - Action
var CompressFilesOfDir = actions.Action{
	ID:   compressFilesOfDirID,
	Name: "Compress Files of a Directory",
	Description: "Compress the files of a directory in gzip format.\nNote: it won't " +
		"compress subdirectories, just files.",
	Run: compressFilesOfDir,
	Args: []actions.Arg{
		actions.Arg{
			ID:   directoryCompressFilesOfDirArgID,
			Name: "Directory Target",
			Description: "The directory where the files to compress are located." +
				" Example: '/home/pegasus8/Images/'",
			// Content: "",
			ContentType: types.Path,
		},
		actions.Arg{
			ID:   savetoCompressFilesOfDirArgID,
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

func compressFilesOfDir(previousResult *actions.ChainedResult, parentAction *data.UserAction, parentTaskID string) (result bool, chainedResult *actions.ChainedResult, err error) {
	var args *[]data.UserArg

	// Directory of files
	var targetDir string
	// Output dir
	var outputDir string

	args = &parentAction.Args

	for _, arg := range *args {
		switch arg.ID {
		case directoryCompressFilesOfDirArgID:
			targetDir = filepath.Clean(arg.Content)
		case savetoCompressFilesOfDirArgID:
			outputDir = filepath.Clean(arg.Content)

		default:
			return false, &actions.ChainedResult{}, ErrUnrecognizedArgID
		}
	}

	if targetDir == "" || outputDir == "" {
		return false, &actions.ChainedResult{}, errors.New("Error: targetDir or outputDir empty")
	}

	log.Info().Str("taskID", parentTaskID).Msgf("Creating the directory '%s' if it doesn't exists...", outputDir)
	err = os.MkdirAll(outputDir, 0700)
	if err != nil {
		return false, &actions.ChainedResult{}, nil
	}

	log.Info().Str("taskID", parentTaskID).Msgf("Getting the files of the directory '%s'", targetDir)
	files, err := ioutil.ReadDir(targetDir)
	if err != nil {
		return false, &actions.ChainedResult{}, err
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
			return false, &actions.ChainedResult{}, err
		}
		defer openedFile.Close()

		content, err := ioutil.ReadAll(openedFile)
		if err != nil {
			return false, &actions.ChainedResult{}, err
		}

		newFilename := file.Name() + ".gz"
		newPath := filepath.Join(outputDir, newFilename)

		outputFile, err := os.Create(newPath)
		if err != nil {
			return false, &actions.ChainedResult{}, err
		}

		gzipWriter := gzip.NewWriter(outputFile)
		defer gzipWriter.Close()

		_, err = gzipWriter.Write(content)
		if err != nil {
			return false, &actions.ChainedResult{}, err
		}

		log.Info().Str("taskID", parentTaskID).Msgf("'%s' compressed by the action CompressFilesOfDir", newFilename)

	}

	log.Info().Str("taskID", parentTaskID).Msgf("Files compression finished into directory '%s'", outputDir)

	return true, &actions.ChainedResult{Result: outputDir, ResultType: types.Path}, nil
}
