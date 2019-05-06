package engine

import (
	"os"
	"path/filepath"
	"time"

	"github.com/Pegasus8/piworker/utilities/log"

	"github.com/Pegasus8/piworker/processment/data"
	actionsList "github.com/Pegasus8/piworker/processment/elements/actions"
	triggersList "github.com/Pegasus8/piworker/processment/elements/triggers"
	"github.com/Pegasus8/piworker/processment/stats"
	"github.com/Pegasus8/piworker/webui"
)

// StartEngine is the function used to start the Dynamic Engine
func StartEngine() {
	log.Infoln("Dynamic Engine started")

	var triggerGoroutines map[string]chan []data.UserTask
	var needUpdateData chan bool
	var statsChannel chan stats.Statistic // Channel between the WebUI and Stats loop
	var dataChannel chan data.UserData

	log.Infoln("Creating channels of the triggers...")
	for _, trigger := range triggersList.TRIGGERS {
		// Create the channel for each task
		triggerGoroutines[trigger.ID] = make(chan []data.UserTask)
		// Start the trigger goroutine
		go runTriggerLoop(trigger, triggerGoroutines[trigger.ID])
	}
	log.Infoln("Channels created correctly")

	log.Infoln("Reading user data for first time...")
	// Read the data for first time
	userData, err := data.ReadData()
	if err != nil {
		log.Fatalln(err)
	}

	// Start the watchdog of the data file
	log.Infoln("Running the data file watchdog...")
	go checkForAnUpdate(needUpdateData)

	// Start the WebUI server
	log.Infoln("Starting the WebUI server...")
	go webui.Run(statsChannel)

	// Start the stats recollection
	log.Infoln("Starting the stats loop...")
	go stats.StartLoop(statsChannel, dataChannel)

	// Keep the data updated
	for range time.Tick(time.Millisecond * 200) {
		select {
		case <-needUpdateData:
			{
				log.Infoln("Updating the data variable due to a change detected...")
				// Renew the data variable
				userData, err = data.ReadData()
				if err != nil {
					log.Fatalln(err)
				} else {
					log.Infoln("Data variable updated successfully")
				}
			}
		default:
			// Keep using the current data
		}

		select {
		case dataChannel <- *userData:
			// Send the data to the stats loop.
		default:
			// If casually the loop is not awaiting for it, continue the loop for prevention
			// of blocking and delay.
		}

		// Discriminate data for each trigger
		discriminedData := make(map[string][]data.UserTask)
		for _, task := range userData.Tasks {
			if task.TaskInfo.State != data.StateTaskActive {
				// Skip the task
				continue
			}
			userTriggerID := task.TaskInfo.Trigger.ID
			discriminedData[userTriggerID] = append(discriminedData[userTriggerID], task)
		}

		// Send the discrimined data to each channel
		for key, value := range discriminedData {
			triggerGoroutines[key] <- value
		}
	}
}

func checkForAnUpdate(updateChannel chan bool) {
	dataPath := filepath.Join(data.DataPath, data.Filename)
	var oldModTime time.Time
	var newModTime time.Time
	for range time.Tick(time.Millisecond * 300) {
		fileInfo, err := os.Stat(dataPath)
		if err != nil {
			log.Criticalln(err)
		}
		// First run
		if oldModTime.IsZero() {
			log.Infoln("First run of the data file watchdog, setting variable of comparison")
			oldModTime = fileInfo.ModTime()
		}
		newModTime = fileInfo.ModTime()
		if oldModTime != newModTime {
			log.Infoln("Change detected on the data file, sending the signal...")
			// Send the signal
			updateChannel <- true
			// Update the variable
			oldModTime = newModTime
		}
	}
}

func runTriggerLoop(trigger triggersList.Trigger, dataChannel chan []data.UserTask) {
	log.Infof("Loop for the trigger '%s' started\n", trigger.Name)
	for range time.Tick(time.Millisecond * 200) {
		// Receive the renewed data for the trigger in question, if there is not data
		// just keep waiting for it.
		dataReceived := <-dataChannel
		// Iterate over every task correspondent to the trigger
		for _, task := range dataReceived {
			// User args to run the trigger
			triggerArgs := &task.TaskInfo.Trigger.Args
			// Run the trigger with the args provided by the user
			result, err := trigger.Run(triggerArgs)
			if err != nil {
				log.Errorln(err)
			}
			// If the trigger is activated, then run the actions
			if result {
				log.Infof("Trigger '%s' of the task '%s' activated, running actions...\n",
					trigger.Name, task.TaskInfo.Name)
				go runTaskActions(&task)
			}
		}
	}
}

func runTaskActions(task *data.UserTask) {
	log.Infof("Running actions of the task '%s'\n", task.TaskInfo.Name)
	startTime := time.Now()

	userActions := &task.TaskInfo.Actions
	previousState := task.TaskInfo.State

	log.Infof("Changing task state of '%s' to '%s'\n", task.TaskInfo.Name, data.StateTaskOnExecution)
	// Set task state to on-execution
	err := data.UpdateTaskState(task.TaskInfo.Name, data.StateTaskOnExecution)
	if err != nil {
		log.Fatalf("Error when trying to update the task state of '%s' to '%s'\n",
			task.TaskInfo.Name, data.StateTaskOnExecution)
	}

	orderN := 0
	for range *userActions {

		for _, userAction := range *userActions {
			if userAction.Order == orderN {

				// Run the action
				for _, action := range actionsList.ACTIONS {
					if userAction.ID == action.ID {
						result, err := action.Run(&userAction.Args)
						if err != nil {
							log.Errorln(err)
						}
						if result {
							log.Infof("Action in order %d of the task '%s' finished correctly",
								userAction.Order, task.TaskInfo.Name)
						} else {
							log.Errorf("Action in order %d of the task '%s' wasn't executed correctly",
								userAction.Order, task.TaskInfo.Name)
						}

						// It's not necessary to continue iterating
						break
					}
				}

				orderN++
				break
			}
		}

	}

	// Needed read the actual task state
	updatedData, err := data.ReadData()
	if err != nil {
		log.Fatalln(err)
	}
	updatedTask, _, err := updatedData.GetTaskByName(task.TaskInfo.Name)
	if err != nil {
		log.Fatalln(err)
	}
	lastState := updatedTask.TaskInfo.State
	// If the state has no changes, return to the original state
	if lastState == data.StateTaskOnExecution {
		err = data.UpdateTaskState(task.TaskInfo.Name, previousState)
		if err != nil {
			log.Fatalln(err)
		}
	}
	executionTime := time.Since(startTime).String()
	log.Infof("Task with name '%s' executed in %s\n", task.TaskInfo.Name, executionTime)
}
