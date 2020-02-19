package data

import (
	"encoding/json"

	"github.com/Pegasus8/piworker/utilities/files"
	"github.com/rs/zerolog/log"
)

// DeleteTask is a function used to delete a specific task from the JSON user database.
func DeleteTask(ID string) error {
	log.Info().Str("taskID", ID).Msg("Deleting task...")

	data, err := ReadData()
	if err != nil {
		return err
	}

	_, index, err := data.GetTaskByID(ID)
	if err != nil {
		return err
	}
	log.Info().Str("taskID", ID).Msg("Task found, deleting...")

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

	log.Info().Str("taskID", ID).Msg("Task deleted successfully")
	return nil
}
