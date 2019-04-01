package configs

import (
	"encoding/json"
	"path/filepath"
	"io/ioutil"
	"os"
	
	"github.com/Pegasus8/piworker/utilities/log"
)

// ReadConfigs is a function used to read the configs file and parse the content into
// the `Configs` struct.
func ReadConfigs() (configs *Configs, err error) {
	fullpath := filepath.Join(ConfigsPath, Filename)
	
	mutex.Lock()
	defer mutex.Unlock()

	log.Infoln("Reading config file...")
	jsonData, err := os.Open(fullpath)
	if err != nil {
		return nil, err
	}
	defer jsonData.Close()
	log.Infoln("Configs loaded")

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