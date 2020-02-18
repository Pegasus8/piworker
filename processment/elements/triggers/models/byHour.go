package models

import (
	"log"
	"time"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/triggers"
	"github.com/Pegasus8/piworker/processment/types"
)

//FIXME Must be merged with the trigger `byDate`

// ID's
const (
	// Trigger
	byHourID = "T1"

	// Args
	hourByHourArgID = "T1-1"
)

// ByHour - Trigger
var ByHour = triggers.Trigger{
	ID:          byHourID,
	Name:        "By Hour",
	Description: "",
	Run:         byHourTrigger,
	Args: []triggers.Arg{
		triggers.Arg{
			ID:   hourByHourArgID,
			Name: "Hour",
			Description: "The hour to launch the  trigger. The format used is HH:mm." +
				" Example: 13:45",
			// Content: "",
			ContentType: types.Time,
		},
	},
}

func byHourTrigger(args *[]data.UserArg, parentTaskID string) (result bool, err error) {

	// Received hour in format 15:04
	var hour time.Time

	for _, arg := range *args {
		switch arg.ID {
		// Hour arg
		case hourByHourArgID:
			{
				hour, err = time.Parse("15:04", arg.Content)
				if err != nil {
					return false, err
				}
			}

		default:
			{
				log.Printf("[%s] Unrecognized argument with the ID '%s' on the "+
					"trigger ByHour\n", parentTaskID, arg.ID)
				return false, ErrUnrecognizedArgID
			}
		}
	}

	if time.Now().Format("15:04") == hour.Format("15:04") {
		log.Printf("[%s] Hour matched, trigger launched\n", parentTaskID)
		return true, nil
	}

	return false, nil
}
