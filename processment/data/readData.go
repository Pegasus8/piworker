package data

import (
	"io/ioutil"
	"os"
	"log"
	"encoding/json"
)

// ReadData is a func that returns the user data into structs
func ReadData(filename string) (*UserData, error){
	jsonData, err := os.Open(filename)
	if err != nil {
		log.Println("User data can't be read")
		log.Println(err)
	}
	log.Println("Data user loaded")
	defer jsonData.Close()

	byteContent, err := ioutil.ReadAll(jsonData)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	var data UserData

	json.Unmarshal(byteContent, &data)

	return &data, nil
}

// GetTask is a method of the UserData struct that returns a specific task, 
// searching it by it name.
func (data *UserData) GetTask(name string) (findedTask *UserTask, indexPosition int, err error) {
	for index, task := range data.Tasks[:] {
		if task.Task.Name == name {
			return &data.Tasks[index].Task, index, nil
		}
	}
	return nil, 0, ErrBadTaskName
}