package configs

import (
	"encoding/json"
	"github.com/rs/zerolog/log"
	"io/ioutil"
	"os"
	"path/filepath"
)

// ReadFromFile is a method used to read the configs file and parse the content into
// the `Configs` struct.
func ReadFromFile() error {
	f := filepath.Join(Path, Filename)

	mutex.Lock()
	defer mutex.Unlock()

	jsonData, err := os.Open(f)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoConfigFileDetected
		}
		return err
	}
	defer func() {
		err := jsonData.Close()
		if err != nil {
			log.Error().Err(err).Msg("Error when trying to close the file of configs after reading it")
		}
	}()

	byteContent, err := ioutil.ReadAll(jsonData)
	if err != nil {
		return err
	}

	var cfg Configs
	err = json.Unmarshal(byteContent, &cfg)
	if err != nil {
		return ErrConfigFileCorrupted
	}

	// Update configs variable
	CurrentConfigs = &cfg

	return nil
}
