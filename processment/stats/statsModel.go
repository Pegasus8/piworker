package stats

import "time"

// Stats is the struct used to work with general stats.
type Stats struct {
	Statistics []Statistic
}

// Statistic is the struct used to parse each statistic.
type Statistic struct {
	// PiWorker stats
	ActiveTasks          int     `json:"activeTasks"`
	InactiveTasks        int     `json:"inactiveTasks"`
	OnExecutionTasks     int     `json:"onExecutionTasks"`
	CompletedTasks       int     `json:"completedTasks"`
	AverageExecutionTime float64 `json:"averageExecutionTime"` // for each task
	OperatingTime        int     `json:"operatingTime"`        // seconds
	BackupLoopState      bool    `json:"backupLoopState"`

	// Raspberry stats
	RaspberryStats RaspberryStats `json:"raspberryStats"`
}

// RaspberryStats is the struct what contains the statistics about the Raspberry device.
type RaspberryStats struct {
	Temperature float64   `json:"temperature"` // ºC
	CPULoad     string    `json:"cpuLoad"`     // %
	FreeStorage string    `json:"freeStorage"`
	RAMUsage    string    `json:"ramUsage"`
	Timestamp   time.Time `json:"timestamp"`
}
