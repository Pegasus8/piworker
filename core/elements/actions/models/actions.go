package models

import (
	"github.com/Pegasus8/piworker/core/elements/actions"
)

// ACTIONS is the main variable used to store all the actions of PiWorker
var ACTIONS = []actions.Action{
	WriteTextFile,
	CompressFilesOfDir,
	ExecuteCommand,
	SetGlobalVariable,
	SetLocalVariable,
	GetGlobalVariable,
	GetLocalVariable,
}

// Get is a function that finds and returns a specific action.
func Get(id string) *actions.Action {
	for _, action := range ACTIONS {
		if action.ID == id {
			return &action
		}
	}

	return &actions.Action{}
}
