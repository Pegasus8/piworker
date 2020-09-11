package data

import (
	"encoding/json"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// GetTasks is a method that returns all the user tasks stored in the database.
func (db *DatabaseInstance) GetTasks() (*[]UserTask, error) {
	sqlStatement := "SELECT * FROM Tasks;"
	var tasks []UserTask

	row, err := db.Query(sqlStatement)
	if err != nil {
		return &tasks, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Error().Err(err).Str("db", Filename).
				Caller(zerolog.CallerSkipFrameCount).
				Msg("Error when trying to close rows")
		}
	}()

	for row.Next() {
		var task UserTask
		var trigger string
		var actions string
		err = row.Scan(
			&task.ID,
			&task.Name,
			&task.State,
			&trigger,
			&actions,
			&task.Created,
			&task.LastTimeModified,
		)
		if err != nil {
			return &tasks, err
		}

		// Parse the Trigger string into the proper struct.
		err = json.Unmarshal([]byte(trigger), &task.Trigger)
		if err != nil {
			return &tasks, err
		}

		// Parse the Actions string into the proper struct.
		err = json.Unmarshal([]byte(actions), &task.Actions)
		if err != nil {
			return &tasks, err
		}

		tasks = append(tasks, task)
	}

	return &tasks, nil
}

// GetTaskByName is a method that returns a specific task, searching it by the name on the tasks database.
func (db *DatabaseInstance) GetTaskByName(name string) (taskFound *UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE Name=?;
	`
	var task UserTask
	var trigger string
	var actions string

	row, err := db.Query(sqlStatement, name)
	if err != nil {
		return &task, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Error().Err(err).Caller(zerolog.CallerSkipFrameCount).Msg("Error when closing rows")
		}
	}()

	if !row.Next() {
		return &task, ErrBadTaskID
	}

	err = row.Scan(
		&task.ID,
		&task.Name,
		&task.State,
		&trigger,
		&actions,
		&task.Created,
		&task.LastTimeModified,
	)
	if err != nil {
		return &task, err
	}

	// Parse the Trigger string into the proper struct.
	err = json.Unmarshal([]byte(trigger), &task.Trigger)
	if err != nil {
		return &task, err
	}

	// Parse the Actions string into the proper struct.
	err = json.Unmarshal([]byte(actions), &task.Actions)
	if err != nil {
		return &task, err
	}

	return &task, nil
}

// GetTaskByID is a method that returns a specific task, searching it by the ID on the database.
func (db *DatabaseInstance) GetTaskByID(ID string) (taskFound *UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE ID=?;
	`
	var task UserTask
	var trigger string
	var actions string

	row, err := db.Query(sqlStatement, ID)
	if err != nil {
		return &task, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Error().Err(err).Caller(zerolog.CallerSkipFrameCount).Msg("Error when closing rows")
		}
	}()

	if !row.Next() {
		return &task, ErrBadTaskID
	}

	err = row.Scan(
		&task.ID,
		&task.Name,
		&task.State,
		&trigger,
		&actions,
		&task.Created,
		&task.LastTimeModified,
	)
	if err != nil {
		return &task, err
	}

	// Parse the Trigger string into the proper struct.
	err = json.Unmarshal([]byte(trigger), &task.Trigger)
	if err != nil {
		return &task, err
	}

	// Parse the Actions string into the proper struct.
	err = json.Unmarshal([]byte(actions), &task.Actions)
	if err != nil {
		return &task, err
	}

	return &task, nil
}

// GetActiveTasks is a method that returns the tasks with the state `StateTaskActive`.
func (db *DatabaseInstance) GetActiveTasks() (activeTasks *[]UserTask, err error) {
	return db.getTasksByState(StateTaskActive)
}

// GetInactiveTasks is a method that returns the tasks with the state
// `StateTaskInactive`.
func (db *DatabaseInstance) GetInactiveTasks() (inactiveTasks *[]UserTask, err error) {
	return db.getTasksByState(StateTaskInactive)
}

// GetFailedTasks is a method that returns the tasks with the state `StateTaskFailed`.
func (db *DatabaseInstance) GetFailedTasks() (failedTasks *[]UserTask, err error) {
	return db.getTasksByState(StateTaskFailed)
}

// GetOnExecutionTasks is a method that returns the tasks with the state
// `StateTaskOnExecution`.
func (db *DatabaseInstance) GetOnExecutionTasks() (onExecutionTasks *[]UserTask, err error) {
	return db.getTasksByState(StateTaskOnExecution)
}

func (db *DatabaseInstance) getTasksByState(state TaskState) (matchedTasks *[]UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE State=?;
	`
	var tasks []UserTask

	row, err := db.Query(sqlStatement, state)
	if err != nil {
		return &tasks, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Error().Err(err).Caller(zerolog.CallerSkipFrameCount).Msg("Error when closing rows")
		}
	}()

	for row.Next() {
		var task UserTask
		var trigger string
		var actions string
		err = row.Scan(
			&task.ID,
			&task.Name,
			&task.State,
			&trigger,
			&actions,
			&task.Created,
			&task.LastTimeModified,
		)
		if err != nil {
			return &tasks, err
		}

		// Parse the Trigger string into the proper struct.
		err = json.Unmarshal([]byte(trigger), &task.Trigger)
		if err != nil {
			return &tasks, err
		}

		// Parse the Actions string into the proper struct.
		err = json.Unmarshal([]byte(actions), &task.Actions)
		if err != nil {
			return &tasks, err
		}

		tasks = append(tasks, task)
	}

	return &tasks, nil
}
