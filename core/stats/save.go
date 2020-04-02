package stats

import (
	"fmt"
	"strconv"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite3 package
	"github.com/rs/zerolog/log"
)

func init() {
	// Create statistics path if not exists
	err := os.MkdirAll(StatisticsPath, os.ModePerm)
	if err != nil {
		log.Fatal().Err(err).Str("path", StatisticsPath).Msg("Cannot initialize the directory to store statistics")
	}

	path := filepath.Join(StatisticsPath, DatabaseName)

	DB, err = InitDB(path)
	if err != nil {
		log.Panic().Err(err).Str("path", path).Msg("Error when initializing the statistics database")
	}

	err = CreateTable()
	if err != nil {
		log.Panic().
			Err(err).
			Msg("Error when trying to create the table on the statistics database")
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
// the SQLite3 database.
func CreateTable() error {
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

	_, err := DB.Exec(sqlStatement1)
	if err != nil {
		return err
	}

	_, err = DB.Exec(sqlStatement2)
	if err != nil {
		return err
	}

	return nil
}

// StoreTStats stores a instance of the struct `TasksStats` into the table `TasksStats` of the SQLite3 database.
func StoreTStats(ts *TasksStats) error {
	sqlStatement := `
	INSERT INTO TasksStats(
		ActiveTasks,
		InactiveTasks,
		OnExecutionTasks,
		FailedTasks,
		AverageExecutionTime,
		Timestamp
	) values (?,?,?,?,?,?)
	`
	now := time.Now()

	_, err := DB.Exec(sqlStatement,
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

	return nil
}

// StoreRStats stores a instance of the struct `RaspberryStats` into the table `RaspberryStats` of the SQLite3 database.
func StoreRStats(rs *RaspberryStats) error {
	 // CURRENT_TIMESTAMP

	sqlStatement := `
	INSERT INTO RaspberryStats(
		Host,
		CPULoad,
		Storage,
		RAM,
		Timestamp
	) values (?,?,?,?,?)
	`

	now := time.Now()

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

	_, err = DB.Exec(sqlStatement,
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

// ReadStatsByDate returns the statistics of a specific date.
func ReadStatsByDate(date string) (*[]TasksStats, *[]RaspberryStats, error) {
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

	// Parse the given date. The stats will be obtained from this date.
	f, err := time.Parse("2006-01-02", date)
	if err != nil {
		return &ts, &rs, err
	}

	// Add a day to the date used as start point. The stats will be obtained until
	// this date.
	t := f.Add(24 * time.Hour)

	// Get the strings of the dates with the format YYYY-MM-DD.
	from := f.Format("2006-01-02")
	to := t.Format("2006-01-02")

	// Database query of tasks stats.
	tsRows, err := DB.Query(sqlStatement1, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer tsRows.Close()

	// Parse the obtained rows.
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

	// Database query of raspberry stats.
	rsRows, err := DB.Query(sqlStatement2, from, to)
	if err != nil {
		return &ts, &rs, err
	}
	defer rsRows.Close()

	// Parse the obtained rows.
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
			return &ts, &[]RaspberryStats{}, err
		}

		err = json.Unmarshal([]byte(host), &item.Host)
		if err != nil {
			return &ts, &[]RaspberryStats{}, err
		}

		err = json.Unmarshal([]byte(storage), &item.Storage)
		if err != nil {
			return &ts, &[]RaspberryStats{}, err
		}

		err = json.Unmarshal([]byte(ram), &item.RAM)
		if err != nil {
			return &ts, &[]RaspberryStats{}, err
		}

		rs = append(rs, item)
	}

	// Only return one RaspberryState per hour, avoiding a big amount of data (entire day running = 1440 entries).
	// This limitation is especially for the performance of the chart on the WebUI.
	r, err := rsPerHour(&rs)
	if err != nil {
		// Why do not return `rs` instead? Because if for some reason, the filtering cannot
		// be executed, a big amount of data will be sended. To avoid that strange but
		// possible situation, directly return a new empty instance.
		return &ts, &[]RaspberryStats{}, err
	}

	return &ts, r, nil
}

// ReadStatsByHour returns the statistics of a specific date and hour.
func ReadStatsByHour(date, hour string) (*[]TasksStats, *[]RaspberryStats, error) {
	sqlStatement1 := `
		SELECT * FROM TasksStats
		WHERE Timestamp >= ? AND Timestamp <= ?;
	`
	sqlStatement2 := `
		SELECT * FROM RaspberryStats
		WHERE Timestamp >= ? AND Timestamp <= ?;
	`
	var ts []TasksStats
	var rs []RaspberryStats

	// Parse the given date and hour. Like the function `ReadStatsByDate`, this will be used
	// as a start point.
	f, err := time.Parse("2006-01-02 15:04", date+" "+hour)
	if err != nil {
		return &ts, &rs, err
	}

	// Add an hour to the parsed time. As before, this will be used as a limit date.
	t := f.Add(1 * time.Hour)

	// Get the strings of the dates with the format YYYY-MM-DD HH:MM.
	from := f.Format("2006-01-02 15:04")
	to := t.Format("2006-01-02 15:04")

	// Database query.
	tsRows, err := DB.Query(sqlStatement1, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer tsRows.Close()

	// Parse the obtained rows.
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

	// Database query.
	rsRows, err := DB.Query(sqlStatement2, from, to)
	if err != nil {
		return &ts, &rs, err
	}
	defer rsRows.Close()

	// Parse the obtained rows.
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

func rsPerHour (rs *[]RaspberryStats) (*[]RaspberryStats, error) {
	if len(*rs) == 0 {
		return rs, nil
	}

	var ns []RaspberryStats
	var h uint64
	var hStr string

	// The first element of the slice is the first record of the day, so must be 
	// the start point.
	h, err := strconv.ParseUint((*rs)[0].Timestamp.Format("15"), 10, 64)
	if err != nil {
		return &ns, err
	}

	for _, s := range *rs {
		if h > 9 {
			hStr = fmt.Sprintf("%d", h)
		} else {
			hStr = fmt.Sprintf("0%d", h)
		}

		// Check if the hour of the timestamp is the required, if not, skip the iteration.
		if s.Timestamp.Format("15") != hStr {
			continue
		}

		// If the hour of the timestamp is the one that we've been searching for,
		// append the statistic to the `ns` slice.
		ns = append(ns, s)

		// If the variable used identify the hours equals to 23, there is no need of keep
		// iterating (unless days of more than 24 hours exist...).
		if h == 23 {
			break
		}

		h++
	}

	return &ns, nil
}
