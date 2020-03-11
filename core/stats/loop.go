package stats

import (
	"database/sql"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

// StartLoop is the function used to start the loop used to work with
// the statistics generated by PiWorker and the Raspberry Pi where it's running.
// Returns the instance of the database with the purpose of be closed properly.
func StartLoop(statsChannel chan Statistic) *sql.DB {
	log.Info().Msg("Preparing to start stats loop...")

	log.Info().Msg("Initializing database...")
	path := filepath.Join(StatisticsPath, DatabaseName)

	db, err := InitDB(path)
	if err != nil {
		log.Fatal().Err(err).Str("path", path).Msg("Error when initializing the statistics database")
	}

	err = CreateTable(db)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Error when trying to create the table on the statistics database")
	}

	log.Info().Msg("Statistics database ready to work!")
	log.Info().Msg("Starting stats loop")

	go func() {
		for range time.Tick(5 * time.Second) {
			if err = UpdateRPiStats(); err != nil {
				log.Panic().Err(err).Msg("Error when trying to update the rpi stats")
			}
	
			Current.RLock()
			if err = StoreStats(db, &Current.TasksStats, &Current.RaspberryStats); err != nil {
				log.Panic().Err(err).Msg("Error when trying to store stats on the db")
			}
			Current.RUnlock()
		}
	}()

	return db
}
