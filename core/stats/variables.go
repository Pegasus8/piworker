package stats

import (
	"database/sql"
	"sync"
)

const (
	// DatabaseName is the name of the sqlite3 database used for storage of some statistics
	DatabaseName = "stats.db"

	// StatisticsPath is the path where the stats will be saved.
	StatisticsPath = "./statistics/"
)

// Current is the variable that holds the different statistics of the tasks's execution and the
// Raspberry Pi running PiWorker.
var Current struct {
	TasksStats     TasksStats
	RaspberryStats RaspberryStats
	sync.RWMutex
}

// DB is the instance of the stats SQLite3 database.
var DB *sql.DB
