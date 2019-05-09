package models

import  (
	"github.com/Pegasus8/piworker/processment/elements/actions"
)

// ACTIONS is the main variable used to store all the actions of PiWorker
var ACTIONS = []actions.Action {
	WriteTextFile,
	CompressFilesOfDir,
}