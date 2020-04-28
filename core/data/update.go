package data

import (
	"encoding/json"

	"github.com/rs/zerolog/log"
)

// UpdateTask is a function used to update an existing task from the JSON data file.
func UpdateTask(ID string, updatedTask *UserTask) error {
	sqlStatement := `
		UPDATE Tasks 
		SET Name = ?, State = ?, Trigger = ?, Actions = ?, LastTimeModified = ? 
		WHERE ID = ?;
	`
	var trigger string
	var actions string

	log.Info().Str("taskID", ID).Msg("Updating task...")

	// Marshal the UserTrigger struct
	t, err := json.Marshal(updatedTask.Trigger)
	if err != nil {
		return err
	}
	trigger = string(t)

	// Marshal the []UserAction slice
	a, err := json.Marshal(updatedTask.Actions)
	if err != nil {
		return err
	}
	actions = string(a)

	_, err = DB.Exec(sqlStatement,
		updatedTask.Name,
		updatedTask.State,
		trigger,
		actions,
		updatedTask.LastTimeModified,
		ID,
	)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", ID).Msg("Task updated successfully, emiting the event...")

	event := Event{
		Type:   Modified,
		TaskID: ID,
	}
	EventBus <- event

	return nil
}

// UpdateTaskState is a function used to change the state of a task.
func UpdateTaskState(ID string, newState TaskState) error {
	log.Info().Str("taskID", ID).Msg("Updating task state...")

	sqlStatement := `
		UPDATE Tasks 
		SET State = ?
		WHERE ID = ?;
	`
	log.Info().Str("taskID", ID).Msg("Updating task...")

	_, err := DB.Exec(sqlStatement,
		newState,
		ID,
	)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", ID).Msg("Task state updated successfully")
	return nil
}
