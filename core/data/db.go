package data

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // SQLite3 package
	"github.com/rs/zerolog/log"
)

// Init initializes the directory where the tasks will be stored (if not exists).
func Init() {
	// Create path if not exists.
	err := os.MkdirAll(Path, os.ModePerm)
	if err != nil {
		log.Fatal().Err(err).Msg("Error when trying to create data directory")
	}

	// Initialize the database.
	DB, err = InitDB(filepath.Join(Path, Filename))
	if err != nil {
		log.Panic().Err(err).Msg("Error on tasks db initialization")
	}

	// Create the table if not exists.
	err = CreateTable()
	if err != nil {
		log.Panic().Err(err).Msg("Error when trying to create the table on the tasks database")
	}
}

/*
*	Usage order:
*	1) InitDB
*	2) defer db.Close()
*	3) CreateTable
*	4) StoreRasberryStatistics/ReadRaspberryStatistics
 */

// InitDB is the function used to initialize the SQLite3 database.
func InitDB(path string) (*sql.DB, error) {
	// First check if the directory is accessible.
	dir := filepath.Dir(path)
	_, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("the db on the path '%s' is nil", path)
	}

	return db, nil
}

// CreateTable is the function that creates the table 'Tasks' into the SQLite3 database.
func CreateTable() error {
	sqlStatement := `
	CREATE TABLE IF NOT EXISTS Tasks(
		ID TEXT NOT NULL,
		Name TEXT NOT NULL,
		State TEXT NOT NULL,
		Trigger TEXT NOT NULL,
		Actions TEXT NOT NULL,
		Created DATETIME,
		LastTimeModified DATETIME
	);
	`

	_, err := DB.Exec(sqlStatement)
	if err != nil {
		return err
	}

	return nil
}
