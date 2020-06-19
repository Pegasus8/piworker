package data

import (
	"errors"
)

// ErrBadTaskName is an error used when a task with specific name is not found
// in the JSON data file.
//var ErrBadTaskName = errors.New("invalid task name: the task name provided not exists " +
//	"in the user database")

// ErrBadTaskID is an error used when a task with specific ID is not found
// in the JSON data file.
var ErrBadTaskID = errors.New("invalid task ID: the task ID provided not exists " +
	"in the user database")

// ErrNoFilenameAssigned is an error used when the name of the json data file was not setted.
//var ErrNoFilenameAssigned = errors.New("no Filename: the filename of the data file was" +
//	" not assigned")

// ErrBackupLoopAlreadyActive is the error used when the backup loop was already started
// and is called to start again.
//var ErrBackupLoopAlreadyActive = errors.New(
//	"error: the backup loop is already active, new loop aborted",
//)

// ErrIntegrity represents the absence (or a unexpected value) of a field.
var ErrIntegrity = errors.New("some field of the task is empty or doesn't have the expected value")
