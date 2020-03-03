package data

import (
	"github.com/rs/zerolog/log"
)

// DeleteTask is a function used to delete a specific task from the table 'Tasks', on the SQLite3 database.
func DeleteTask(ID string) error {
	sqlStatement := `
		DELETE FROM Tasks
		WHERE ID = ?;
	`
	log.Info().Str("taskID", ID).Msg("Deleting task...")

	_, err := DB.Exec(sqlStatement, ID)
	if err != nil {
		return err
	}

	log.Info().Str("taskID", ID).Msg("Task deleted successfully")
	return nil
}
