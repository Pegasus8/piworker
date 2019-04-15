package core

import (
	"time"

	"github.com/Pegasus8/piworker/utilities/log"

	actionsList "github.com/Pegasus8/piworker/processment/elements/actions"
	triggersList "github.com/Pegasus8/piworker/processment/elements/triggers"
	"github.com/Pegasus8/piworker/processment/data"
)

var triggers = &triggersList.TRIGGERS
var actions = &actionsList.ACTIONS

// RunEngine is the function used to start the main loop of PiWorker (alias "engine")
func RunEngine() {
	startTime := time.Now()
	
	// Main loop
	for range time.Tick(time.Millisecond * 200) {
		userData, err := data.ReadData()
		if err != nil {
			log.Fatalln(err)
		}
		userTasks := &userData.Tasks

		// Execute the trigger
		for _, userTask := range *userTasks {

			for _, pwtrigger := range *triggers {

				if userTask.TaskInfo.Trigger.ID == pwtrigger.ID {
					result := runTrigger(&pwtrigger, &userTask)
					// If trigger returns true, then run the action
					if result {
						log.Infof(
							"Executing actions of the task '%s' at %s from the start of PiWorker",
							userTask.TaskInfo.Name, time.Since(startTime).String(),
						)
						go runActions(actions, &userTask)
					}
				}

			}

		}

	}

}

func runTrigger(pwtrigger *triggersList.Trigger, userTask *data.UserTask) bool {
	result, err := pwtrigger.Run(&userTask.TaskInfo.Trigger.Args)
	if err != nil {
		log.Errorf("Error when trying run the trigger '%s': %s - User task: '%s'\n",
			pwtrigger.Name, err, userTask.TaskInfo.Name)
	}

	return result
}

func runActions(pwactions *[]actionsList.Action, userTask *data.UserTask) {
	userActions := &userTask.TaskInfo.Actions

	orderN := 0
	for range *userActions {

		for _, userAction := range *userActions {
			if userAction.Order == orderN {
					
				// Run the action
				for _, action := range *actions {
					if userAction.ID == action.ID {
						result, err := action.Run(&userAction.Args)
						if err != nil {
							log.Errorln(err)
						}
						if result {
							log.Infof("Action in order %d of the task '%s' finished correctly", 
								userAction.Order, userTask.TaskInfo.Name)
						} else {
							log.Errorf("Action in order %d of the task '%s' wasn't executed correctly",
								userAction.Order, userTask.TaskInfo.Name)
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
}