package configs

import (
	"github.com/Pegasus8/piworker/utilities/log"
)

// UpdateBehaviorConfigs is the function used to update the behavior configs in the
// configs file.
func UpdateBehaviorConfigs(behaviorCfg *Behavior) error {
	mutex.Lock()
	defer mutex.Unlock()
	log.Infoln("Updating Behavior configs...")

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

	log.Infoln("Behavior configs updated successfully")
	return nil
}

// UpdateSecurityConfigs is the function used to update the security configs in the
// configs file.
func UpdateSecurityConfigs(securityCfg *Security) error {
	mutex.Lock()
	defer mutex.Unlock()
	log.Infoln("Updating Security configs...")

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.Security = *securityCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	log.Infoln("Security configs updated successfully")
	return nil
}

// UpdateAPIConfigs is the function used to update the API configs in the
// configs file.
func UpdateAPIConfigs(apiCfg *APIConfigs) error {
	mutex.Lock()
	defer mutex.Unlock()
	log.Infoln("Updating API configs...")

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.APIConfigs = *apiCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	log.Infoln("API configs updated successfully")
	return nil
}

// UpdateUpdatesConfigs is the function used to update the PiWorker updates configs in the
// configs file.
func UpdateUpdatesConfigs(updatesCfg *Updates) error {
	mutex.Lock()
	defer mutex.Unlock()
	log.Infoln("Updating Updates configs...")

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.Updates = *updatesCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	log.Infoln("Updates configs updated successfully")
	return nil
}

// UpdateWebUIConfigs is the function used to update the WebUI configs in the
// configs file.
func UpdateWebUIConfigs(webuiCfg *WebUI) error {
	mutex.Lock()
	defer mutex.Unlock()
	log.Infoln("Updating WebUI configs...")
	
	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.WebUI = *webuiCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	log.Infoln("WebUI configs updated successfully")
	return nil
}