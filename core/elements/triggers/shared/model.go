package shared

import (
	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/types"
)

// Trigger represents the trigger of a task. 
// Once activated it will cause the actions of the task to be executed.
type Trigger struct {
	ID          string                                                        `json:"ID"`
	Name        string                                                        `json:"name"`
	Description string                                                        `json:"description"`
	Run         func(args *[]data.UserArg, parentTaskID string) (bool, error) `json:"-"`
	Args        []Arg                                                         `json:"args"`
}

// Arg is the struct that defines each argument received by a Trigger.
type Arg struct {
	ID          string `json:"ID"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Content interface{} `json:"content"`
	ContentType types.PWType `json:"contentType"`
}
