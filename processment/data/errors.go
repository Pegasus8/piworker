package data

import (
	"errors"
)


// ErrBadTaskName is an error used when a task with specific name is not finded
// in the JSON data file.
var	ErrBadTaskName = errors.New("Invalid task name: the task name provided not exists "+
		"in the user database.")


