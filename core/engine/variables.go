package engine

import (
	"os"
	"path/filepath"
)

var (
	// TempDir is the directory used to store the temp files of the Dynamic Engine
	TempDir = filepath.Join(os.TempDir(), "/PiWorker/")
)

// Stats is the variable that holds the different statistics of the tasks's execution.
var Stats TasksStats
