package data

import (
	"encoding/json"
	"log"

	"github.com/Pegasus8/piworker/utilities/files"
)

// UpdateTask is a function used to update an existing task from the JSON data file.
func UpdateTask(ID string, updatedTask *UserTask) error {
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

	log.Println("Task updated successfully")
	return nil
}

// UpdateTaskName is a function used to change the name of a task.
func UpdateTaskName(ID, oldName, newName string) error {

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

	return nil
}

// UpdateTaskTrigger is a function used to change the trigger of a task.
func UpdateTaskTrigger(ID string, newTrigger *UserTrigger) error {

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

	return nil
}

// UpdateTaskActions is a function used to change the actions of a task.
func UpdateTaskActions(ID string, newActions *[]UserAction) error {

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

	return nil
}

// UpdateTaskState is a function used to change the state of a task.
func UpdateTaskState(ID string, newState TaskState) error {

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

	return nil
}
