package stats

import (
	"encoding/json"
	"time"
	"database/sql"
	"os"

	_ "github.com/mattn/go-sqlite3" // SQLite3 package
	"github.com/rs/zerolog/log"
)

func init() {
	// Create statistics path if not exists
	err := os.MkdirAll(StatisticsPath, os.ModePerm)
	if err != nil {
		log.Fatal().Err(err).Str("path", StatisticsPath).Msg("Cannot initialize the directory to store statistics")
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
	sqlStatement1 := `
	CREATE TABLE IF NOT EXISTS TasksStats(
		ActiveTasks INTEGER NOT NULL,
		InactiveTasks INTEGER NOT NULL,
		OnExecutionTasks INTEGER NOT NULL,
		FailedTasks INTEGER NOT NULL,
		AverageExecutionTime REAL NOT NULL, 
		Timestamp DATETIME
	);
	`

	sqlStatement2 := `
	CREATE TABLE IF NOT EXISTS RaspberryStats(
		Host TEXT NOT NULL,
		CPULoad REAL NOT NULL,
		Storage TEXT NOT NULL,
		RAM TEXT NOT NULL, 
		Timestamp DATETIME
	);
	`

	_, err := db.Exec(sqlStatement1)
	if err != nil {
		return err
	}

	_, err = db.Exec(sqlStatement2)
	if err != nil {
		return err
	}

	return nil
}

// StoreStats is the function used to save a slice of
// `RaspberryStats` struct into the sqlite3 database.
func StoreStats(db *sql.DB, item *Statistic) error {
	item.RLock()
	defer item.RUnlock()

	sqlStatement1 := `
	INSERT INTO TasksStats(
		ActiveTasks,
		InactiveTasks,
		OnExecutionTasks,
		FailedTasks,
		AverageExecutionTime,
		Timestamp
	) values (?,?,?,?,?,?)
	` // CURRENT_TIMESTAMP

	sqlStatement2 := `
	INSERT INTO RaspberryStats(
		Host,
		CPULoad,
		Storage,
		RAM,
		Timestamp
	) values (?,?,?,?,?)
	`

	now := time.Now()

	_, err := db.Exec(sqlStatement1,
		item.ActiveTasks,
		item.InactiveTasks,
		item.OnExecutionTasks,
		item.FailedTasks,
		item.AverageExecutionTime,
		now,
	)
	if err != nil {
		return err
	}

	host, err := json.Marshal(item.RaspberryStats.Host)
	if err != nil {
		return err
	}
	storage, err := json.Marshal(item.RaspberryStats.Storage)
	if err != nil {
		return err
	}
	ram, err := json.Marshal(item.RaspberryStats.RAM)
	if err != nil {
		return err
	}

	_, err = db.Exec(sqlStatement2,
		string(host),
		item.RaspberryStats.CPULoad,
		string(storage),
		string(ram),
		now,
	)
	if err != nil {
		return err
	}

	return nil
}

/*
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
}*/
