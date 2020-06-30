package data

import "fmt"

// DeleteTask is a function used to delete a specific task from the table 'Tasks', on the SQLite3 database.
func DeleteTask(ID string) error {
	sqlStatement := `
		DELETE FROM Tasks
		WHERE ID = ?;
	`

	r, err := DB.Exec(sqlStatement, ID)
	if err != nil {
		return err
	}

	rowsAffected, err := r.RowsAffected()
	if err != nil {
		return err
	}

	// If the task with the given ID does not exist the rows affected in the database
	// should be zero.
	if rowsAffected == 0 {
		return fmt.Errorf("the task with the ID '%s' does not exist", ID)
	}

	event := Event{
		Type:   Deleted,
		TaskID: ID,
	}
	EventBus <- event

	return nil
}
