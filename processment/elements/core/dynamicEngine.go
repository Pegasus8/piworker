package core

import (
	"time"

	"github.com/Pegasus8/piworker/utilities/log"

	"github.com/Pegasus8/piworker/processment/data"
	triggersList "github.com/Pegasus8/piworker/processment/elements/triggers"
	actionsList "github.com/Pegasus8/piworker/processment/elements/actions"
)

func StartEngine() {

	var triggerGoroutines map[string]chan []data.UserTask

	for _, trigger := range triggersList.TRIGGERS {
		// Create the channel for each task
		triggerGoroutines[trigger.ID] = make(chan []data.UserTask)
		// Start the trigger goroutine
		go runTriggerLoop(trigger, triggerGoroutines[trigger.ID])
	}

	// Keep the data updated
	for range time.Tick(time.Millisecond * 200) {
		userData, err := data.ReadData()
		if err != nil {
			log.Criticalln(err)
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

func runTriggerLoop(trigger triggersList.Trigger, dataChannel chan []data.UserTask) {
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
				go runTaskActions(&task)

			}
		}
	}
}

func runTaskActions(task *data.UserTask) {
	userActions := &task.TaskInfo.Actions
	previousState := task.TaskInfo.State

	// Set task state to on-execution
	data.UpdateTaskState(task.TaskInfo.Name, data.StateTaskOnExecution)

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
	if lastState == data.StateTaskOnExecution{
		data.UpdateTaskState(task.TaskInfo.Name, previousState)
	}
}
