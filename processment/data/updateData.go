package data

import (
	"encoding/json"
	"log"

	"github.com/Pegasus8/piworker/utilities/files"
)

// UpdateTask is a function used to update an existing task from the JSON data file.
func UpdateTask(taskName string, updatedTask *UserTask) error {
	data, err := ReadData()
	if err != nil {
		return err
	}

	_, index, err := data.GetTaskByName(taskName)
	if err != nil {
		return err
	}

	log.Printf("Task with name '%s' finded, updating data...\n", taskName)
	data.Tasks[index].Task = *updatedTask

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	// Re-write data into file
	_, err = files.WriteFile(".", Filename, byteData)
	if err != nil {
		return err
	}

	log.Println("Task updated successfully")
	return nil
}

// UpdateTaskName is a function used to change the name of a task.
func UpdateTaskName(oldName, newName string) error {

	data, err := ReadData()
	if err != nil {
		return err
	}

	oldTask, index, err := data.GetTaskByName(oldName)
	if err != nil {
		return err
	}
	
	data.Tasks[index].Task = UserTask{
		Name: newName,
		State: oldTask.State,
		Trigger: oldTask.Trigger,
		Actions: oldTask.Actions,
	}

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(".", Filename, byteData)
	if err != nil {
		return err
	}

	return nil
}

// UpdateTaskTrigger is a function used to change the trigger of a task.
func UpdateTaskTrigger(name string, newTrigger *UserTrigger) error {

	data, err := ReadData()
	if err != nil {
		return err
	}

	oldTask, index, err := data.GetTaskByName(name)
	if err != nil {
		return err
	}
	
	data.Tasks[index].Task = UserTask{
		Name: oldTask.Name,
		State: oldTask.State,
		Trigger: *newTrigger,
		Actions: oldTask.Actions,
	}

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(".", Filename, byteData)
	if err != nil {
		return err
	}

	return nil
}

// UpdateTaskActions is a function used to change the actions of a task.
func UpdateTaskActions(name string, newActions *[]UserAction) error {

	data, err := ReadData()
	if err != nil {
		return err
	}

	oldTask, index, err := data.GetTaskByName(name)
	if err != nil {
		return err
	}
	
	data.Tasks[index].Task = UserTask{
		Name: oldTask.Name,
		State: oldTask.State,
		Trigger: oldTask.Trigger,
		Actions: *newActions,
	}

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(".", Filename, byteData)
	if err != nil {
		return err
	}
	
	return nil
}

// UpdateTaskState is a function used to change the state of a task.
func UpdateTaskState(name string, newState string) error {

	data, err := ReadData()
	if err != nil {
		return err
	}

	oldTask, index, err := data.GetTaskByName(name)
	if err != nil {
		return err
	}
	
	data.Tasks[index].Task = UserTask{
		Name: oldTask.Name,
		State: newState,
		Trigger: oldTask.Trigger,
		Actions: oldTask.Actions,
	}

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(".", Filename, byteData)
	if err != nil {
		return err
	}

	return nil
}