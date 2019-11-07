package models

import (
	"time"
	"log"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/triggers"
)

// ID's
const (
	// Trigger
	everyXTime = "T4"

	// Args
	timeEveryXTimeArgID = "T4-1"
)

// EveryXTime - Trigger - NOT IMPLEMENTED
var _EveryXTime = triggers.Trigger{
	ID:          everyXTime,
	Name:        "Every X Time",
	Description: "",
	Run:         everyXTimeTrigger,
	Args: []triggers.Arg{
		triggers.Arg{
			ID:   timeEveryXTimeArgID,
			Name: "Time",
			Description: "Time of repetition. Format must be '1h10m10s' where" +
				" 'h' = hours, 'm' = minutes and 's' = seconds.",
			// Content:     "",
			ContentType: "text",
		},
	},
}

var nextExecution time.Time

func everyXTimeTrigger(args *[]data.UserArg) (result bool, err error) {
	// Time
	var timeToWait time.Duration

	for _, arg := range *args {
		switch arg.ID {
		case timeEveryXTimeArgID:
			timeToWait, err = time.ParseDuration(arg.Content)
			if err != nil {
				return false, err
			}
		default:
			{
				log.Printf("Unrecognized argument with the ID '%s' on the "+
					"trigger EveryXTime\n", arg.ID)
				return false, ErrUnrecognizedArgID
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
