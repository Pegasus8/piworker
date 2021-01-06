package configs

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
)

// NewConfig returns a new instance of `Configs`. If the target file (path + filename) doesn't exist it will be
// created directly. Otherwise, the file will be read directly.
func NewConfig(path, filename string) (*Configs, error) {
	file := filepath.Join(path, filename)
	var c *Configs

	if _, err := os.Stat(file); os.IsNotExist(err) {
		defaultConfigs := Configs{
			Behavior: Behavior{
				LoopSleep: 500, // Milliseconds
			},
			Security: Security{
				DeniedIPs:          []string{},
				LocalNetworkAccess: true,
			},
			Backups: Backups{
				BackupData:        false,
				BackupConfigs:     false,
				DataBackupPath:    ".",
				ConfigsBackupPath: ".",
				Freq:              24, // 1 day
			},
			APIConfigs: APIConfigs{
				NewTaskAPI:     true,
				EditTaskAPI:    true,
				DeleteTaskAPI:  true,
				GetAllTasksAPI: true,
				StatisticsAPI:  true,
				LogsAPI:        true,
				TypesCompatAPI: true,
				RequireToken:   true,
				SigningKey:     "",
				TokenDuration:  168, // 7 days
			},
			Updates: Updates{
				DailyCheck:     true,
				AutoDownload:   true,
				BugsPrevention: true,
			},
			WebUI: WebUI{
				Enabled:       true,
				ListeningPort: "8080",
			},
			Users: []User{},
		}

		err = writeToFile(file, &defaultConfigs, true)
		if err != nil {
			return c, err
		}

		c = &defaultConfigs
	} else {
		c, err = readFromFile(file)
		if err != nil {
			return c, err
		}
	}

	c.path = file

	return c, nil
}

// writeToFile is a function used to write the configs into the configs file,
// overwriting the previous content if exists. Use carefully.
func writeToFile(path string, configs *Configs, safe bool) error {
	if safe {
		configs.RLock()
		defer configs.RUnlock()
	}

	byteData, err := json.MarshalIndent(configs, "", "   ")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(path, byteData, 0644)
	if err != nil {
		return err
	}

	return nil
}
