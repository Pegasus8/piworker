package configs


import (
	"encoding/json"
	"log"

	"github.com/Pegasus8/piworker/utilities/files"
	// "github.com/Pegasus8/piworker/utilities/log"
)

// WriteConfigs is a function used to write the configs into the configs file, 
// overwritting the previous content if exists. Use carefully.
func WriteConfigs(configs *Configs) error {
	mutex.Lock()
	defer mutex.Unlock()
	log.Println("Writing configs...")
	
	log.Println("Reading JSON data...")
	byteData, err := json.MarshalIndent(configs, "", "   ")
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