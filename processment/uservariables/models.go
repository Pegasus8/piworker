package uservariables

import (
	"sync"
)

/**
 *  What is the difference between a LocalVariable and a GlobalVariable?
 *	The difference is that the GlobalVariable can be used in any task, when the
 *	LocalVariable only can be used on the same task where is declared.
 *
 *	GlobalVariable name sintax -> $SOME_GLOBAL_VARIABLE
 *	LocalVariable name sintax -> $some_local_variable
 */

// LocalVariable is the struct used to represent a local variable of the user.
type LocalVariable struct {
	Name           string `json:"name"`
	Content        string `json:"content"`
	Type           uint    `json:"type"`
	ParentTaskName string `json:"parentTaskName"`
	*sync.RWMutex
}

// GlobalVariable is the struct used to represent a global variable of the user.
type GlobalVariable struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Type    uint    `json:"type"`
	*sync.RWMutex
}

var globalMutex = sync.Mutex{}
