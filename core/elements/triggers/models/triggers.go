package models

import (
	"github.com/Pegasus8/piworker/core/elements/triggers"
)

// TRIGGERS is the main variable used to store all the triggers of PiWorker
var TRIGGERS = []triggers.Trigger{
	ByHour,
	ByDate,
	RaspberryTemperature,
	VariationOfFileSize,
}

// Get is a function that finds and returns a specific trigger.
func Get(id string) *triggers.Trigger {
	for _, trigger := range TRIGGERS {
		if trigger.ID == id {
			return &trigger
		}
	}

	return &triggers.Trigger{}
}
