package engine

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/Pegasus8/piworker/core/data"
	actionsModel "github.com/Pegasus8/piworker/core/elements/actions"
	actionsList "github.com/Pegasus8/piworker/core/elements/actions/models"
	triggersList "github.com/Pegasus8/piworker/core/elements/triggers/models"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/core/uservariables"
	"github.com/rs/zerolog/log"
)

func runTaskLoop(taskID string, taskChannel chan data.UserTask, managementChannel chan uint8) {
	log.Info().Str("taskID", taskID).Msg("Loop started")
	// Receive the task for first time.
	taskReceived := <-taskChannel

	for {
		select {
		// Update the data.
		case taskReceived = <-taskChannel:
		// Stop signal received.
		case code := <- managementChannel: {
			switch code {
			// Stopped by the system.
			case 0: {
				log.Info().
					Str("taskID", taskReceived.ID).
					Msg("Task stopped by the system")
				return
			}
			// Stopped by the user.
			case 1: {
				log.Info().
					Str("taskID", taskReceived.ID).
					Msg("Task stopped by the user due to a state change")
				return
			}
			// Task deleted by the user.
			case 2: {
				log.Info().
					Str("taskID", taskReceived.ID).
					Msg("Task deleted by the user")
				return
			}
			}
		}

		default:
			// Keep using the same data unless a new event (related with
			// the task running the loop) is received on the engine.
		}

		triggered, err := runTrigger(taskReceived.Trigger, taskReceived.ID)
		if err != nil {
			log.Error().
				Err(err).
				Str("taskID", taskReceived.ID).
				Msg("Error while trying to run the trigger of the task, stopping the task execution...")
			break
		}

		if triggered {
			if wasRecentlyExecuted(taskReceived.ID) {
				log.Debug().
					Str("taskID", taskReceived.ID).
					Msg("The task was recently executed, the trigger stills active. Skipping it...")

				goto skipTaskExecution
			}

			log.Info().
				Str("taskID", taskReceived.ID).
				Str("triggerID", taskReceived.Trigger.ID).
				Msg("[%s] Trigger with the ID '%s' activated, running actions...")

			err = runActions(&taskReceived)
			if err != nil {
				log.Error().
					Str("taskID", taskReceived.ID).
					Err(err).
					Msg("Error when running the actions of the task")
				break
			}

			err = setAsRecentlyExecuted(taskReceived.ID)
			if err != nil {
				log.Error().
					Err(err).
					Str("taskID", taskReceived.ID).
					Msg("Error when trying to set a task as recently executed")
				break
			}

		skipTaskExecution:
			// Skip the execution of the task but not skip the entire iteration
			// in case of have to do something else with the task.
		} else {
			if wasRecentlyExecuted(taskReceived.ID) {
				err = setAsReadyToExecuteAgain(taskReceived.ID)
				if err != nil {
					log.Error().
						Err(err).
						Str("taskID", taskReceived.ID).
						Msg("Error when trying to set a task as ready to execute again")
					break
				}
			}
		}
	}

	// If the loop breaks (by a 'break' statement), there was a failure, so an event of type `Failed` must
	// be emitted.
	event := data.Event{
		Type: data.Failed,
		TaskID: taskReceived.ID,
	}
	data.EventBus <- event
}

func runTrigger(trigger data.UserTrigger, parentTaskID string) (bool, error) {
	for _, pwTrigger := range triggersList.TRIGGERS {
		if trigger.ID == pwTrigger.ID {
			for _, arg := range trigger.Args {
				// Check if the arg contains a user global variable
				err := searchAndReplaceVariable(&arg, parentTaskID)
				if err != nil {
					return false, err
				}
			}
			result, err := pwTrigger.Run(&trigger.Args, parentTaskID)
			if err != nil {
				return false, err
			}
			if result {
				return true, nil
			}
			return false, nil
		}
	}

	return false, fmt.Errorf("The trigger with the ID '%s' cannot be found", trigger.ID)
}

