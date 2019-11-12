package uservariables

import (
	"github.com/Pegasus8/piworker/processment/data"
)

// UserVariablesPath is the directory where the files of the variables are saved.
const UserVariablesPath = data.DataPath + ".variables"

const (
	// TypeString is the constant used to represent a user variable of type string.
	TypeString = 999
	// TypeInt is the constant used to represent a user variable of type int.
	TypeInt = 998
	// TypeFloat is the constant used to represent a user variable of type float64.
	TypeFloat = 997
	// TypePath is the constant used to represent a user variable of type path (a string with the format 'some/random/path').
	TypePath = 996
)

// LocalVariablesSlice is the global variable where the local variables defined by the user are saved.
var LocalVariablesSlice []LocalVariable

// GlobalVariablesSlice is the global variable where the global variables defined by the user are saved.
var GlobalVariablesSlice []GlobalVariable

var globalVariableRgx = regexp.MustCompile(`(?m)^(:?\$)([A-Z_?]+)$`)

var localVariableRgx = regexp.MustCompile(`(?m)^(:?\$)([a-z_?]+)$`)
