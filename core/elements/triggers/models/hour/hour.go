package hour

import (
	"time"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers/shared"
	"github.com/Pegasus8/piworker/core/types"
)

//FIXME Must be merged with the trigger `byDate`

// ID's
const (
	// Trigger
	triggerID = "T1"

	// Args
	arg1ID = triggerID + "-1"
)

// ByHour - Trigger
var ByHour = shared.Trigger{
	ID:          triggerID,
	Name:        "By Hour",
	Description: "",
	Run:         byHourTrigger,
	Args: []shared.Arg{
		shared.Arg{
			ID:   arg1ID,
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
		case arg1ID:
			{
				hour, err = time.Parse("15:04", arg.Content)
				if err != nil {
					return false, err
				}
			}

		default:
			{
				return false, shared.ErrUnrecognizedArgID
			}
		}
	}

	if time.Now().Format("15:04") == hour.Format("15:04") {
		return true, nil
	}

	return false, nil
}