func runActions(task *data.UserTask) error {
	log.Info().Str("taskID", task.ID).Msg("Running actions...")
	startTime := time.Now()

	userActions := &task.Actions

	log.Printf("[%s] Changing task state to '%s'\n", task.ID, data.StateTaskOnExecution)
	// Set task state to on-execution
	err := data.UpdateTaskState(task.ID, data.StateTaskOnExecution)
	if err != nil {
		log.Error().
			Str("taskID", task.ID).
			Msgf("Error when trying to update the task state to '%s'\n", data.StateTaskOnExecution)
		return err
	}

	var chainedResult *actionsModel.ChainedResult
	var orderN uint8 = 0
	for range *userActions {

		for _, userAction := range *userActions {
			if userAction.Order == orderN {

				// Run the action
				for _, action := range actionsList.ACTIONS {
					if userAction.ID == action.ID {
						log.Info().
							Str("taskID", task.ID).
							Str("actionID", userAction.ID).
							Bool("chained", userAction.Chained).
							Uint8("actionOrder", userAction.Order).
							Str("previousResultType", string(chainedResult.ResultType)).
							Str("previousResultContent", chainedResult.Result).
							Msg("Running action")

						for _, arg := range userAction.Args {
							err := searchAndReplaceVariable(&arg, task.ID)
							if err != nil {
								log.Error().
									Str("taskID", task.ID).
									Str("actionID", userAction.ID).
									Str("argID", arg.ID).
									Err(err).
									Uint8("actionOrder", orderN).
									Msg("Error when searching for a variable on the argument")
								return err
							}
						}

						ua, err := replaceArgByCR(chainedResult, &userAction)
						if err != nil {
							log.Error().
								Str("taskID", task.ID).
								Str("actionID", userAction.ID).
								Err(err).
								Uint8("actionOrder", userAction.Order).
								Msg("Error when trying to replace an argument for a variable")
							return err
						}
						userAction = *ua

						result, chr, err := action.Run(chainedResult, &userAction, task.ID)
						// Set the returned chr (chained result) to our main instance of the ChainedResult struct (`chainedResult`).
						// This will be given to the next action (if exists).
						chainedResult = chr
						if err != nil {
							log.Error().
								Str("taskID", task.ID).
								Str("actionID", userAction.ID).
								Err(err).
								Uint8("actionOrder", userAction.Order).
								Msg("Error when running the action")
							return err
						}
						if result {
							log.Info().
								Str("taskID", task.ID).
								Str("actionID", userAction.ID).
								Uint8("actionOrder", userAction.Order).
								Msg("Action finished correctly")
						} else {
							log.Warn().
								Str("taskID", task.ID).
								Str("actionID", userAction.ID).
								Uint8("actionOrder", userAction.Order).
								Msg("Action wasn't executed correctly. Aborting task for prevention of future errors...")
							return fmt.Errorf("Action returned an unsuccessful result")
						}

						// No need to keep iterating
						break
					}
				}

				orderN++
				break
			}
		}

	}

	// Before the begin of actions's execution, the only possible state of the task is 'active' (otherwise the task won't be here).
	// So now, after the execution of all the actions, let's restore the state to its previous value (remember that while the task is
	// being executed the state will be 'on-execution').
	err = data.UpdateTaskState(task.ID, data.StateTaskActive)
	if err != nil {
		log.Fatal().
			Str("taskID", task.ID).
			Str("previousState", string(data.StateTaskOnExecution)).
			Err(err).
			Msg("Error when trying to update the task's state")
	}

	executionTime := time.Since(startTime).String()
	log.Info().
		Str("taskID", task.ID).
		Str("executionTime", executionTime).
		Msg("Actions executed")

	return nil
}

func setAsRecentlyExecuted(ID string) error {
	dir, err := ioutil.TempDir(TempDir, "")
	if err != nil {
		return err
	}

	file, err := ioutil.TempFile(filepath.Join(dir, ID), "")
	if err != nil {
		return err
	}
	file.Close()

	return nil
}

