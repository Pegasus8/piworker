package uservariables

import (
	"github.com/Pegasus8/piworker/core/data"
	"regexp"
)

// UserVariablesPath is the directory where the files of the variables are saved.
const UserVariablesPath = data.DataPath + ".variables"

// LocalVariablesSlice is the global variable where the local variables defined by the user are saved.
var LocalVariablesSlice *[]LocalVariable

// GlobalVariablesSlice is the global variable where the global variables defined by the user are saved.
var GlobalVariablesSlice *[]GlobalVariable

var globalVariableRgx = regexp.MustCompile(`^(:?\$)([A-Z_?]+)$`)

var localVariableRgx = regexp.MustCompile(`^(:?\$)([a-z_?]+)$`)
