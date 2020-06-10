package configs

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
)

// ReadFromFile is a method used to read the configs file and parse the content into
// the `Configs` struct.
func ReadFromFile() error {
	fullpath := filepath.Join(Path, Filename)

	mutex.Lock()
	defer mutex.Unlock()

	jsonData, err := os.Open(fullpath)
	if err != nil {
		return err
	}
	defer jsonData.Close()

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
