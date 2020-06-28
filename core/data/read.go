package data

import (
	"encoding/json"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// GetTasks is a func that returns all the user tasks from the table `Tasks`, from the SQLite3 database.
func GetTasks() (*[]UserTask, error) {
	sqlStatement := "SELECT * FROM Tasks;"
	var tasks []UserTask

	row, err := DB.Query(sqlStatement)
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

// GetTaskByName is a function that returns a specific task,
// searching it by the name on the tasks database.
func GetTaskByName(name string) (taskFound *UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE Name=?;
	`
	var task UserTask
	var trigger string
	var actions string

	row, err := DB.Query(sqlStatement, name)
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

// GetTaskByID is a function that returns a specific task,
// searching it by the ID on the tasks database.
func GetTaskByID(ID string) (taskFound *UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE ID=?;
	`
	var task UserTask
	var trigger string
	var actions string

	row, err := DB.Query(sqlStatement, ID)
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

// GetActiveTasks is a function that returns the tasks
// with the state `active` from the tasks database.
func GetActiveTasks() (activeTasks *[]UserTask, err error) {
	return getTasksByState(StateTaskActive)
}

// GetInactiveTasks is a function that returns the tasks
// with the state `inactive` from the tasks database.
func GetInactiveTasks() (inactiveTasks *[]UserTask, err error) {
	return getTasksByState(StateTaskInactive)
}

// GetFailedTasks is a function that returns the tasks
// with the state `failed` from the tasks database.
func GetFailedTasks() (failedTasks *[]UserTask, err error) {
	return getTasksByState(StateTaskFailed)
}

// GetOnExecutionTasks is a method of the UserData struct that returns the tasks
// with the state `on-execution`.
func GetOnExecutionTasks() (onExecutionTasks *[]UserTask, err error) {
	return getTasksByState(StateTaskOnExecution)
}

func getTasksByState(state TaskState) (matchedTasks *[]UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE State=?;
	`
	var tasks []UserTask

	row, err := DB.Query(sqlStatement, state)
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
