package configs

import (
	"sync"
)

// Filename is the name of the configs file.
const Filename string = "configs.json"

// Path is the path where the configs file wanna be saved
var Path string = "./configs/"

// DefaultConfigs is the variable that contains the default configs generally
// used when the config file not exists.
var DefaultConfigs = Configs{
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

// CurrentConfigs is the variable that contains the parsed configs
var CurrentConfigs *Configs

var mutex = sync.Mutex{}
