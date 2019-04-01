package data

import (
	"encoding/json"

	"github.com/Pegasus8/piworker/utilities/files"
	"github.com/Pegasus8/piworker/utilities/log"
)

// DeleteTask is a function used to delete a specific task from the JSON user database.
func DeleteTask(name string) error {
	mutex.Lock()
	defer mutex.Unlock()
	log.Infoln("Deleting a task...")

	data, err := ReadData()
	if err != nil {
		return err
	}

	_, index, err := data.GetTaskByName(name)
	if err != nil {
		return err
	}
	log.Infof("Task with the name '%s' finded, deleting...\n", name)

	if len(data.Tasks) == 1 {
		data.Tasks = []UserTask{}
	} else {
		data.Tasks = append(data.Tasks[:index], data.Tasks[index+1:]...)
	}

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	// Re-write data into file
	_, err = files.WriteFile(DataPath, Filename, byteData)
	if err != nil {
		return err
	}
	log.Infoln("Task deleted successfully")

	return nil
}