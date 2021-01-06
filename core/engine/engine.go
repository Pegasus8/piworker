package engine

import (
	// "time"
	"os"

	// "github.com/Pegasus8/piworker/core/configs"
	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/engine/queue"
	"github.com/Pegasus8/piworker/core/signals"
	"github.com/Pegasus8/piworker/core/stats"
	"github.com/Pegasus8/piworker/webui/backend"
	"github.com/rs/zerolog/log"
)

// StartEngine is the function used to start the Dynamic Engine
func (engine *Engine) Start() {
	log.Info().Msg("Starting the Dynamic Engine...")

	// Hook
	if !engine.OnStart() {
		return
	}

	defer func() {
		err := os.RemoveAll(TempDir)
		if err != nil {
			log.Error().Err(err).Msg("Error when trying to remove temp dir")
		}
	}()

	var stopSignal = make(chan struct{})
	var tasksGoroutines = make(map[string]chan data.UserTask)
	// 0 = stopped by the system. For example, on a system shutdown.
	// 1 = stopped by the user. For example, changing the state of the task.
	// 2 = task deleted by the user.
	var managementChannels = make(map[string]chan uint8)
	defer func() {
		stopSignal <- struct{}{}
		// Stop each loop when the engine is going to shutdown. This is with the intention
		// of handle some post execution operations.
		for c := range managementChannels {
			managementChannels[c] <- 0
		}
	}()
	data.EventBus = make(chan data.Event)
	actionsQ := queue.NewQueue()

	err := checkTempDir()
	if err != nil {
		log.Panic().Err(err).Msg("Error when trying to create the temp dir")
	}

	log.Info().Msg("Reading the user data for first time...")

	activeTasks, err := engine.userdataDB.GetActiveTasks()
	if err != nil {
		log.Panic().
			Err(err).
			Msgf("Error when trying to read the tasks with state '%s'\n", data.StateTaskActive)
	}

	// Set Stats.Inactive counter (`inactiveTasks` will be used only here).
	inactiveTasks, err := engine.userdataDB.GetInactiveTasks()
	if err != nil {
		log.Panic().
			Err(err).
			Msgf("Error when trying to read the tasks with state '%s'\n", data.StateTaskInactive)
	}
	stats.Current.Lock()
	stats.Current.TasksStats.InactiveTasks = uint16(len(*inactiveTasks))
	stats.Current.Unlock()

	failedTasks, err := engine.userdataDB.GetFailedTasks()
	if err != nil {
		log.Panic().
			Err(err).
			Msgf("Error when trying to read the tasks with state '%s'\n", data.StateTaskFailed)
	}
	stats.Current.Lock()
	stats.Current.TasksStats.FailedTasks = uint8(len(*failedTasks))
	stats.Current.Unlock()

	log.Info().Msg("Creating channels for active tasks...")
	for _, task := range *activeTasks {
		// Create the channel for each task (with active state).
		tasksGoroutines[task.ID] = make(chan data.UserTask)
		managementChannels[task.ID] = make(chan uint8)
		go engine.runTaskLoop(task.ID, tasksGoroutines[task.ID], managementChannels[task.ID], actionsQ)

		tasksGoroutines[task.ID] <- task

		stats.Current.Lock()
		stats.Current.TasksStats.ActiveTasks++
		stats.Current.Unlock()
	}
	log.Info().Msg("Channels created correctly")

	// Start the server of the WebUI.
	log.Info().Msg("Starting the WebUI server...")
	go backend.Run(engine.userdataDB, engine.configs)

	// Hook
	if !engine.OnBackendInit() {
		return
	}

	// Start the stats recollection.
	log.Info().Msg("Starting the stats loop...")
	go stats.StartLoop(stopSignal)

	// Hook
	if !engine.OnStatsLoopInit() {
		return
	}

	go func() {
		for {
			event := <-data.EventBus

			if !engine.OnEvent(&event) {
				continue
			}

			switch event.Type {
			case data.Added:
				{
					// Get the recently added task by it ID.
					t, err := engine.userdataDB.GetTaskByID(event.TaskID)
					if err != nil {
						log.Panic().Err(err).Msg("Error when responding to an event of type Added")
					}

					// Only add the new task if the state is 'active'.
					if t.State != data.StateTaskActive {
						stats.Current.Lock()
						stats.Current.TasksStats.InactiveTasks++
						stats.Current.Unlock()
						updateTStatsDB()

						continue
					}

					// Because the task is new, the proper channel and loop must be initialized.
					tasksGoroutines[t.ID] = make(chan data.UserTask)
					managementChannels[t.ID] = make(chan uint8)
					go engine.runTaskLoop(t.ID, tasksGoroutines[t.ID], managementChannels[t.ID], actionsQ)

					// Once the loop and the channels are initialized is time to send the new task.
					tasksGoroutines[t.ID] <- *t

					stats.Current.Lock()
					stats.Current.TasksStats.ActiveTasks++
					stats.Current.Unlock()
					updateTStatsDB()
				}
			case data.Modified:
				{
					// Get the recently modified task by it ID.
					t, err := engine.userdataDB.GetTaskByID(event.TaskID)
					if err != nil {
						log.Panic().Err(err).Msg("Error when responding to an event of type Modified")
					}

					if t.State != data.StateTaskActive {
						// Check if the task has been running before the event.
						if _, ok := tasksGoroutines[event.TaskID]; ok {
							// Send the signal to indicate the change of the state, and thus, the detention of the task loop.
							// Note: here we haven't checked if the management channel for this task exists, this
							// is unnecessary because both channels are initialized simultaneously, so if one exists, the other too.
							managementChannels[event.TaskID] <- 1

							// Close the channels and delete them from their maps.
							close(tasksGoroutines[event.TaskID])
							close(managementChannels[event.TaskID])
							delete(tasksGoroutines, event.TaskID)
							delete(managementChannels, event.TaskID)

							stats.Current.Lock()
							stats.Current.TasksStats.ActiveTasks--
							stats.Current.TasksStats.InactiveTasks++
							stats.Current.Unlock()
							updateTStatsDB()
						}
						// If the task was not running, there is nothing to do.

					} else {
						// If the channel already exists, the previous state of the task was the same (active), so
						// there is no necessity to send a signal thought the management channel, just send the updated
						// data.
						if _, ok := tasksGoroutines[event.TaskID]; ok {
							tasksGoroutines[t.ID] <- *t
						} else {
							// If the channel doesn't exists, the previously state of the task was another than 'active',
							// so the task must be managed as a new one.
							tasksGoroutines[t.ID] = make(chan data.UserTask)
							managementChannels[t.ID] = make(chan uint8)
							go engine.runTaskLoop(t.ID, tasksGoroutines[t.ID], managementChannels[t.ID], actionsQ)

							// Once the loop and the channels are initialized is time to send the new task.
							tasksGoroutines[t.ID] <- *t

							stats.Current.Lock()
							stats.Current.TasksStats.ActiveTasks++
							stats.Current.TasksStats.InactiveTasks--
							stats.Current.Unlock()
							updateTStatsDB()
						}
					}
				}
			case data.Deleted:
				{
					// If the task is not running (state != 'active'), skip the iteration.
					if _, ok := tasksGoroutines[event.TaskID]; !ok {
						stats.Current.Lock()
						stats.Current.TasksStats.InactiveTasks--
						stats.Current.Unlock()
						updateTStatsDB()
						continue
					}

					// Send a signal of detention (2 = task deleted).
					managementChannels[event.TaskID] <- 2

					// And finally close the channels and delete them from the maps.
					close(tasksGoroutines[event.TaskID])
					close(managementChannels[event.TaskID])
					delete(tasksGoroutines, event.TaskID)
					delete(managementChannels, event.TaskID)

					stats.Current.Lock()
					stats.Current.TasksStats.ActiveTasks--
					stats.Current.Unlock()
					updateTStatsDB()
				}
			case data.Failed:
				{
					// Close the channels of the failed task and delete them of the maps.
					close(tasksGoroutines[event.TaskID])
					close(managementChannels[event.TaskID])
					delete(tasksGoroutines, event.TaskID)
					delete(managementChannels, event.TaskID)

					// Decrease the active tasks counter.
					stats.Current.Lock()
					stats.Current.TasksStats.ActiveTasks--
					stats.Current.TasksStats.FailedTasks++
					stats.Current.Unlock()
					updateTStatsDB()
				}
			}
		}
	}()

	<-signals.Shutdown

	// Hook
	engine.OnShutdown()
}

func updateTStatsDB() {
	stats.Current.RLock()
	err := stats.StoreTStats(&stats.Current.TasksStats)
	stats.Current.RUnlock()

	if err != nil {
		log.Error().Err(err).Msg("Error when storing tasks stats")
	}
}

func checkTempDir() error {
	// Create temp dir if not exists
	err := os.MkdirAll(TempDir, 0755)
	if err != nil {
		return err
	}

	return nil
}
