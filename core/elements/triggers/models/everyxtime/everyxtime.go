package everyxtime

import (
	"time"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers/shared"
	"github.com/Pegasus8/piworker/core/types"
)

// ID's
const (
	// Trigger
	triggerID = "T4"

	// Args
	arg1ID = triggerID + "-1"
)

// EveryXTime - Trigger - NOT IMPLEMENTED
var _EveryXTime = shared.Trigger{
	ID:          triggerID,
	Name:        "Every X Time",
	Description: "",
	Run:         everyXTimeTrigger,
	Args: []shared.Arg{
		shared.Arg{
			ID:   arg1ID,
			Name: "Time",
			Description: "Time of repetition. Format must be '1h10m10s' where" +
				" 'h' = hours, 'm' = minutes and 's' = seconds.",
			// Content:     "",
			ContentType: types.Text,
		},
	},
}

var nextExecution time.Time

func everyXTimeTrigger(args *[]data.UserArg, parentTaskID string) (result bool, err error) {
	// Time
	var timeToWait time.Duration

	for _, arg := range *args {
		switch arg.ID {
		case arg1ID:
			timeToWait, err = time.ParseDuration(arg.Content)
			if err != nil {
				return false, err
			}
		default:
			{
				return false, shared.ErrUnrecognizedArgID
			}
		}
	}

	if nextExecution.IsZero() {
		nextExecution = time.Now().Add(timeToWait)
		return false, nil
	}

	//TODO

	return false, nil
}
