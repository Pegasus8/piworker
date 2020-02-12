package data

import (
	"sync"
)


//					** Data storage **					//

// Filename is the name of the user JSON data file. Must be dynamically assigned
// depending of the size of the file.
var Filename = ""
// BackupLoopState is the boolean variable used to show the state of the backup loop.
var BackupLoopState = false
const (
	// DataPath is the path of the user JSON data file.
	DataPath string = "./data/" 
)
var mutex = sync.Mutex{}


//					** Tasks's States **					//

// TaskState is the type used to represent the different states of the tasks.
type TaskState string

const (
	// StateTaskCompleted is a variable that can be used in the `State` 
	// field of every task. This state represents a finished task.
	StateTaskCompleted TaskState = "completed"

	// StateTaskOnExecution is a variable that can be used in the `State` 
	// field of every task. This state represents a task currently on execution.
	StateTaskOnExecution TaskState = "on-execution"

	// StateTaskInactive is a variable that can be used in the `State` 
	// field of every task. This state represents a deactivated/inactive task.
	StateTaskInactive TaskState = "inactive"

	// StateTaskActive is a variable that can be used in the `State` 
	// field of every task. This state represents an active task.
	StateTaskActive TaskState = "active"
)
