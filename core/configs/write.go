package configs

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
)

// WriteToFile is a method used to write the configs into the configs file,
// overwriting the previous content if exists. Use carefully.
func WriteToFile() error {
	mutex.Lock()
	defer mutex.Unlock()

	CurrentConfigs.RLock()
	byteData, err := json.MarshalIndent(CurrentConfigs, "", "   ")
	CurrentConfigs.RUnlock()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(Path, Filename), byteData, 0644)
	if err != nil {
		return err
	}

	return nil
}