func wasRecentlyExecuted(ID string) bool {
	_, err := os.Stat(filepath.Join(TempDir, ID))
	if err != nil {
		if os.IsNotExist(err) {
			return false
		} else if os.IsExist(err) {
			return true
		}
		log.Fatal().Err(err).Str("taskID", ID).Msg("Error when trying to get the the execution file's info")
	}

	return true
}

func setAsReadyToExecuteAgain(ID string) error {
	path := filepath.Join(TempDir, ID)
	err := os.Remove(path)
	if err != nil {
		return err
	}

	return nil
}

func searchAndReplaceVariable(arg *data.UserArg, parentTaskID string) error {
	// Check if the arg contains a user global variable
	if uservariables.ContainGlobalVariable(&arg.Content) {
		// If yes, then get the name of the variable by using regex
		varName := uservariables.GetGlobalVariableName(arg.Content)

		// Get the variable from the name
		globalVar, err := uservariables.GetGlobalVariable(varName)
		if err != nil {
			log.Error().Err(err).Str("taskID", parentTaskID).Str("varName", varName).Msg("Error when trying to read the user global variable")
			return err
		}

		// If all it's ok, replace the content of the argument (which is the variable name basically)
		// with the content of the desired user global variable.
		globalVar.RLock()
		arg.Content = globalVar.Content
		globalVar.RUnlock()

		// If the arg not contains a user global variable, then check if contains a user local variable instead.
	} else if uservariables.ContainLocalVariable(&arg.Content) {
		// If yes, then get the name of the variable by using regex
		varName := uservariables.GetLocalVariableName(arg.Content)

		// Get the variable from the name
		localVariable, err := uservariables.GetLocalVariable(varName, parentTaskID)
		if err != nil {
			log.Error().Err(err).Str("taskID", parentTaskID).Str("varName", varName).Msg("Error when trying to read the user local variable")
			return err
		}

		// If all it's ok, replace the content of the argument (which is the variable name basically)
		// with the content of the desired user local variable.
		localVariable.RLock()
		arg.Content = localVariable.Content
		localVariable.RUnlock()
	}

	return nil
}

func replaceArgByCR(chainedResult *actionsModel.ChainedResult, userAction *data.UserAction) (*data.UserAction, error) {
	if userAction.Order == 0 {
		// Prevent the usage of ChainedResult because there are no previous actions.
		userAction.Chained = false
	}
	if userAction.Chained {
		if chainedResult.Result == "" {
			return nil, actionsList.ErrEmptyChainedResult
		}

		for _, userArg := range userAction.Args {
			if userArg.ID == userAction.ArgumentToReplaceByCR {
				userArgType, err := getUserArgType(userAction.ID, userArg.ID)
				if err != nil {
					return nil, err
				}

				if chainedResult.ResultType != userArgType && userArgType != types.Any {
					return nil, fmt.Errorf("Can't replace the arg with the ID '%s' of type '%s' with the previous ChainedResult of type '%s'", userArg.ID, userArgType, chainedResult.ResultType)

				}

				// If all is ok, replace the content
				userArg.Content = chainedResult.Result
			}
		}
	}

	return userAction, nil
}

func getUserArgType(userActionID string, userArgID string) (types.PWType, error) {
	var actionFound bool

	for _, action := range actionsList.ACTIONS {
		if action.ID == userActionID {
			actionFound = true

			for _, arg := range action.Args {
				if arg.ID == userArgID {
					return arg.ContentType, nil
				}
			}
		}
	}

	var err error
	if actionFound {
		err = fmt.Errorf("Unrecognized argument ID '%s' of the action '%s'", userArgID, userActionID)
	} else {
		err = fmt.Errorf("Unrecognized action ID '%s'", userActionID)
	}

	return types.Any, err
}
