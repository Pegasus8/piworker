package configs

// Configs is the struct used to store all PiWorker configurations.
type Configs struct {
	Behavior Behavior `json:"behavior"`
	Security Security `json:"security"`
	APIConfigs APIConfigs `json:"api-configs"`
	Updates Updates `json:"updates"`
	WebUI WebUI `json:"webui"`
}

// Behavior is the struct used to store Behavior configs of PiWorker.
type Behavior struct {
	LoopSleep float64 `json:"loop-sleep"`
}

// Security is the struct used to store Security configs of PiWorker.
type Security struct {
	DeniedIPs []string `json:"denied-ips"`
	// DeniedMACAdresses []string `json:"denied-macadresses"` 
	LocalNetworkAccess bool `json:"local-network-access"`

}

// APIConfigs is the struct used to store API configs of PiWorker.
type APIConfigs struct {
	// API's States
	NewTaskAPI bool `json:"newtask-api"`
	EditTaskAPI bool `json:"edittask-api"`
	DeleteTaskAPI bool `json:"deletetask-api"`

	// Autentication
	RequireToken bool `json:"require-token"`
	Token string `json:"token"`
}

// Updates is the struct used to store update configs of PiWorker.
type Updates struct {
	DailyCheck bool `json:"daily-check"`
	AutoDownload bool `json:"auto-download"` // Only if daily check is active
	BugsPrevention bool `json:"bugs-prevention"`
}

// WebUI is the struct used to store web user interface configs of PiWorker.
type WebUI struct {
	RequireToken bool `json:"require-token"`
	TokenToAccess string `json:"token"`
}