package models

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"io/ioutil"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/actions"
	"github.com/Pegasus8/piworker/utilities/log"
)

// ID's
const (
	// Action
	compressFilesOfDirID = "A2"

	// Args
	directoryCompressFilesOfDirArgID = "A2-1"
	savetoCompressFilesOfDirArgID = "A2-2"
)

// CompressFilesOfDir - Action
var CompressFilesOfDir = actions.Action {
	ID: compressFilesOfDirID,
	Name: "Compress Files of a Directory",
	Description: "Compress the files of a directory in gzip format.\nNote: it won't " +
		"compress subdirectories, just files.",
	Run: compressFilesOfDir,
	Args: []actions.Arg{
		actions.Arg {
			ID: directoryCompressFilesOfDirArgID,
			Name: "Directory Target",
			Description: "The directory where the files to compress are located." + 
				" Example: '/home/pegasus8/Images/'",
			Content: "",
			ContentType: "string",
		},
		actions.Arg {
			ID: savetoCompressFilesOfDirArgID,
			Name: "Directory Where Save",
			Description: "Directory where save the compressed file, if not exists " + 
				"it will be created. Example: '/home/'",
			Content: "",
			ContentType: "string", 
		},
	},
}

func compressFilesOfDir(args *[]data.UserArg) (result bool, err error) {

	// Directory of files
	var targetDir string
	// Output dir
	var outputDir string

	for _, arg := range *args {
		switch arg.ID {
		case directoryCompressFilesOfDirArgID:
			targetDir = filepath.Clean(arg.Content)
		case savetoCompressFilesOfDirArgID: 
			outputDir = filepath.Clean(arg.Content)

		default:
			return false, ErrUnrecognizedArgID
		}
	}

	log.Infof("Creating the directory '%s' if it doesn't exist...\n", outputDir)
	err = os.MkdirAll(outputDir, 0700)
	if err != nil {
		return false, nil
	}
	
	log.Infof("Getting the files of the directory '%s'\n", targetDir)
	files, err := ioutil.ReadDir(targetDir)
	if err != nil {
		return false, err
	}
	log.Infoln("Files obtained")

	for _, file := range files {
		if file.IsDir() {
			log.Infof("Skipping '%s' because it isn't a file\n", file.Name())
			continue
		}
		log.Infof("Starting the compression of the file '%s'...\n", file.Name())
		
		openedFile, err := os.Open(
			filepath.Join(targetDir, file.Name()),
		)
		if err != nil {
			return false, err
		}
		defer openedFile.Close()

		content, err := ioutil.ReadAll(openedFile)
		if err != nil {
			return false, err
		}

		newFilename := file.Name() + ".gz"
		newPath := filepath.Join(outputDir, newFilename)

		outputFile, err := os.Create(newPath)
		if err != nil {
			return false, err
		}

		gzipWriter := gzip.NewWriter(outputFile)
		defer gzipWriter.Close()

		_, err = gzipWriter.Write(content)
		if err != nil {
			return false, err
		}

		log.Infof("'%s' compressed by the action CompressFilesOfDir\n", newFilename)

	}

	log.Infof("Files compression finished into directory '%s'\n", outputDir)

	return true, nil
}