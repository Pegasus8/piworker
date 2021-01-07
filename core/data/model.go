package data

import (
	"database/sql"
	"time"
)

type DatabaseInstance struct {
	Path string
	// EventBus is the channel used to transport the events related to the tasks stored in the database.
	EventBus chan Event

	instance *sql.DB
}

// UserTask is the struct that represents the data of a task created by the user.
type UserTask struct {
	Name             string       `json:"name"`
	State            TaskState    `json:"state"`
	Trigger          UserTrigger  `json:"trigger"`
	Actions          []UserAction `json:"actions"`
	Created          time.Time    `json:"created"`
	LastTimeModified time.Time    `json:"lastTimeModified"`
	ID               string       `json:"ID"`
}

// UserTrigger is the struct that represents a trigger created by the user to use on a specific task.
type UserTrigger struct {
	ID        string    `json:"ID"`
	Args      []UserArg `json:"args"`
	Timestamp string    `json:"timestamp"`
}

// UserAction is the struct that represents an action created by the user to use on a specific task.
type UserAction struct {
	ID                    string    `json:"ID"`
	Args                  []UserArg `json:"args"`
	Timestamp             string    `json:"timestamp"`
	Chained               bool      `json:"chained"`
	ArgumentToReplaceByCR string    `json:"argumentToReplaceByCR"`
	Order                 uint8     `json:"order"`
}

// UserArg is the struct that represents an argument created by the user to use on a specific trigger or action.
type UserArg struct {
	ID      string `json:"ID"`
	Content string `json:"content"`
}

// EventType represents the different situations in which a task can be involved.
type EventType uint8

const (
	// Modified represents a task that has varied some of its fields.
	Modified EventType = iota
	// Deleted represents a task that has been removed.
	Deleted
	// Added represents a new task.
	Added
	// Failed represents a task that failed during execution.
	Failed
)

// Event is the struct used represent an event related with a specific task.
type Event struct {
	Type   EventType
	TaskID string
}
