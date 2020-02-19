package engine

import (
	"os"
	"time"

	"github.com/Pegasus8/piworker/core/configs"
	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/stats"
	"github.com/Pegasus8/piworker/webui/backend"
	"github.com/rs/zerolog/log"
)

// StartEngine is the function used to start the Dynamic Engine
func StartEngine() {
	log.Info().Msg("Starting the Dynamic Engine...")
	defer os.RemoveAll(TempDir)

	var tasksGoroutines = make(map[string]chan data.UserTask)
	var needUpdateData chan bool
	var statsChannel chan stats.Statistic // Channel between the WebUI and Stats loop.
	var dataChannel chan data.UserData

	log.Info().Msg("Reading the user data for first time...")
	userData, err := data.ReadData()
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Error when trying to read the user data file")
	}

	log.Info().Msg("Creating channels for tasks...")
	for _, task := range userData.Tasks {
		// Create the channel for each task (with active state).
		if task.TaskInfo.State == data.StateTaskActive {
			tasksGoroutines[task.TaskInfo.ID] = make(chan data.UserTask)
			go runTaskLoop(task.TaskInfo.ID, tasksGoroutines[task.TaskInfo.ID])
		}
	}
	log.Info().Msg("Channels created correctly")

	// Start the watchdog for the data file.
	log.Info().Msg("Running the watchdog for the data file...")
	go checkForAnUpdate(needUpdateData)

	// Start the WebUI server.
	log.Info().Msg("Starting the WebUI server...")
	go backend.Run(statsChannel)

	// Start the stats recollection.
	log.Info().Msg("Starting the stats loop...")
	go stats.StartLoop(statsChannel, dataChannel)

	configs.CurrentConfigs.RLock()
	tickDuration := time.Millisecond * time.Duration(configs.CurrentConfigs.Behavior.LoopSleep)
	configs.CurrentConfigs.RUnlock()

	// Keep the data updated
	for range time.Tick(tickDuration) {
		select {
		case <-needUpdateData:
			{
				log.Info().
					Msg("Updating the data variable due to a change detected...")
				// Renew the data variable.
				userData, err = data.ReadData()
				if err != nil {
					log.Fatal().
						Err(err).
						Msg("Error when trying to update the data on the engine")
				}

				log.Info().Msg("Data variable updated successfully")
			}
		default:
			// Keep using the current data.
		}

		select {
		case dataChannel <- *userData:
			// Send the data to the stats loop.
		default:
			// If casually the loop is not awaiting for it, continue the loop for prevention
			// of blocking and delay.
		}

		for _, task := range userData.Tasks {
			if task.TaskInfo.State != data.StateTaskActive {
				// Skip the task
				continue
			}
			taskID := task.TaskInfo.ID
			// Check if the task loop and channel have already been initialized
			if _, alreadyExists := tasksGoroutines[taskID]; !alreadyExists {
				// Initialize the channel
				tasksGoroutines[taskID] = make(chan data.UserTask)
				// Start the loop
				go runTaskLoop(taskID, tasksGoroutines[taskID])
			}
			// Send the data to the task's channel
			tasksGoroutines[taskID] <- task
		}
	}
}
