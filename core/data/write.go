package data

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// NewTask is a function used to add a new task to the database.
func (db *DatabaseInstance) NewTask(task *UserTask) error {
	if c := checkIntegrity(task); !c {
		return ErrIntegrity
	}

	sqlStatement := `
	INSERT INTO Tasks(
		ID,
		Name,
		State,
		Trigger,
		Actions,
		Created,
		LastTimeModified
	) values (?,?,?,?,?,?,?)
	`

	// Set the task ID
	task.ID = uuid.New().String()

	stmt, err := db.instance.Prepare(sqlStatement)
	if err != nil {
		return err
	}
	defer func() {
		err := stmt.Close()
		if err != nil {
			log.Error().Err(err).Str("taskID", task.ID).Msg("")
		}
	}()

	trigger, err := json.Marshal(task.Trigger)
	if err != nil {
		return err
	}

	actions, err := json.Marshal(task.Actions)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(
		task.ID,
		task.Name,
		task.State,
		string(trigger),
		string(actions),
		task.Created,
		task.LastTimeModified,
	)
	if err != nil {
		return err
	}

	event := Event{
		Type:   Added,
		TaskID: task.ID,
	}
	db.EventBus <- event

	return nil
}

func checkIntegrity(t *UserTask) bool {
	if t.Name == "" {
		return false
	}

	// These are the admitted states. `StateTaskOnExecution` and `StateTaskFailed` can be used only by PW itself.
	if !(t.State == StateTaskActive || t.State == StateTaskInactive) {
		return false
	}

	// *--- Trigger check ---*
	if t.Trigger.ID == "" {
		return false
	}

	for _, tArg := range t.Trigger.Args {
		if tArg.ID == "" {
			return false
		}
	}
	// --- End of Trigger check ---

	// *--- Actions check ---*
	for _, action := range t.Actions {
		if action.ID == "" {
			return false
		}

		for _, aArg := range action.Args {
			if aArg.ID == "" {
				return false
			}
		}
	}
	// --- End of Actions check ---

	return true
}
