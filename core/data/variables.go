package data

//					** Data storage **					//

// BackupLoopState is the boolean variable used to show the state of the backup loop.
//var BackupLoopState = false

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
