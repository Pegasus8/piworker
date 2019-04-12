package stats

// Stats is the struct used to work with general stats.
type Stats struct {
	Statistics []Statistic
}

// Statistic is the struct used to parse each statistic.
type Statistic struct {
	// PiWorker stats
	ActiveTasks int
	InactiveTasks int
	OnExecutionTasks int
	CompletedTasks int
	AverageExecutionTime float64 // for each task
	OperatingTime int // seconds
	BackupLoopState bool

	// Raspberry stats
	RaspberryStats RaspberryStats
}

// RaspberryStats is the struct what contains the statistics about the Raspberry device.
type RaspberryStats struct {
	Temperature float64 // ºC
	CPULoad string // %
	FreeStorage string
	FilesCreated int
	RAMUsage float64 // MB
}