package stats

import (
	"path/filepath"
	"time"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/rs/zerolog/log"
)

// StartLoop is the function used to start the loop used to work with
// the statistics generated by PiWorker and the Raspberry Pi where it's running.
func StartLoop(statsChannel chan Statistic, dataChannel chan data.UserData) {
	log.Info().Msg("Preparing to start stats loop...")

	log.Info().Msg("Preparing database...")
	path := filepath.Join(StatisticsPath, DatabaseName)
	db, err := InitDB(path)
	if err != nil {
		log.Fatal().Err(err).Str("path", path).Msg("Error when initializing the statistics database")
	}
	defer db.Close()
	err = CreateTable(db)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Error when trying to create the table on the statistics database")
	}
	log.Info().Msg("Statistics database ready to work!")

	log.Info().Msg("Starting stats loop...")
	for range time.Tick(1 * time.Second) {
		userData := <-dataChannel
		statistics, err := GetStatistics(&userData)
		if err != nil {
			log.Error().Err(err).Msg("Cannot get the statistics")
		}
		select {
		case statsChannel <- *statistics:
			// Sending data to channel
		default:
			// No receiver
		}

		StoreRasberryStatistics(db, statistics.RaspberryStats)
	}
}
