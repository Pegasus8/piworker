package configs

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

// ReadFromFile is a method used to read the configs file and parse the content into
// the `Configs` struct.
func ReadFromFile() error {
	fullpath := filepath.Join(ConfigsPath, Filename)

	mutex.Lock()
	defer mutex.Unlock()

	log.Println("Reading config file...")
	jsonData, err := os.Open(fullpath)
	if err != nil {
		return err
	}
	defer jsonData.Close()
	log.Println("Configs loaded")

	byteContent, err := ioutil.ReadAll(jsonData)
	if err != nil {
		return err
	}

	var cfg Configs
	err = json.Unmarshal(byteContent, &cfg)
	if err != nil {
		return err
	}

	// Update configs variable
	CurrentConfigs = &cfg

	return nil
}
