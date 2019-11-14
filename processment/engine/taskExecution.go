package engine

import (
	"errors"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/processment/data"
	actionsModel "github.com/Pegasus8/piworker/processment/elements/actions"
	actionsList "github.com/Pegasus8/piworker/processment/elements/actions/models"
	triggersList "github.com/Pegasus8/piworker/processment/elements/triggers/models"
	"github.com/Pegasus8/piworker/processment/uservariables"
)

func runTaskLoop(taskname string, taskChannel chan data.UserTask) {
	log.Printf("[%s] Loop started\n", taskname)
	for {
		// Receive the renewed data for the task in question, if there is not data
		// just keep waiting for it.
		taskReceived := <-taskChannel

		triggered, err := runTrigger(taskReceived.TaskInfo.Trigger, taskReceived.TaskInfo.Name)
		if err != nil {
			log.Fatalf("[%s] Error while trying to run the trigger of the task, stopping the task execution...\n",
				taskReceived.TaskInfo.Name)
		}
		if triggered {
			if wasRecentlyExecuted(taskReceived.TaskInfo.Name) {
				log.Printf("[%s] The task was recently executed, the trigger "+
					"stills active. Skipping it...\n", taskReceived.TaskInfo.Name)
				goto skipTaskExecution
			}

			log.Printf("[%s] Trigger with the ID '%s' activated, running actions...\n",
				taskReceived.TaskInfo.Name, taskReceived.TaskInfo.Trigger.ID)
			runActions(&taskReceived)

			err = setAsRecentlyExecuted(taskReceived.TaskInfo.Name)
			if err != nil {
				log.Printf("[%s] %s\n", taskReceived.TaskInfo.Name, err.Error())
			}

		skipTaskExecution:
			// Skip the execution of the task but not skip the entire iteration
			// in case of have to do something else with the task.
		} else {
			if wasRecentlyExecuted(taskReceived.TaskInfo.Name) {
				err = setAsReadyToExecuteAgain(taskReceived.TaskInfo.Name)
				if err != nil {
					log.Printf("[%s] %s\n", taskReceived.TaskInfo.Name, err.Error())
				}
			}
		}
	}
}

func runTrigger(trigger data.UserTrigger, parentTaskName string) (bool, error) {
	for _, pwTrigger := range triggersList.TRIGGERS {
		if trigger.ID == pwTrigger.ID {
			for _, arg := range trigger.Args {
				// Check if the arg contains a user global variable
				err := searchAndReplaceVariable(&arg, parentTaskName)
				if err != nil {
					return false, err
				}
			}
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

	log.Printf("The trigger with the ID '%s' cannot be found\n", trigger.ID)
	return false, errors.New("Trigger not found")
}

func runActions(task *data.UserTask) {
	log.Printf("[%s] Running actions...\n", task.TaskInfo.Name)
	startTime := time.Now()

	userActions := &task.TaskInfo.Actions
	previousState := task.TaskInfo.State

	log.Printf("[%s] Changing task state to '%s'\n", task.TaskInfo.Name, data.StateTaskOnExecution)
	// Set task state to on-execution
	err := data.UpdateTaskState(task.TaskInfo.Name, data.StateTaskOnExecution)
	if err != nil {
		log.Fatalf("[%s] Error when trying to update the task state to '%s'\n",
			task.TaskInfo.Name, data.StateTaskOnExecution)
	}

	var chainedResult *actionsModel.ChainedResult
	orderN := 0
	for range *userActions {

		for _, userAction := range *userActions {
			if userAction.Order == orderN {

				// Run the action
				for _, action := range actionsList.ACTIONS {
					if userAction.ID == action.ID {
						log.Printf("[%s] Running action n%d. Chained: %t | Previous chained result: %+v\n", task.TaskInfo.Name, orderN, userAction.Chained, chainedResult)
						if !userAction.Chained {
							// Overwrite previous result to prevent being used.
							chainedResult = &actionsModel.ChainedResult{}
						}
						for _, arg := range userAction.Args {
							err := searchAndReplaceVariable(&arg, task.TaskInfo.Name)
							if err != nil {
								log.Printf("[%s] %s\n", task.TaskInfo.Name, err.Error())
								return
							}
						}
						result, chr, err := action.Run(chainedResult, &userAction)
						// Set the returned chr (chained result) to our main instance of the ChainedResult struct (`chainedResult`).
						// This will be given to the next action (if exists).
						chainedResult = chr
						if err != nil {
							log.Printf("[%s] %s\n", task.TaskInfo.Name, err.Error())
						}
						if result {
							log.Printf("[%s] Action in order %d finished correctly\n",
								task.TaskInfo.Name, userAction.Order)
						} else {
							log.Printf("[%s] Action in order %d wasn't executed correctly\n",
								task.TaskInfo.Name, userAction.Order)
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
	log.Printf("[%s] Actions executed in %s\n", task.TaskInfo.Name, executionTime)
}

func checkForAnUpdate(updateChannel chan bool) {
	dataPath := filepath.Join(data.DataPath, data.Filename)
	var oldModTime time.Time
	var newModTime time.Time
	for range time.Tick(time.Millisecond * 300) {
		fileInfo, err := os.Stat(dataPath)
		if err != nil {
			log.Println(err)
		}
		// First run
		if oldModTime.IsZero() {
			log.Println("First run of the data file watchdog, setting variable of comparison")
			oldModTime = fileInfo.ModTime()
		}
		newModTime = fileInfo.ModTime()
		if oldModTime != newModTime {
			log.Println("Change detected on the data file, sending the signal...")
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
		log.Printf("[%s] %s\n", taskName, err.Error())
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

func searchAndReplaceVariable(arg *data.UserArg, parentTaskName string) error {
	// Check if the arg contains a user global variable
	if uservariables.ContainGlobalVariable(&arg.Content) {
		// If yes, then get the name of the variable by using regex
		varName := uservariables.GetGlobalVariableName(arg.Content)
		// Get the variable from the name
		globalVar, err := uservariables.GetGlobalVariable(varName)
		if err != nil {
			log.Printf("[%s] Error when trying to read the user global variable '%s': %s\n", parentTaskName, varName, err.Error())
			return err
		}
		globalVar.RLock()
		// If all it's ok, replace the content of the argument (wich is the variable name basically)
		// with the content of the desired user global variable.
		arg.Content = globalVar.Content
		globalVar.RUnlock()
		// If the arg not contains a user global variable, then check if contains a user local variable instead.
	} else if uservariables.ContainLocalVariable(&arg.Content) {
		// If yes, then get the name of the variable by using regex
		varName := uservariables.GetLocalVariableName(arg.Content)
		// Get the variable from the name
		localVariable, err := uservariables.GetLocalVariable(varName, parentTaskName)
		if err != nil {
			log.Printf("[%s] Error when trying to read the user local variable '%s': %s\n", parentTaskName, varName, err.Error())
			return err
		}
		localVariable.RLock()
		// If all it's ok, replace the content of the argument (wich is the variable name basically)
		// with the content of the desired user local variable.
		arg.Content = localVariable.Content
		localVariable.RUnlock()
	}

	return nil
}
