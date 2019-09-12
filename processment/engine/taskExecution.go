package engine

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/utilities/log"

	"github.com/Pegasus8/piworker/processment/data"
	actionsList "github.com/Pegasus8/piworker/processment/elements/actions"
	triggersList "github.com/Pegasus8/piworker/processment/elements/triggers"
)

func runTaskLoop(taskname string, taskChannel chan data.UserTask) {
	log.Infof("[%s] Loop started\n", taskname)
	for range time.Tick(time.Millisecond * 400) {
		// Receive the renewed data for the task in question, if there is not data
		// just keep waiting for it.
		taskReceived := <-taskChannel

		triggered, err := runTrigger(taskReceived.TaskInfo.Trigger)
		if err != nil {
			log.Fatalf("[%s] Error while trying to run the trigger of the task, stopping the task execution...\n",
				taskReceived.TaskInfo.Name)
		}
		if triggered {
			if wasRecentlyExecuted(taskReceived.TaskInfo.Name) {
				log.Infof("[%s] The task was recently executed, the trigger "+
					"stills active. Skipping it...\n", taskReceived.TaskInfo.Name)
				goto skipTaskExecution
			}

			log.Infof("[%s] Trigger with the ID '%s' activated, running actions...\n",
				taskReceived.TaskInfo.Name, taskReceived.TaskInfo.Trigger.ID)
			runTaskActions(&taskReceived)

			err = setAsRecentlyExecuted(taskReceived.TaskInfo.Name)
			if err != nil {
				log.Criticalln(err)
			}

		skipTaskExecution:
			// Skip the execution of the task but not skip the entire iteration
			// in case of have to do something else with the task.
		} else {
			if wasRecentlyExecuted(taskReceived.TaskInfo.Name) {
				err = setAsReadyToExecuteAgain(taskReceived.TaskInfo.Name)
				if err != nil {
					log.Criticalln(err)
				}
			}
		}
	}
}

func runTrigger(trigger data.UserTrigger) (bool, error) {
	for _, pwTrigger := range triggersList.TRIGGERS {
		if trigger.ID == pwTrigger.ID {
			result, err := pwTrigger.Run(&trigger.Args)
			if err != nil {
				return false, err
			}
			if result {
				return true, nil
			}
			return false, nil
		}
	}

	log.Errorf("The trigger with the ID '%s' cannot be found\n", trigger.ID)
	return false, nil
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

func setAsRecentlyExecuted(taskName string) error {
	taskName = strings.ReplaceAll(taskName, " ", "_")
	dir, err := ioutil.TempDir(TempDir, "")
	if err != nil {
		return err
	}

	file, err := ioutil.TempFile(filepath.Join(dir, taskName), "")
	if err != nil {
		return err
	}
	defer file.Close()

	return nil
}

func wasRecentlyExecuted(taskName string) bool {
	taskName = strings.ReplaceAll(taskName, " ", "_")
	_, err := os.Stat(filepath.Join(TempDir, taskName))
	if err != nil {
		if os.IsNotExist(err) {
			return false
		} else if os.IsExist(err) {
			return true
		}
		log.Criticalln(err)
		return false
	}

	return true
}

func setAsReadyToExecuteAgain(taskName string) error {
	taskName = strings.ReplaceAll(taskName, " ", "_")
	path := filepath.Join(TempDir, taskName)
	err := os.Remove(path)
	if err != nil {
		return err
	}

	return nil
}
