package uservariables

import (
	"github.com/Pegasus8/piworker/core/types"
)

/**
 *  What is the difference between a LocalVariable and a GlobalVariable?
 *	The difference is that the GlobalVariable can be used in any task, when the
 *	LocalVariable only can be used on the same task where is declared.
 *
 *	GlobalVariable name syntax -> $SOME_GLOBAL_VARIABLE
 *	LocalVariable name syntax -> $some_local_variable
 */

// LocalVariable is the struct used to represent a local variable of the user.
type LocalVariable struct {
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	Content      string       `json:"content"`
	Type         types.PWType `json:"type"`
	ParentTaskID string       `json:"parentTaskID"`
}

// GlobalVariable is the struct used to represent a global variable of the user.
type GlobalVariable struct {
	ID      int          `json:"id"`
	Name    string       `json:"name"`
	Content string       `json:"content"`
	Type    types.PWType `json:"type"`
}
