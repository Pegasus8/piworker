package stats

import (
	"os"
	"database/sql"

	"github.com/Pegasus8/piworker/utilities/log"
	
	_ "github.com/mattn/go-sqlite3" // SQLite3 package
)

func init() {
	// Create statistics path if not exists
	err := os.MkdirAll(SatisticsPath, os.ModePerm)
	if err != nil {
		log.Fatalln(err)
	}
}

/* 
*	Usage order:
*	1) InitDB
*	2) defer db.Close()
*	3) CreateTable
*	4) StoreRasberryStatistics/ReadRaspberryStatistics
*/

// InitDB is the function used to initialize the sqlite3 database.
func InitDB(filepath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", filepath)
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
	CREATE TABLE IF NOT EXISTS RaspberryStats(
		Temperature REAL NOT NULL,
		CPULoad TEXT NOT NULL,
		FreeStorage TEXT NOT NULL,
		RAMUsage TEXT NOT NULL, 
		Timestamp DATETIME
	);
	`
	_, err := db.Exec(sqlStatement)
	if err != nil {
		return err
	}
	return nil
}

// StoreRasberryStatistics is the function used to save a slice of 
// `RaspberryStats` struct into the sqlite3 database.
func StoreRasberryStatistics(db *sql.DB, items *[]RaspberryStats) error {
	sqlStatement := `
	INSERT INTO RaspberryStats(
		Temperature,
		CPULoad,
		FreeStorage,
		RAMUsage,
		Timestamp
	) values (?,?,?,?,?)
	` // CURRENT_TIMESTAMP

	stmt, err := db.Prepare(sqlStatement)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, item := range *items {
		_, err = stmt.Exec(
			item.Temperature,
			item.CPULoad,
			item.FreeStorage,
			item.RAMUsage,
			item.Timestamp,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// ReadRaspberryStatistics is the function used to read the raspberry's 
// statistics stored in the sqlite3 database.
func ReadRaspberryStatistics(db *sql.DB) ([]RaspberryStats, error) {
	sqlStatement := `
	SELECT * FROM RaspberryStats
	ORDER BY datetime(Timestamp) DESC
	`

	rows, err := db.Query(sqlStatement)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []RaspberryStats
	for rows.Next() {
		var item RaspberryStats
		err = rows.Scan(
			&item.Temperature,
			&item.CPULoad,
			&item.FreeStorage,
			&item.RAMUsage,
			&item.Timestamp,
		)
		if err != nil {
			return nil, err
		}

		result = append(result, item)
	}

	return result, nil
}