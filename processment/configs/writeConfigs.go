package configs


import (
	"encoding/json"
	"log"

	"github.com/Pegasus8/piworker/utilities/files"
	// "github.com/Pegasus8/piworker/utilities/log"
)

// WriteToFile is a method used to write the configs into the configs file, 
// overwritting the previous content if exists. Use carefully.
func WriteToFile() error {
	mutex.Lock()
	defer mutex.Unlock()
	log.Println("Writing configs...")
	
	log.Println("Reading JSON data...")
	CurrentConfigs.RLock()
	byteData, err := json.MarshalIndent(CurrentConfigs, "", "   ")
	CurrentConfigs.RUnlock()
	if err != nil {
		return err
	}
	log.Println("JSON data read correctly")

	log.Println("Writing configs...")
	_, err = files.WriteFile(ConfigsPath, Filename, byteData)
	if err != nil {
		return err
	}
	log.Println("Configs writed successfully")

	return nil
}