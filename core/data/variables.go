package data

import (
	"database/sql"
)

//					** Data storage **					//

// BackupLoopState is the boolean variable used to show the state of the backup loop.
//var BackupLoopState = false

// Path is the path of the user SQLite database.
var Path string = "./data/"

// Filename is the name of the SQLite file.
const Filename string = "tasks.db"

//					** Tasks's States **					//

// TaskState is the type used to represent the different states of the tasks.
type TaskState string

const (
	// StateTaskFailed is a variable that can be used in the `State`
	// field of each task. This state represents a failed task.
	StateTaskFailed TaskState = "failed"

	// StateTaskOnExecution is a variable that can be used in the `State`
	// field of each task. This state represents a task currently on execution.
	StateTaskOnExecution TaskState = "on-execution"

	// StateTaskInactive is a variable that can be used in the `State`
	// field of each task. This state represents a deactivated/inactive task.
	StateTaskInactive TaskState = "inactive"

	// StateTaskActive is a variable that can be used in the `State`
	// field of each task. This state represents an active task.
	StateTaskActive TaskState = "active"
)

// EventBus is the channel used to transport the events related to the tasks.
var EventBus chan Event

// DB is the instance of the SQLite3 database used to store the user's tasks.
var DB *sql.DB
