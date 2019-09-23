package configs

import (
	"sync"
	"time"
)

// Filename is the name of the configs file.
const Filename string = "configs.json"

// ConfigsPath is the path where the configs file wanna be saved
const ConfigsPath string = "./configs/"

// DefaultConfigs is the variable that contains the default configs generally 
// used when the config file not exists.
var DefaultConfigs = Configs{
	Behavior: Behavior{
		LoopSleep: 0.5,
	},
	Security: Security{
		DeniedIPs: []string{},
		LocalNetworkAccess: true,
	},
	APIConfigs: APIConfigs{
		NewTaskAPI: true,
		EditTaskAPI: true,
		DeleteTaskAPI: true,
		GetAllTasksAPI: true,
		StatisticsAPI: true,
		RequireToken: true,
		SigningKey: "",
		TokenDuration: time.Hour * 168, // 7 days 
	},
	Updates: Updates{
		DailyCheck: true,
		AutoDownload: true,
		BugsPrevention: true,
	},
	WebUI: WebUI{
		Enabled: true,
		RequireCredentials: true,
		ListeningPort: "8080",
 	},
}

// CurrentConfigs is the variable that contains the parsed configs
var CurrentConfigs *Configs

var mutex = sync.Mutex{}