package shared

import (
	"github.com/Pegasus8/piworker/core/elements/actions/models/cmdexec"
	"github.com/Pegasus8/piworker/core/elements/actions/models/compress"
	"github.com/Pegasus8/piworker/core/elements/actions/models/getgv"
	"github.com/Pegasus8/piworker/core/elements/actions/models/getlv"
	"github.com/Pegasus8/piworker/core/elements/actions/models/setgv"
	"github.com/Pegasus8/piworker/core/elements/actions/models/setlv"
	"github.com/Pegasus8/piworker/core/elements/actions/models/writetf"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
)

// ACTIONS is the main variable used to store all the actions of PiWorker
var ACTIONS = []shared.Action{
	writetf.WriteTextFile,
	compress.CompressFilesOfDir,
	cmdexec.ExecuteCommand,
	setgv.SetGlobalVariable,
	setlv.SetLocalVariable,
	getgv.GetGlobalVariable,
	getlv.GetLocalVariable,
}

// Get is a function that finds and returns a specific action.
func Get(id string) *shared.Action {
	for _, action := range ACTIONS {
		if action.ID == id {
			return &action
		}
	}

	return &shared.Action{}
}
