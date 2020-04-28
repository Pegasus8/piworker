package configs

import (
	"sync"
)

// Configs is the struct used to store all PiWorker configurations.
type Configs struct {
	sync.RWMutex `json:"-"`
	Behavior     Behavior   `json:"behavior"`
	Security     Security   `json:"security"`
	Backups      Backups    `json:"backups"`
	APIConfigs   APIConfigs `json:"api-configs"`
	Updates      Updates    `json:"updates"`
	WebUI        WebUI      `json:"webui"`
	Users        []User     `json:"users"`
}

// Behavior is the struct used to store Behavior configs of PiWorker.
type Behavior struct {
	LoopSleep int64 `json:"loop-sleep(ms)"`
}

// Security is the struct used to store Security configs of PiWorker.
type Security struct {
	DeniedIPs          []string `json:"denied-ips"`
	LocalNetworkAccess bool     `json:"local-network-access"`
}

// Backups is the struct used to store Backups configs of PiWorker.
type Backups struct {
	BackupData        bool   `json:"backup-data"`
	BackupConfigs     bool   `json:"backup-configs"`
	DataBackupPath    string `json:"data-backup-path"`
	ConfigsBackupPath string `json:"configs-backup-path"`
	Freq              int16  `json:"frequency(hs)"`
}

// APIConfigs is the struct used to store API configs of PiWorker.
type APIConfigs struct {
	// API's States
	NewTaskAPI     bool `json:"new-task-api"`
	EditTaskAPI    bool `json:"edit-task-api"`
	DeleteTaskAPI  bool `json:"delete-task-api"`
	GetAllTasksAPI bool `json:"get-all-tasks-api"`
	StatisticsAPI  bool `json:"statistics-api"`
	LogsAPI        bool `json:"logs-api"`

	// Authentication
	RequireToken  bool   `json:"require-token"`
	SigningKey    string `json:"signing-key"`
	TokenDuration int64  `json:"token-duration(hs)"`
}

// Updates is the struct used to store update configs of PiWorker.
type Updates struct {
	DailyCheck     bool `json:"daily-check"`
	AutoDownload   bool `json:"auto-download"` // Only if daily check is active
	BugsPrevention bool `json:"bugs-prevention"`
}

// WebUI is the struct used to store web user interface configs of PiWorker.
type WebUI struct {
	Enabled            bool   `json:"enabled"`
	RequireCredentials bool   `json:"require-credentials"`
	ListeningPort      string `json:"listening-port"`
}

// User is used to store each user's credentials.
type User struct {
	Username string `json:"username"`
	// NOTE It would be safer to store the hash instead of the password
	// in plain text, but if the user lost the password he could not
	// retrieve it.
	PasswordHash string `json:"password-hash"`
	Admin        bool   `json:"admin"`
}
