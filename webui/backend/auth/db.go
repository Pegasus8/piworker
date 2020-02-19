package auth

import (
	"database/sql"
	"os"
	"path/filepath"
	// "os/signal"
	// "syscall"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite3 package
	"github.com/rs/zerolog/log"
)

func init() {
	// Create the path if not exists
	err := os.MkdirAll(DatabasePath, os.ModePerm)
	if err != nil {
		log.Fatal().Err(err).Str("path", DatabasePath).Msg("Cannot initialize the directory to store tokens")
	}

	Database, err = InitDB()
	if err != nil {
		log.Panic().Err(err).Msg("Cannot initialize the database to store tokens")
	}

	// go func() {
	// 	sigs := make(chan os.Signal, 1)
	// 	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	// 	// Close the database when the shutdown signal is received.
	// 	<-sigs

	// 	log.Println("Database closed")
	// }()

	err = CreateTable()
	if err != nil {
		log.Panic().Err(err).Msg("Error when trying to create the table on the tokens database")
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
func CreateTable() error {
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
	_, err := Database.Exec(sqlStatement)
	if err != nil {
		return err
	}
	return nil
}

// StoreToken is the function used to save a `UserInfo` struct into the
// sqlite3 database.
func StoreToken(authUser UserInfo) error {
	sqlStatement := `
	INSERT INTO UsersTokens(
		User,
		Token,
		ExpiresAt,
		LastTimeUsed,
		InsertedDatetime
	) values (?,?,?,?,?);
	`

	stmt, err := Database.Prepare(sqlStatement)
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
func ReadLastToken(user string) (UserInfo, error) {
	sqlStatement := `
	SELECT * FROM UsersTokens
	WHERE User=?
	ORDER BY datetime(InsertedDatetime) DESC
	LIMIT 1;
	`
	row, err := Database.Query(sqlStatement, user)
	if err != nil {
		return UserInfo{}, err
	}
	defer row.Close()

	var result UserInfo
	// Must be only one row
	for row.Next() {
		err = row.Scan(
			&result.ID,
			&result.User,
			&result.Token,
			&result.ExpiresAt,
			&result.LastTimeUsed,
			&result.InsertedDatetime,
		)
		if err != nil {
			return UserInfo{}, err
		}
	}

	return result, nil
}

// UpdateLastTimeUsed is the function used to update the the LastTimeUsed field of a specific
// register.
func UpdateLastTimeUsed(id int64, lastTimeUsed time.Time) error {
	sqlStatement := `
		UPDATE UsersTokens 
		SET LastTimeUsed = ?
		WHERE ID = ?;
	`
	_, err := Database.Exec(sqlStatement, lastTimeUsed, id)
	if err != nil {
		return err
	}

	return nil
}
