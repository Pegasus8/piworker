package data

import (
	"path"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/utilities/files"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

func init() {
	// Create data path if not exists
	err := os.MkdirAll(DataPath, os.ModePerm)
	if err != nil {
		log.Fatal().Err(err).Msg("Error when trying to create data directory")
	}
}

// NewTask is a function used to add a new task to the JSON data file.
func NewTask(task *UserTask) error {
	// Set the task ID
	task.TaskInfo.ID = uuid.New().String()

	log.Info().Str("taskID", task.TaskInfo.ID).Msg("Adding a new task into JSON user data...")

	fullpath := filepath.Join(DataPath, Filename)
	if err := checkFile(fullpath); err != nil {
		return err
	}

	data, err := ReadData()
	if err != nil {
		return err
	}

	// Add the task
	data.Tasks = append(data.Tasks, *task)

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(DataPath, Filename, byteData)
	if err != nil {
		return err
	}
	log.Info().Str("taskID", task.TaskInfo.ID).Msg("Task successfully added")

	if BackupLoopState != true {
		StartBackupLoop()
	}

	return nil
}

func checkFile(filepath string) error {
	if Filename == "" {
		log.Fatal().Err(ErrNoFilenameAssigned)
	}

	_, err := os.Stat(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create the file
			log.Info().Str("path", filepath).Msg("Data file not found, creating it...")
			if err = newJSONDataFile(); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func newJSONDataFile() error {
	log.Info().Str("path", path.Join(DataPath, Filename)).Msg("Initializing a new data file")
	mutex.Lock()
	defer mutex.Unlock()

	// Initialize a data file
	emptyDataStruct := UserData{[]UserTask{}}
	byteData, err := json.MarshalIndent(emptyDataStruct, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(DataPath, Filename, byteData)
	if err != nil {
		return err
	}
	
	log.Info().Msg("Data file initialized successfully")
	return nil
}
