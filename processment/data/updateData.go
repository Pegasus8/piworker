package data

import (
	"encoding/json"
	"log"

	"github.com/Pegasus8/piworker/utilities/files"
)

// UpdateData is a function used to update an existing task from the JSON data file
func UpdateData(filename string, taskName string, 
	updatedTask *UserTask) (bool, error) {
	
	data, err := ReadData(filename)
	if err != nil {
		return false, err
	}

	_, index, err := data.GetTask(taskName)
	if err != nil {
		return false, err
	}

	log.Printf("Task with name '%s' finded, updating data...\n", taskName)
	data.Tasks[index].Task = *updatedTask

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return false, err
	}

	// Re-write data into file
	_, err = files.WriteFile(".", filename, byteData)
	if err != nil {
		return false, err
	}

	log.Println("Task updated successfully")
	return true, nil
}