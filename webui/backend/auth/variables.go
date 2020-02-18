package auth

import (
	"database/sql"
	"github.com/Pegasus8/piworker/core/data"
)

const (
	// DatabaseName is the name of the sqlite3 database used for storage of auth info.
	DatabaseName = "tokens.db"
	// DatabasePath is the path where the auth info will be saved.
	DatabasePath = data.DataPath
)

// Database is the tokens database instance. Need the execution of the function `InitDB` for initialization.
var Database *sql.DB
