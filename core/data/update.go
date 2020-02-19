package data

import (
	"encoding/json"

	"github.com/Pegasus8/piworker/utilities/files"
	"github.com/rs/zerolog/log"
)

// UpdateTask is a function used to update an existing task from the JSON data file.
func UpdateTask(ID string, updatedTask *UserTask) error {
	log.Info().Str("taskID", ID).Msg("Updating task...")

	data, err := ReadData()
	if err != nil {
		return err
	}

	_, index, err := data.GetTaskByID(ID)
	if err != nil {
		return err
	}

	// Set the same ID
	updatedTask.TaskInfo.ID = data.Tasks[index].TaskInfo.ID

	log.Printf("Task with ID '%s' found, updating data...\n", ID)
	data.Tasks[index] = *updatedTask

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	// Re-write data into file
	_, err = files.WriteFile(DataPath, Filename, byteData)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", ID).Msg("Task updated successfully")
	return nil
}

// UpdateTaskName is a function used to change the name of a task.
func UpdateTaskName(ID, oldName, newName string) error {
	log.Info().Str("taskID", ID).Msg("Updating task name...")

	data, err := ReadData()
	if err != nil {
		return err
	}

	_, index, err := data.GetTaskByID(ID)
	if err != nil {
		return err
	}

	data.Tasks[index].TaskInfo.Name = newName

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(DataPath, Filename, byteData)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", ID).Msg("Task name updated successfully")
	return nil
}

// UpdateTaskTrigger is a function used to change the trigger of a task.
func UpdateTaskTrigger(ID string, newTrigger *UserTrigger) error {
	log.Info().Str("taskID", ID).Msg("Updating task trigger...")

	data, err := ReadData()
	if err != nil {
		return err
	}

	_, index, err := data.GetTaskByID(ID)
	if err != nil {
		return err
	}

	data.Tasks[index].TaskInfo.Trigger = *newTrigger

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(DataPath, Filename, byteData)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", ID).Msg("Task trigger updated successfully")
	return nil
}

// UpdateTaskActions is a function used to change the actions of a task.
func UpdateTaskActions(ID string, newActions *[]UserAction) error {
	log.Info().Str("taskID", ID).Msg("Updating task actions...")

	data, err := ReadData()
	if err != nil {
		return err
	}

	_, index, err := data.GetTaskByID(ID)
	if err != nil {
		return err
	}

	data.Tasks[index].TaskInfo.Actions = *newActions

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(DataPath, Filename, byteData)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", ID).Msg("Task actions updated successfully")
	return nil
}

// UpdateTaskState is a function used to change the state of a task.
func UpdateTaskState(ID string, newState TaskState) error {
	log.Info().Str("taskID", ID).Msg("Updating task state...")

	data, err := ReadData()
	if err != nil {
		return err
	}

	_, index, err := data.GetTaskByID(ID)
	if err != nil {
		return err
	}

	data.Tasks[index].TaskInfo.State = newState

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(DataPath, Filename, byteData)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", ID).Msg("Task state updated successfully")
	return nil
}
