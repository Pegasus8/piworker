package data

import (
	"encoding/json"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// GetTasks is a method that returns all the user tasks stored in the database.
func (db *DatabaseInstance) GetTasks() (*[]UserTask, error) {
	sqlStatement := "SELECT * FROM Tasks;"

	row, err := db.instance.Query(sqlStatement)
	if err != nil {
		return nil, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Error().Err(err).Str("db", db.Path).
				Caller(zerolog.CallerSkipFrameCount).
				Msg("Error when trying to close rows")
		}
	}()

	var tasks []UserTask

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

	row, err := db.instance.Query(sqlStatement, name)
	if err != nil {
		return nil, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Error().Err(err).Caller(zerolog.CallerSkipFrameCount).Msg("Error when closing rows")
		}
	}()

	if !row.Next() {
		return nil, ErrBadTaskID
	}

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

	row, err := db.instance.Query(sqlStatement, ID)
	if err != nil {
		return nil, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Error().Err(err).Caller(zerolog.CallerSkipFrameCount).Msg("Error when closing rows")
		}
	}()

	if !row.Next() {
		return nil, ErrBadTaskID
	}

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

// GetInactiveTasks is a method that returns the tasks with the state `StateTaskInactive`.
func (db *DatabaseInstance) GetInactiveTasks() (inactiveTasks *[]UserTask, err error) {
	return db.getTasksByState(StateTaskInactive)
}

// GetFailedTasks is a method that returns the tasks with the state `StateTaskFailed`.
func (db *DatabaseInstance) GetFailedTasks() (failedTasks *[]UserTask, err error) {
	return db.getTasksByState(StateTaskFailed)
}

// GetOnExecutionTasks is a method that returns the tasks with the state `StateTaskOnExecution`.
func (db *DatabaseInstance) GetOnExecutionTasks() (onExecutionTasks *[]UserTask, err error) {
	return db.getTasksByState(StateTaskOnExecution)
}

func (db *DatabaseInstance) getTasksByState(state TaskState) (matchedTasks *[]UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE State=?;
	`

	row, err := db.instance.Query(sqlStatement, state)
	if err != nil {
		return nil, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Error().Err(err).Caller(zerolog.CallerSkipFrameCount).Msg("Error when closing rows")
		}
	}()

	var tasks []UserTask

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
