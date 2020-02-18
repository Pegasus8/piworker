package triggers

import (
	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/types"
)

// Trigger is a struct used in every Action
type Trigger struct {
	ID          string                                                        `json:"ID"`
	Name        string                                                        `json:"name"`
	Description string                                                        `json:"description"`
	Run         func(args *[]data.UserArg, parentTaskID string) (bool, error) `json:"-"`
	Args        []Arg                                                         `json:"args"`
}

// Arg is the struct that defines every argument received by any Trigger.
type Arg struct {
	ID          string `json:"ID"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Content interface{} `json:"content"`
	ContentType types.PWType `json:"contentType"`
}
