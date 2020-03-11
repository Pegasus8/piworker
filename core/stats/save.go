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
func StoreStats(db *sql.DB, ts *TasksStats, rs *RaspberryStats) error {
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
		ts.ActiveTasks,
		ts.InactiveTasks,
		ts.OnExecutionTasks,
		ts.FailedTasks,
		ts.AverageExecutionTime,
		now,
	)
	if err != nil {
		return err
	}

	host, err := json.Marshal(rs.Host)
	if err != nil {
		return err
	}
	storage, err := json.Marshal(rs.Storage)
	if err != nil {
		return err
	}
	ram, err := json.Marshal(rs.RAM)
	if err != nil {
		return err
	}

	_, err = db.Exec(sqlStatement2,
		string(host),
		rs.CPULoad,
		string(storage),
		string(ram),
		now,
	)
	if err != nil {
		return err
	}

	return nil
}


// ReadStats reads the statistics stored in the stats database on a specific period of time.
func ReadStats(db *sql.DB, from, to string) (*[]TasksStats, *[]RaspberryStats, error) {
	// sqlStatement := `
	// SELECT * FROM RaspberryStats
	// ORDER BY datetime(Timestamp) DESC
	// `
	sqlStatement1 := `
		SELECT * FROM TasksStats
		WHERE date(Timestamp) BETWEEN ? AND ?;
	`
	sqlStatement2 := `
		SELECT * FROM RaspberryStats
		WHERE date(Timestamp) BETWEEN ? AND ?;
	`
	var ts []TasksStats
	var rs []RaspberryStats

	tsRows, err := db.Query(sqlStatement1, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer tsRows.Close()

	
	for tsRows.Next() {
		var item TasksStats
		var avg float64

		err = tsRows.Scan(
			&item.ActiveTasks,
			&item.InactiveTasks,
			&item.OnExecutionTasks,
			&item.FailedTasks,
			&avg,
			&item.Timestamp,
		)
		if err != nil {
			return &ts, &rs, err
		}

		item.AverageExecutionTime = time.Duration(avg)

		ts = append(ts, item)
	}

	rsRows, err := db.Query(sqlStatement2, from, to)
	if err != nil {
		return &ts, &rs, err
	}
	defer rsRows.Close()

	for rsRows.Next() {
		var item RaspberryStats
		var host string
		var storage string
		var ram string

		err = rsRows.Scan(
			&host,
			&item.CPULoad,
			&storage,
			&ram,
			&item.Timestamp,
		)
		if err != nil {
			return &ts, &rs, err
		}

		err = json.Unmarshal([]byte(host), &item.Host)
		if err != nil {
			return &ts, &rs, err
		}

		err = json.Unmarshal([]byte(storage), &item.Storage)
		if err != nil {
			return &ts, &rs, err
		}

		err = json.Unmarshal([]byte(ram), &item.RAM)
		if err != nil {
			return &ts, &rs, err
		}

		rs = append(rs, item)
	}

	return &ts, &rs, nil
}
