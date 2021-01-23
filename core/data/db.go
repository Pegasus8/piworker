package data

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // SQLite3 package
)

// NewDB initializes the database where tasks will be saved and read.
func NewDB(path, filename string) (*DatabaseInstance, error) {
	// Check if the path is accessible.
	_, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// Initialize the database.
	db, err := initDB(filepath.Join(path, filename))
	if err != nil {
		return nil, err
	}

	// Create the table if not exists.
	err = createTables(db)
	if err != nil {
		return nil, err
	}

	d := DatabaseInstance{
		Path:     filepath.Join(path, filename),
		EventBus: make(chan Event),
		instance: db,
	}

	return &d, nil
}

func (db *DatabaseInstance) Close() error {
	return db.instance.Close()
}

// GetSQLInstance returns a pointer to the instance of the database itself. Mainly needed to execute custom queries.
func (db *DatabaseInstance) GetSQLInstance() *sql.DB {
	return db.instance
}

// initDB is the function used to initialize the SQLite3 database.
func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("the db on the path '%s' is nil", path)
	}

	return db, nil
}

// createTable is the function that creates the table 'Tasks' into the SQLite3 database.
func createTables(db *sql.DB) error {
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

CREATE TABLE IF NOT EXISTS variables_local(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    content TEXT NOT NULL,
    type TEXT NOT NULL,
    parent_task_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS variables_global(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    content TEXT NOT NULL,
    type TEXT NOT NULL
);
	`

	_, err := db.Exec(sqlStatement)
	if err != nil {
		return err
	}

	return nil
}
