package triggers

import (
	"github.com/Pegasus8/piworker/processment/data"
)

// Trigger is a struct used in every Action
type Trigger struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	Run func(*[]data.UserArg) (bool, error) `json:"-"`
	Args []Arg `json:"args"`
}

// Arg is the struct that defines every argument received by any Trigger.
type Arg struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	Content interface{} `json:"content"`
	ContentType string `json:"content-type"`
}