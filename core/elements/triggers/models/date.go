package models

import (
	"time"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers"
	"github.com/Pegasus8/piworker/core/types"
)

//FIXME Must be merged with the trigger `byHour`

// ID's
const (
	// Trigger
	byDateID = "T2"

	// Args
	dateByDateArgID = "T2-1"
)

// ByDate - Trigger
var ByDate = triggers.Trigger{
	ID:          byDateID,
	Name:        "By Date",
	Description: "",
	Run:         byDateTrigger,
	Args: []triggers.Arg{
		triggers.Arg{
			ID:   dateByDateArgID,
			Name: "Date",
			Description: "The date to launch the trigger. The format used is dd/MM/YYYY." +
				" Example: 15/11/2019",
			ContentType: types.Date,
		},
	},
}

func byDateTrigger(args *[]data.UserArg, parentTaskID string) (result bool, err error) {

	// Received hour in format 02/01/2006
	var date time.Time

	for _, arg := range *args {
		switch arg.ID {
		// Date arg
		case dateByDateArgID:
			{
				date, err = time.Parse("02/01/2006", arg.Content)
				if err != nil {
					return false, err
				}
			}

		default:
			{
				return false, ErrUnrecognizedArgID
			}
		}
	}

	if time.Now().Format("02/01/2006") == date.Format("02/01/2006") {
		return true, nil
	}

	return false, nil
}
