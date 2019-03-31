package configs

import (

)

// UpdateBehaviorConfigs is the function used to update the behavior configs in the
// configs file.
func UpdateBehaviorConfigs(behaviorCfg *Behavior) error {
	mutex.Lock()
	defer mutex.Unlock()

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

	return nil
}

// UpdateSecurityConfigs is the function used to update the security configs in the
// configs file.
func UpdateSecurityConfigs(securityCfg *Security) error {
	mutex.Lock()
	defer mutex.Unlock()

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.Security = *securityCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	return nil
}

// UpdateAPIConfigs is the function used to update the API configs in the
// configs file.
func UpdateAPIConfigs(apiCfg *APIConfigs) error {
	mutex.Lock()
	defer mutex.Unlock()

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.APIConfigs = *apiCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	return nil
}

// UpdateUpdatesConfigs is the function used to update the PiWorker updates configs in the
// configs file.
func UpdateUpdatesConfigs(updatesCfg *Updates) error {
	mutex.Lock()
	defer mutex.Unlock()

	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.Updates = *updatesCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	return nil
}

// UpdateWebUIConfigs is the function used to update the WebUI configs in the
// configs file.
func UpdateWebUIConfigs(webuiCfg *WebUI) error {
	mutex.Lock()
	defer mutex.Unlock()
	
	data, err := ReadConfigs()
	if err != nil {
		return err
	}

	data.WebUI = *webuiCfg

	err = WriteConfigs(data)
	if err != nil {
		return err
	}
	
	return nil
}