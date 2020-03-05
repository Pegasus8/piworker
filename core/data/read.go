package data

import (
	"encoding/json"

	"github.com/rs/zerolog/log"
)

// GetTasks is a func that returns all the user tasks from the table `Tasks`, from the SQLite3 database.
func GetTasks() (*[]UserTask, error) {
	sqlStatement := "SELECT * FROM Tasks;"

	var tasks []UserTask

	log.Info().Str("path", DataPath).Msg("Reading user data...")

	row, err := DB.Query(sqlStatement)
	if err != nil {
		return &tasks, err
	}
	defer row.Close()

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

	log.Info().Msg("User data loaded")
	return &tasks, nil
}

// GetTaskByName is a method of the UserData struct that returns a specific task,
// searching it by it name.
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
	defer row.Close()

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

// GetTaskByID is a method of the UserData struct that returns a specific task,
// searching it by it ID.
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
	defer row.Close()

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

// GetActiveTasks is a method of the UserData struct that returns the tasks
// with the state `active`.
func GetActiveTasks() (activeTasks *[]UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE State=?;
	`
	var tasks []UserTask

	row, err := DB.Query(sqlStatement, StateTaskActive)
	if err != nil {
		return &tasks, err
	}
	defer row.Close()

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

// GetInactiveTasks is a method of the UserData struct that returns the tasks
// with the state `inactive`.
func GetInactiveTasks() (inactiveTasks *[]UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE State=?;
	`
	var tasks []UserTask

	row, err := DB.Query(sqlStatement, StateTaskInactive)
	if err != nil {
		return &tasks, err
	}
	defer row.Close()

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

// GetCompletedTasks is a method of the UserData struct that returns the tasks
// with the state `completed`.
func GetCompletedTasks() (completedTasks *[]UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE State=?;
	`
	var tasks []UserTask

	row, err := DB.Query(sqlStatement, StateTaskCompleted)
	if err != nil {
		return &tasks, err
	}
	defer row.Close()

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

// GetOnExecutionTasks is a method of the UserData struct that returns the tasks
// with the state `on-execution`.
func GetOnExecutionTasks() (onExecutionTasks *[]UserTask, err error) {
	sqlStatement := `
		SELECT * FROM Tasks
		WHERE State=?;
	`
	var tasks []UserTask

	row, err := DB.Query(sqlStatement, StateTaskOnExecution)
	if err != nil {
		return &tasks, err
	}
	defer row.Close()

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
