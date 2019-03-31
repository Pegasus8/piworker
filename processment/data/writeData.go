package data

import (
	"os"
	"log"
	"path/filepath"
	"encoding/json"

	"github.com/Pegasus8/piworker/utilities/files"
)

func init() {
	// Create data path if not exists
	err := os.MkdirAll(DataPath, os.ModePerm)
	if err != nil {
		log.Fatalln(err)
	}
}

// NewTask is a function used to add a new task to the JSON data file.
func NewTask(task *UserTask) error {
	log.Println("Adding a new task into JSON user data...")
	fullpath := filepath.Join(DataPath, Filename)
	if err := checkFile(fullpath); err != nil {
		return err
	}

	mutex.Lock()
	defer mutex.Unlock()

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
	log.Printf("Successfully added a new task with the name '%s' into " + 
		"JSON user data\n", task.TaskInfo.Name)

	// If the backup loop is not on, then start it
	if BackupLoopState != true {
		StartBackupLoop()
	} 

	return nil
}

func checkFile(filepath string) error {
	if Filename == "" {
		log.Fatalln(ErrNoFilenameAsigned)
	}
	
	_, err := os.Stat(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create the file
			log.Printf("Data file with name '%s' not exists, creating it...\n", Filename)
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
	log.Printf("New JSON data file with name '%s' initialized successfully\n", Filename)
	
	return nil
}	