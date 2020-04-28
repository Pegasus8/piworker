package models

import (
	"github.com/Pegasus8/piworker/core/elements/triggers/models/everyxtime"
	"github.com/Pegasus8/piworker/core/elements/triggers/models/fsvariation"
	"github.com/Pegasus8/piworker/core/elements/triggers/models/temp"
	"github.com/Pegasus8/piworker/core/elements/triggers/models/time"
	"github.com/Pegasus8/piworker/core/elements/triggers/shared"
)

// TRIGGERS is the main variable used to store all the triggers of PiWorker
var TRIGGERS = []shared.Trigger{
	time.ByTime,
	temp.RaspberryTemperature,
	fsvariation.VariationOfFileSize,
	everyxtime.EveryXTime,
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
