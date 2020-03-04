package data

import "time"

// UserTask is a struct for parse each task.
type UserTask struct {
	Name             string       `json:"name"`
	State            TaskState    `json:"state"`
	Trigger          UserTrigger  `json:"trigger"`
	Actions          []UserAction `json:"actions"`
	Created          time.Time    `json:"created"`
	LastTimeModified time.Time    `json:"lastTimeModified"`
	ID               string       `json:"ID"`
}

// UserTrigger is a struct for parsing every trigger
type UserTrigger struct {
	ID        string    `json:"ID"`
	Args      []UserArg `json:"args"`
	Timestamp string    `json:"timestamp"`
}

// UserAction is a struct for parsing every action
type UserAction struct {
	ID                    string    `json:"ID"`
	Args                  []UserArg `json:"args"`
	Timestamp             string    `json:"timestamp"`
	Chained               bool      `json:"chained"`
	ArgumentToReplaceByCR string    `json:"argumentToReplaceByCR"`
	Order                 uint8     `json:"order"`
}

// UserArg is a struct for arg parsing
type UserArg struct {
	ID      string `json:"ID"`
	Content string `json:"content"`
}

// EventType represents the different situations in which a task can be involved when the user
// data file has been modified.
type EventType uint8

const (
	// Modified represents a task that has variated some of its fields.
	Modified EventType = iota
	// Deleted represents a task that has been removed.
	Deleted
	// Added represents a new task.
	Added
	// Failed represents a task that failed during execution.
	Failed
)

// Event is the struct used to know which action must be executed by the engine
// with the given task.
type Event struct {
	Type   EventType
	TaskID string
}
