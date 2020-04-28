package queue

import (
	"github.com/Pegasus8/piworker/core/data"
	actions "github.com/Pegasus8/piworker/core/elements/actions/shared"
)

// Job represents a task to be executed on the pool.
type Job struct {
	TaskID     string
	Action     actions.Action
	UserAction *data.UserAction
	PreviousCR actions.ChainedResult
	OutputChan chan ExecResult
}

// Queue is where the jobs are executed.
type Queue struct {
	q chan Job
}

// ExecResult represents the returned result of a executed action.
type ExecResult struct {
	Successful  bool
	RetournedCR actions.ChainedResult
	Err         error
}
