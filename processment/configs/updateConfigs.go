package configs

import (
	// "github.com/Pegasus8/piworker/utilities/log"
	"log"
)

// UpdateBehaviorConfigs is the function used to update the behavior configs in the
// configs file.
func UpdateBehaviorConfigs(behaviorCfg *Behavior) error {
	log.Println("Updating Behavior configs...")

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	// Overwrite configs
	data.Behavior = *behaviorCfg
	
	err = WriteConfigs(data)
	if err != nil {
		return err
	}

	log.Println("Behavior configs updated successfully")
	return nil
}

// UpdateSecurityConfigs is the function used to update the security configs in the
// configs file.
func UpdateSecurityConfigs(securityCfg *Security) error {
	log.Println("Updating Security configs...")

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.Security = *securityCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	log.Println("Security configs updated successfully")
	return nil
}

// UpdateAPIConfigs is the function used to update the API configs in the
// configs file.
func UpdateAPIConfigs(apiCfg *APIConfigs) error {
	log.Println("Updating API configs...")

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.APIConfigs = *apiCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	log.Println("API configs updated successfully")
	return nil
}

// UpdateUpdatesConfigs is the function used to update the PiWorker updates configs in the
// configs file.
func UpdateUpdatesConfigs(updatesCfg *Updates) error {
	log.Println("Updating Updates configs...")

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.Updates = *updatesCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	log.Println("Updates configs updated successfully")
	return nil
}

// UpdateWebUIConfigs is the function used to update the WebUI configs in the
// configs file.
func UpdateWebUIConfigs(webuiCfg *WebUI) error {
	log.Println("Updating WebUI configs...")
	
	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.WebUI = *webuiCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	log.Println("WebUI configs updated successfully")
	return nil
}