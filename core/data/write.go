package data

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// NewTask is a function used to add a new task to the table 'Tasks', on the SQLite3 database.
func NewTask(task *UserTask) error {
	sqlStatement := `
	INSERT INTO Tasks(
		ID,
		Name,
		State,
		Trigger,
		Actions,
		Created,
		LastTimeModified
	) values (?,?,?,?,?,?,?)
	`

	// Set the task ID
	task.ID = uuid.New().String()

	log.Info().Str("taskID", task.ID).Msg("Adding a new task into SQLite3 db...")

	stmt, err := DB.Prepare(sqlStatement)
	if err != nil {
		return err
	}
	defer stmt.Close()

	trigger, err := json.Marshal(task.Trigger)
	if err != nil {
		return err
	}

	actions, err := json.Marshal(task.Actions)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(
		task.ID,
		task.Name,
		task.State,
		string(trigger),
		string(actions),
		task.Created,
		task.LastTimeModified,
	)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", task.ID).Msg("Task successfully added, emiting the event...")

	event := Event{
		Type:   Added,
		TaskID: task.ID,
	}
	EventBus <- event

	return nil
}
