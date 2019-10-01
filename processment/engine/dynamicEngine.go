package engine

import (
	"os"
	"time"

	"github.com/Pegasus8/piworker/utilities/log"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/stats"
	"github.com/Pegasus8/piworker/processment/configs"
	"github.com/Pegasus8/piworker/webui"
)

// StartEngine is the function used to start the Dynamic Engine
func StartEngine() {
 	log.Infoln("Starting the Dynamic Engine...")
	defer os.RemoveAll(TempDir)

	var tasksGoroutines map[string]chan data.UserTask
	var needUpdateData chan bool
	var statsChannel chan stats.Statistic // Channel between the WebUI and Stats loop.
	var dataChannel chan data.UserData

	log.Infoln("Reading the user data for first time...")
	userData, err := data.ReadData()
	if err != nil {
		log.Fatalln(err)
	}

	log.Infoln("Creating channels for tasks ...")
	for _, task := range userData.Tasks {
		// Create the channel for each task (with active state).
		if task.TaskInfo.State == data.StateTaskActive {
			tasksGoroutines[task.TaskInfo.Name] = make(chan data.UserTask)
			go runTaskLoop(task.TaskInfo.Name, tasksGoroutines[task.TaskInfo.Name])
		}
	}
	log.Infoln("Channels created correctly")

	// Start the watchdog for the data file.
	log.Infoln("Running the watchdog for the data file...")
	go checkForAnUpdate(needUpdateData)

	// Start the WebUI server.
	log.Infoln("Starting the WebUI server...")
	go webui.Run(statsChannel)

	// Start the stats recollection.
	log.Infoln("Starting the stats loop...")
	go stats.StartLoop(statsChannel, dataChannel)

	// Keep the data updated
	for range time.Tick(time.Millisecond * time.Duration(configs.CurrentConfigs.Behavior.LoopSleep)) {
		select {
		case <-needUpdateData:
			{
				log.Infoln("Updating the data variable due to a change detected...")
				// Renew the data variable.
				userData, err = data.ReadData()
				if err != nil {
					log.Fatalln(err)
				} else {
					log.Infoln("Data variable updated successfully")
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
			taskName := task.TaskInfo.Name
			// Check if the task loop and channel have already been initialized
			if _, alreadyExists := tasksGoroutines[taskName]; !alreadyExists {
				// Initialize the channel
				tasksGoroutines[taskName] = make(chan data.UserTask)
				// Start the loop
				go runTaskLoop(taskName, tasksGoroutines[taskName])
			}
			// Send the data to the task's channel
			tasksGoroutines[taskName] <- task
		}
	}
}
