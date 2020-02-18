package models

import (
	"log"
	"time"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/triggers"
	"github.com/Pegasus8/piworker/processment/types"
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
				log.Printf("[%s] Unrecognized argument with the ID '%s' on the "+
					"trigger ByDate\n", parentTaskID, arg.ID)
				return false, ErrUnrecognizedArgID
			}
		}
	}

	if time.Now().Format("02/01/2006") == date.Format("02/01/2006") {
		log.Printf("[%s] Date matched, trigger launched\n", parentTaskID)
		return true, nil
	}

	return false, nil
}
