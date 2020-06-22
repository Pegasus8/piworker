package data

// DeleteTask is a function used to delete a specific task from the table 'Tasks', on the SQLite3 database.
func DeleteTask(ID string) error {
	sqlStatement := `
		DELETE FROM Tasks
		WHERE ID = ?;
	`

	_, err := DB.Exec(sqlStatement, ID)
	if err != nil {
		return err
	}

	event := Event{
		Type:   Deleted,
		TaskID: ID,
	}
	EventBus <- event

	return nil
}
