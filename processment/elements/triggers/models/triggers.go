package models

import (
	"github.com/Pegasus8/piworker/processment/elements/triggers"
)

// TRIGGERS is the main variable used to store all the triggers of PiWorker
var TRIGGERS = []triggers.Trigger {
	ByHour,
}