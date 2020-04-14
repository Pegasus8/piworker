package date

import (
	"time"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers/shared"
	"github.com/Pegasus8/piworker/core/types"
)

//FIXME Must be merged with the trigger `byHour`

// ID's
const (
	// Trigger
	triggerID = "T2"

	// Args
	arg1ID = triggerID + "-1"
)

// ByDate - Trigger
var ByDate = shared.Trigger{
	ID:          triggerID,
	Name:        "By Date",
	Description: "",
	Run:         byDateTrigger,
	Args: []shared.Arg{
		shared.Arg{
			ID:   arg1ID,
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
		case arg1ID:
			{
				date, err = time.Parse("02/01/2006", arg.Content)
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

	if time.Now().Format("02/01/2006") == date.Format("02/01/2006") {
		return true, nil
	}

	return false, nil
}
