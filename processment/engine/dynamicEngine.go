package engine

import (
	"os"
	"time"
	"log"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/stats"
	"github.com/Pegasus8/piworker/processment/configs"
	"github.com/Pegasus8/piworker/webui/backend"
)

// StartEngine is the function used to start the Dynamic Engine
func StartEngine() {
 	log.Println("Starting the Dynamic Engine...")
	defer os.RemoveAll(TempDir)

	var tasksGoroutines = make(map[string]chan data.UserTask)
	var needUpdateData chan bool
	var statsChannel chan stats.Statistic // Channel between the WebUI and Stats loop.
	var dataChannel chan data.UserData

	log.Println("Reading the user data for first time...")
	userData, err := data.ReadData()
	if err != nil {
		log.Fatalln(err)
	}

	log.Println("Creating channels for tasks ...")
	for _, task := range userData.Tasks {
		// Create the channel for each task (with active state).
		if task.TaskInfo.State == data.StateTaskActive {
			tasksGoroutines[task.TaskInfo.ID] = make(chan data.UserTask)
			go runTaskLoop(task.TaskInfo.ID, tasksGoroutines[task.TaskInfo.ID])
		}
	}
	log.Println("Channels created correctly")

	// Start the watchdog for the data file.
	log.Println("Running the watchdog for the data file...")
	go checkForAnUpdate(needUpdateData)

	// Start the WebUI server.
	log.Println("Starting the WebUI server...")
	go backend.Run(statsChannel)

	// Start the stats recollection.
	log.Println("Starting the stats loop...")
	go stats.StartLoop(statsChannel, dataChannel)

	configs.CurrentConfigs.RLock()
	tickDuration := time.Millisecond * time.Duration(configs.CurrentConfigs.Behavior.LoopSleep)
	configs.CurrentConfigs.RUnlock()
	
	// Keep the data updated
	for range time.Tick(tickDuration) {
		select {
		case <-needUpdateData:
			{
				log.Println("Updating the data variable due to a change detected...")
				// Renew the data variable.
				userData, err = data.ReadData()
				if err != nil {
					log.Fatalln(err)
				} else {
					log.Println("Data variable updated successfully")
				}
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
