package auth

import (
	"os"
	"database/sql"
	"log"
	"path/filepath"
	"os/signal"
	"syscall"

	_ "github.com/mattn/go-sqlite3" // SQLite3 package
)

func init() {
	// Create statistics path if not exists
	err := os.MkdirAll(DatabasePath, os.ModePerm)
	if err != nil {
		log.Panicln(err)
	}

	database, err = InitDB()
	if err != nil {
		log.Panicln(err)
	}

	go func() {
		defer database.Close()
		
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		// Close the database when the shutdown signal is received.
		<-sigs
	}()

	err = CreateTable(database)
	if err != nil {
		log.Panicln(err)
	}
}

/* 
*	Usage order:
*	1) InitDB
*	2) defer db.Close()
*	3) CreateTable
*	4) StoreToken/ReadLastToken
*/

// InitDB is the function used to initialize the sqlite3 database.
func InitDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", filepath.Join(DatabasePath, DatabaseName))
	if err != nil {
		return nil, err
	}
	if db == nil {
		return db, ErrNilDB
	}

	return db, nil
}

// CreateTable is the function used to create the default tables into 
// the sqlite3 database.
func CreateTable(db *sql.DB) error {
	sqlStatement := `
	CREATE TABLE IF NOT EXISTS UsersTokens(
		ID INTEGER PRIMARY KEY AUTOINCREMENT,
		User TEXT NOT NULL,
		Token TEXT NOT NULL,
		ExpiresAt DATETIME NOT NULL, 
		LastTimeUsed DATETIME NOT NULL,
		InsertedDatetime DATETIME NOT NULL
	);
	`
	_, err := db.Exec(sqlStatement)
	if err != nil {
		return err
	}
	return nil
}
// StoreToken is the function used to save a `UserInfo` struct into the
// sqlite3 database.
func StoreToken(db *sql.DB, authUser UserInfo) error {
	sqlStatement := `
	INSERT INTO UsersTokens(
		User,
		Token,
		ExpiresAt,
		LastTimeUsed,
		InsertedDatetime
	) values (?,?,?,?,?)
	`

	stmt, err := db.Prepare(sqlStatement)
	if err != nil {
		return err
	}
	defer stmt.Close()

	stmt.Exec(
		authUser.User,
		authUser.Token,
		authUser.ExpiresAt,
		authUser.LastTimeUsed,
		authUser.InsertedDatetime,
	)
	if err != nil {
		return err
	}

	return nil
}

// ReadLastToken is the function used to read the last auth info of a user 
// from the sqlite3 database.
func ReadLastToken(db *sql.DB, user string) (UserInfo, error) {
	sqlStatement := `
	SELECT * FROM UsersTokens
	ORDER BY datetime(InsertedDatetime) DESC LIMIT 1
	WHERE User='?'
	`
	row, err := db.Query(sqlStatement, user)
	if err != nil {
		return UserInfo{}, err
	}
	defer row.Close()

	var result UserInfo
	// Must be only one row
	err = row.Scan(
		&result.User,
		&result.Token,
		&result.ExpiresAt,
		&result.LastTimeUsed,
		&result.InsertedDatetime,
	)
	if err != nil {
		return UserInfo{}, err
	}

	return result, nil
}