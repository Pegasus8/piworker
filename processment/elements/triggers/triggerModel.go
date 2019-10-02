package triggers

import (
	"github.com/Pegasus8/piworker/processment/data"
)

// Trigger is a struct used in every Action
type Trigger struct {
	ID string
	Name string
	Description string
	Run func(*[]data.UserArg) (bool, error)
	Args []Arg
}

// Arg is the struct that defines every argument received by any Trigger.
type Arg struct {
	ID string
	Name string
	Description string
	Content interface{}
	ContentType string
}