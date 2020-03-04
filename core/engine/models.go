package engine

import (
	"time"
	"sync"
)

// TasksStats is the struct used to parse the statistics related with the execution of the tasks.
type TasksStats struct{
	Active uint16
	OnExec uint8
	Inactive uint16
	AvgExecTime time.Duration
	*sync.RWMutex
	observations uint32
}
