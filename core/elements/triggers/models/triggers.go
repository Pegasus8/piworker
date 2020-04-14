package models

import (
	"github.com/Pegasus8/piworker/core/elements/triggers/shared"
	"github.com/Pegasus8/piworker/core/elements/triggers/models/date"
	"github.com/Pegasus8/piworker/core/elements/triggers/models/hour"
	// "github.com/Pegasus8/piworker/core/elements/triggers/models/everyxtime"
	"github.com/Pegasus8/piworker/core/elements/triggers/models/temp"
	"github.com/Pegasus8/piworker/core/elements/triggers/models/fsvariation"

)

// TRIGGERS is the main variable used to store all the triggers of PiWorker
var TRIGGERS = []shared.Trigger{
	hour.ByHour,
	date.ByDate,
	temp.RaspberryTemperature,
	fsvariation.VariationOfFileSize,
}

// Get is a function that finds and returns a specific trigger.
func Get(id string) *shared.Trigger {
	for _, trigger := range TRIGGERS {
		if trigger.ID == id {
			return &trigger
		}
	}

	return &shared.Trigger{}
}
