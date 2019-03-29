package data

import (
	"io/ioutil"
	"os"
	"log"
	"encoding/json"
	"path/filepath"
)

// ReadData is a func that returns the user data into structs
func ReadData() (*UserData, error){
	fullpath := filepath.Join(DataPath, Filename)

	jsonData, err := os.Open(fullpath)
	if err != nil {
		return nil, err
	}
	defer jsonData.Close()
	log.Println("Data user loaded")

	byteContent, err := ioutil.ReadAll(jsonData)
	if err != nil {
		return nil, err
	}

	var data UserData
	err = json.Unmarshal(byteContent, &data)
	if err != nil {
		return nil, err
	}

	return &data, nil
}

// GetTaskByName is a method of the UserData struct that returns a specific task, 
// searching it by it name.
func (data *UserData) GetTaskByName(name string) (findedTask *UserTask, indexPosition int, err error) {
	for index, task := range data.Tasks[:] {
		if task.TaskInfo.Name == name {
			return &data.Tasks[index], index, nil
		}
	}
	return nil, 0, ErrBadTaskName
}