package models

import (
	"time"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/utilities/log"
	"github.com/Pegasus8/piworker/processment/elements/triggers"
)

// ID's
const (
	// Trigger
	byDateID = "T2"

	// Args
	dateByDateArgID = "T2-1"
)

// ByDate - Trigger
var ByDate = triggers.Trigger {
	ID: byDateID,
	Name: "By Date",
	Description: "",
	Run: byDateTrigger,
	Args: []triggers.Arg {
		triggers.Arg {
			ID: dateByDateArgID,
			Name: "Date",
			Description: "The date to launch the trigger. The format used is dd/MM/YYYY." + 
				" Example: 15/11/2019",
			Content: "",
			ContentType: "string",
		},
	},
}

func byDateTrigger(args *[]data.UserArg) (result bool, err error) {
	
	// Received hour in format 02/01/2006
	var date time.Time

	for _, arg := range *args {
		switch arg.ID {
			// Date arg
			case dateByDateArgID: {
				date, err = time.Parse("02/01/2006", arg.Content)
				if err != nil {
					return false, err
				}
			}

			default: {
				log.Criticalf("Unrecognized argument with the ID '%s' on the " + 
					"trigger ByDate\n", arg.ID)
				return false, ErrUnrecognizedArgID
			}
		}
	}

	if time.Now().Format("02/01/2006") == date.Format("02/01/2006") {
		log.Infoln("Date matched, trigger launched")
		return true, nil
	}

	return false, nil
}