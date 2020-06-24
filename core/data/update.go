package data

import (
	"encoding/json"
	"fmt"
)

// UpdateTask is a function used to update an existing task from the JSON data file.
func UpdateTask(ID string, updatedTask *UserTask) error {
	if c := checkIntegrity(updatedTask); !c {
		return ErrIntegrity
	}

	sqlStatement := `
		UPDATE Tasks 
		SET Name = ?, State = ?, Trigger = ?, Actions = ?, LastTimeModified = ? 
		WHERE ID = ?;
	`
	var trigger string
	var actions string

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

	r, err := DB.Exec(sqlStatement,
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

	rowsAffected, err := r.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("the task with the ID '%s' does not exist", ID)
	}

	event := Event{
		Type:   Modified,
		TaskID: ID,
	}
	EventBus <- event

	return nil
}

// UpdateTaskState is a function used to change the state of a task.
func UpdateTaskState(ID string, newState TaskState) error {
	if !(newState == StateTaskActive || newState == StateTaskInactive || newState == StateTaskFailed ||
		newState == StateTaskOnExecution) {
		return ErrIntegrity
	}

	sqlStatement := `
		UPDATE Tasks 
		SET State = ?
		WHERE ID = ?;
	`

	_, err := DB.Exec(sqlStatement,
		newState,
		ID,
	)
	if err != nil {
		return err
	}

	return nil
}
