package data

import (
	"errors"
)


// ErrBadTaskName is an error used when a task with specific name is not finded.
// in the JSON data file.
var	ErrBadTaskName = errors.New("Invalid task name: the task name provided not exists "+
		"in the user database.")

// ErrNoFilenameAsigned is an error used when the name of the json data file was not setted.
var ErrNoFilenameAsigned = errors.New("No Filename: the filename of the data file was" + 
	" not asigned")

// ErrBackupLoopAlreadyActive is the error used when the backup loop was already started 
// and is called to start again.
var ErrBackupLoopAlreadyActive = errors.New(
	"Error: the backup loop is already active, new loop aborted",
)