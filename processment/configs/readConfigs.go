package configs

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/utilities/files"
)

func init() {
	configsPath := filepath.Join(ConfigsPath, Filename)
	exists, err := files.Exists(configsPath)
	if err != nil {
		log.Fatal(err.Error())
	}
	if !exists {
		CurrentConfigs = &DefaultConfigs
		err = CurrentConfigs.WriteToFile()
		if err != nil {
			log.Fatal(err.Error())
		}
	} else {
		log.Println("Configs file found")
		err = CurrentConfigs.ReadFromFile()
		if err != nil {
			log.Fatal(err.Error())
		}
	}
}

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
