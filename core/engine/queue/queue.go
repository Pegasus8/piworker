package queue

import (
	"runtime"

	"github.com/Pegasus8/piworker/core/data"
	actions "github.com/Pegasus8/piworker/core/elements/actions/shared"

	"github.com/rs/zerolog/log"
)

// NewQueue initializes the pool of actions execution.
func NewQueue() *Queue {
	var maxWorkers = runtime.NumCPU()

	// Is this possible?
	if maxWorkers <= 0 {
		maxWorkers = 2
	}

	queue := &Queue{
		q: make(chan Job),
	}

	for i := 1; i <= maxWorkers; i++ {
		go worker(i, queue.q)
	}

	return queue
}

// AddJob adds a new job to be processed by the workers.
func (q *Queue) AddJob(taskID string, action actions.Action, userAction *data.UserAction, previousCR actions.ChainedResult) (result chan ExecResult) {
	j := Job{
		TaskID:     taskID,
		Action:     action,
		UserAction: userAction,
		PreviousCR: previousCR,
		OutputChan: make(chan ExecResult),
	}

	q.q <- j

	return j.OutputChan
}

func worker(id int, inputs chan Job) {
	log.Info().Int("workerID", id).Msg("Starting worker")

	for {
		job := <-inputs
		log.Info().Int("workerID", id).Str("taskID", job.TaskID).Msg("New job received!")

		// Action execution
		r, cr, err := job.Action.Run(&job.PreviousCR, job.UserAction, job.TaskID)
		execResult := ExecResult{
			Successful:  r,
			RetournedCR: *cr,
			Err:         err,
		}

		log.Info().Int("workerID", id).Str("taskID", job.TaskID).Msg("Job executed!, reporting result...")
		job.OutputChan <- execResult
	}
}
