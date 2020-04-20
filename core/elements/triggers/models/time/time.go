package time

import (
	"time"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers/shared"
	"github.com/Pegasus8/piworker/core/types"
)

// ID's
const (
	// Trigger
	triggerID = "T1"

	// Args
	arg1ID = triggerID + "-1"
	arg2ID = triggerID + "-2"
)

// ByTime - Trigger
var ByTime = shared.Trigger{
	ID:          triggerID,
	Name:        "By Time",
	Description: "",
	Run:         trigger,
	Args: []shared.Arg{
		shared.Arg{
			ID:   arg1ID,
			Name: "Date",
			Description: "The date to launch the trigger. The format used is YYYY-MM-dd." +
				" Example: 2019-11-15.",
			ContentType: types.Date,
		},
		shared.Arg{
			ID:   arg2ID,
			Name: "Hour",
			Description: "The hour to launch the  trigger. The format used is HH:mm." +
				" Example: 13:45",
			ContentType: types.Time,
		},
	},
}

func trigger(args *[]data.UserArg, parentTaskID string) (result bool, err error) {

	// Contains the time and date received from the arguments.
	var t time.Time
	var date, hour string

	for _, arg := range *args {
		switch arg.ID {
		case arg1ID:
			date = arg.Content
		case arg2ID:
			hour = arg.Content
		default:
			return false, shared.ErrUnrecognizedArgID
		}
	}

	t, err = time.Parse("2006-01-02 15:04", date + " " + hour)
	if err != nil {
		return false, err
	}

	if time.Now().Format("2006-01-02 15:04") == t.Format("2006-01-02 15:04") {
		return true, nil
	}

	return false, nil
}
