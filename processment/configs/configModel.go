package configs

import (
	"time"
)

// Configs is the struct used to store all PiWorker configurations.
type Configs struct {
	Behavior Behavior `json:"behavior"`
	Security Security `json:"security"`
	APIConfigs APIConfigs `json:"api-configs"`
	Updates Updates `json:"updates"`
	WebUI WebUI `json:"webui"`
	Users []User `json:"users"`
}

// Behavior is the struct used to store Behavior configs of PiWorker.
type Behavior struct {
	LoopSleep int64 `json:"loop-sleep"`
}

// Security is the struct used to store Security configs of PiWorker.
type Security struct {
	DeniedIPs []string `json:"denied-ips"`
	LocalNetworkAccess bool `json:"local-network-access"`

}

// APIConfigs is the struct used to store API configs of PiWorker.
type APIConfigs struct {
	// API's States
	NewTaskAPI bool `json:"new-task-api"`
	EditTaskAPI bool `json:"edit-task-api"`
	DeleteTaskAPI bool `json:"delete-task-api"`
	GetAllTasksAPI bool `json:"get-all-tasks-api"`
	StatisticsAPI bool `json:"statistics-api"`

	// Authentication
	RequireToken bool `json:"require-token"`
	SigningKey string `json:"signing-key"`
	TokenDuration time.Duration `json:"token-duration"`
}

// Updates is the struct used to store update configs of PiWorker.
type Updates struct {
	DailyCheck bool `json:"daily-check"`
	AutoDownload bool `json:"auto-download"` // Only if daily check is active
	BugsPrevention bool `json:"bugs-prevention"`
}

// WebUI is the struct used to store web user interface configs of PiWorker.
type WebUI struct {
	Enabled bool `json:"enabled"`
	RequireCredentials bool `json:"require-credentials"`
	ListeningPort string `json:"listening-port"`
}

// User is used to store each user's credentials.
type User struct {
	Username string `json:"username"`
	// NOTE It would be safer to store the hash instead of the password 
	// in plain text, but if the user lost the password he could not 
	// retrieve it.
	Password string `json:"password"`
	Admin bool `json:"admin"`
}