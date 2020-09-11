package engine

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/Pegasus8/piworker/core/engine/queue"

	"github.com/Pegasus8/piworker/core/data"
	actionsList "github.com/Pegasus8/piworker/core/elements/actions/models"
	actionsModel "github.com/Pegasus8/piworker/core/elements/actions/shared"
	triggersList "github.com/Pegasus8/piworker/core/elements/triggers/models"
	"github.com/Pegasus8/piworker/core/stats"
	"github.com/Pegasus8/piworker/core/uservariables"

	"github.com/rs/zerolog/log"
)

func (engine *Engine) runTaskLoop(taskID string, taskChannel chan data.UserTask, managementChannel chan uint8, actionsQueue *queue.Queue) {
	log.Info().Str("taskID", taskID).Msg("Task running, waiting for data...")

	// Receive the task for first time.
	taskReceived := <-taskChannel

	log.Info().Str("taskID", taskID).Msg("Data received, getting tick duration config before start the loop...")

	// Load configs
	d := engine.configs.Behavior.LoopSleep
	ticker := time.NewTicker(time.Millisecond * time.Duration(d))
	defer ticker.Stop()

	log.Info().Str("taskID", taskID).Int64("tickDuration", d).Msg("Tick duration obtained, starting task loop")

	// Hook
	if !engine.OnTaskLoopInit(taskID) {
		return
	}

	for range ticker.C {
		select {
		// Update the data.
		case taskReceived = <-taskChannel:
		// Stop signal received.
		case code := <-managementChannel:
			{
				switch code {
				// Stopped by the system.
				case 0:
					{
						log.Info().
							Str("taskID", taskReceived.ID).
							Msg("Task execution stopped by the system")
						return
					}
				// Stopped by the user.
				case 1:
					{
						log.Info().
							Str("taskID", taskReceived.ID).
							Msg("Task execution stopped by the user due to a state change")
						return
					}
				// Task deleted by the user.
				case 2:
					{
						log.Info().
							Str("taskID", taskReceived.ID).
							Msg("Task deleted by the user, execution stopped")
						return
					}
				}
			}

		default:
			// Keep using the same data unless a new event (related with
			// the task running the loop) is received on the engine.
		}

		var beforeRunActions time.Time
		var actionsExecutionDuration time.Duration

		triggered, err := engine.runTrigger(taskReceived.Trigger, taskReceived.ID)
		if err != nil {
			log.Error().
				Err(err).
				Str("taskID", taskReceived.ID).
				Msg("Error while trying to run the trigger of the task, stopping the task execution...")
			break
		}

		if triggered {
			if wasRecentlyExecuted(taskReceived.ID) {
				goto skipTaskExecution
			}

			// Hook
			if !engine.OnTriggerActivation(taskReceived.ID, &taskReceived.Trigger) {
				return
			}

			log.Info().
				Str("taskID", taskReceived.ID).
				Str("triggerID", taskReceived.Trigger.ID).
				Msg("[%s] Trigger with the ID '%s' activated, running actions...")

			beforeRunActions = time.Now()
			err = engine.runActions(&taskReceived, actionsQueue)
			actionsExecutionDuration = time.Since(beforeRunActions)

			if err != nil {
				log.Error().
					Str("taskID", taskReceived.ID).
					Err(err).
					Msg("Error when running the actions of the task")

				// Hook
				engine.OnTaskExecutionFail(taskReceived.ID, err)

				break
			}

			// Hook
			if !engine.OnTaskExecutionSuccess(taskReceived.ID, actionsExecutionDuration) {
				return
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
		Type:   data.Failed,
		TaskID: taskReceived.ID,
	}
	data.EventBus <- event
	// And finally, update the state of the task on the database.
	err := engine.userdataDB.UpdateTaskState(taskReceived.ID, data.StateTaskFailed)
	if err != nil {
		log.Panic().Err(err).Str("taskID", taskReceived.ID).Msg("Error when trying to update the state of the task to 'failed'")
	}
}

func (engine *Engine) runTrigger(trigger data.UserTrigger, parentTaskID string) (bool, error) {
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

	return false, fmt.Errorf("the trigger with the ID '%s' cannot be found", trigger.ID)
}

func (engine *Engine) runActions(task *data.UserTask, actionsQueue *queue.Queue) error {
	log.Info().Str("taskID", task.ID).Msg("Running actions...")
	startTime := time.Now()

	userActions := &task.Actions

	log.Info().Str("taskID", task.ID).Msgf("Changing task state to '%s'\n", data.StateTaskOnExecution)

	// Set task state to on-execution
	err := engine.userdataDB.UpdateTaskState(task.ID, data.StateTaskOnExecution)
	if err != nil {
		log.Error().
			Str("taskID", task.ID).
			Msgf("Error when trying to update the task state to '%s'\n", data.StateTaskOnExecution)
		return err
	}

	var chainedResult = &actionsModel.ChainedResult{}
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

						beforeActionExecution := time.Now()

						// Send the action execution to the queue
						execResult := actionsQueue.AddJob(task.ID, action, &userAction, *chainedResult)
						r := <-execResult

						actionExecutionDuration := time.Since(beforeActionExecution)

						// Hook
						if !engine.OnActionRun(task.ID, &userAction, actionExecutionDuration) {
							return nil
						}

						// Set the returned chr (chained result) to our main instance of the ChainedResult struct (`chainedResult`).
						// This will be given to the next action (if exists).
						chainedResult = &r.RetournedCR
						if r.Err != nil {
							log.Error().
								Str("taskID", task.ID).
								Str("actionID", userAction.ID).
								Err(r.Err).
								Uint8("actionOrder", userAction.Order).
								Msg("Error when running the action")
							return err
						}
						if r.Successful {
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
							return fmt.Errorf("action returned an unsuccessful result")
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

	// Before the begin of actions' execution, the only possible state of the task is 'active' (otherwise the task won't be here).
	// So now, after the execution of all the actions, let's restore the state to its previous value (remember that while the task is
	// being executed the state will be 'on-execution').
	err = engine.userdataDB.UpdateTaskState(task.ID, data.StateTaskActive)
	if err != nil {
		log.Fatal().
			Str("taskID", task.ID).
			Str("previousState", string(data.StateTaskOnExecution)).
			Err(err).
			Msg("Error when trying to update the task's state")
	}

	executionTime := time.Since(startTime)
	log.Info().
		Str("taskID", task.ID).
		Str("executionTime", executionTime.String()).
		Msg("Actions executed")

	// Add the execution time to the calculation of the field `stats.Current.AverageExecutionTime`.
	stats.Current.Lock()
	stats.Current.TasksStats.NewAvgObs(executionTime) // TODO elaborate a new way to calculate the average.
	stats.Current.Unlock()

	return nil
}

func setAsRecentlyExecuted(ID string) error {
	err := ioutil.WriteFile(filepath.Join(TempDir, ID), []byte{}, 0644)
	if err != nil {
		return err
	}

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
