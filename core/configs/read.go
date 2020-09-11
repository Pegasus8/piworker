package configs

import (
	"encoding/json"
	"github.com/rs/zerolog/log"
	"io/ioutil"
	"os"
)

// readFromFile is a function that reads the file that stores the configs and returns it content parsed in the struct
// `Configs`.
func readFromFile(path string) (*Configs, error) {
	jsonData, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoConfigFileDetected
		}

		return nil, err
	}

	defer func() {
		err := jsonData.Close()
		if err != nil {
			log.Error().Err(err).Msg("Error when trying to close the file of configs after reading it")
		}
	}()

	byteContent, err := ioutil.ReadAll(jsonData)
	if err != nil {
		return nil, err
	}

	var cfg Configs
	err = json.Unmarshal(byteContent, &cfg)
	if err != nil {
		return &cfg, ErrConfigFileCorrupted
	}

	return &cfg, nil
}
