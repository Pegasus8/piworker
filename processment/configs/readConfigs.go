package configs

import (
	"encoding/json"
	"path/filepath"
	"io/ioutil"
	"os"
	"log"
	
	"github.com/Pegasus8/piworker/utilities/files"
)

func init() {
	configsPath := filepath.Join(ConfigsPath, Filename)
	exists, err := files.Exists(configsPath)
	if err != nil {
		log.Fatal(err.Error())
	}
	if !exists {
		err = WriteConfigs(&DefaultConfigs)
		if err != nil {
			log.Fatal(err.Error())
		}
		// Don't need to read the file because we already know what's inside it.
		CurrentConfigs = &DefaultConfigs
	} else {
		log.Println("Configs file found")
		CurrentConfigs, err = ReadConfigs()
		if err != nil {
			log.Fatal(err.Error())
		}
	}
}

// ReadConfigs is a function used to read the configs file and parse the content into
// the `Configs` struct.
func ReadConfigs() (configs *Configs, err error) {
	fullpath := filepath.Join(ConfigsPath, Filename)
	
	mutex.Lock()
	defer mutex.Unlock()

	log.Println("Reading config file...")
	jsonData, err := os.Open(fullpath)
	if err != nil {
		return nil, err
	}
	defer jsonData.Close()
	log.Println("Configs loaded")

	byteContent, err := ioutil.ReadAll(jsonData)
	if err != nil {
		return nil, err
	}

	var cfg Configs
	err = json.Unmarshal(byteContent, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}