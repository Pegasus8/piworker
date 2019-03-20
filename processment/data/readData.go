package data

import (
	"io/ioutil"
	"os"
	"log"
	"encoding/json"
)

const (
	// FileName is the name of user data file
	FileName = "testing_data.json" //! Name just for testing. Remove.
)

// ReadData is a func that returns the user data into structs
func ReadData() (*UserData, error){
	jsonData, err := os.Open(FileName)
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