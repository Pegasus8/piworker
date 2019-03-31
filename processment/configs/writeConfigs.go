package configs


import (
	"encoding/json"

	"github.com/Pegasus8/piworker/utilities/files"
)

// WriteConfigs is a function used to write the configs into the configs file, 
// overwritting the previous content if exists. Use carefully.
func WriteConfigs(configs *Configs) error {
	mutex.Lock()
	defer mutex.Unlock()
	
	byteData, err := json.MarshalIndent(configs, "", "   ")
	if err != nil {
		return err
	}

	_, err = files.WriteFile(ConfigsPath, Filename, byteData)
	if err != nil {
		return err
	}

	return nil
}