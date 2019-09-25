package configs

import (
	// "github.com/Pegasus8/piworker/utilities/log"
	"log"
)

// UpdateBehaviorConfigs is the function used to update the behavior configs in the
// configs file.
func UpdateBehaviorConfigs(behaviorCfg *Behavior) error {
	log.Println("Updating Behavior configs...")

	// Overwrite configs
	CurrentConfigs.Behavior = *behaviorCfg
	
	err := WriteConfigs(CurrentConfigs)
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

	CurrentConfigs.Security = *securityCfg

	err := WriteConfigs(CurrentConfigs)
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

	CurrentConfigs.APIConfigs = *apiCfg

	err := WriteConfigs(CurrentConfigs)
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

	CurrentConfigs.Updates = *updatesCfg

	err := WriteConfigs(CurrentConfigs)
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

	CurrentConfigs.WebUI = *webuiCfg

	err := WriteConfigs(CurrentConfigs)
	if err != nil {
		return err
	}
	
	log.Println("WebUI configs updated successfully")
	return nil
}