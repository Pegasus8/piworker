package configs

import (
	"encoding/json"

	"github.com/Pegasus8/piworker/utilities/files"
)

// WriteToFile is a method used to write the configs into the configs file,
// overwritting the previous content if exists. Use carefully.
func WriteToFile() error {
	mutex.Lock()
	defer mutex.Unlock()

	CurrentConfigs.RLock()
	byteData, err := json.MarshalIndent(CurrentConfigs, "", "   ")
	CurrentConfigs.RUnlock()
	if err != nil {
		return err
	}

	_, err = files.WriteFile(ConfigsPath, Filename, byteData)
	if err != nil {
		return err
	}

	return nil
}
