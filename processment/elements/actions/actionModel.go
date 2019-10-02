package actions

import (
	"github.com/Pegasus8/piworker/processment/data"
)

// Action is a struct used in every Action
type Action struct {
	ID string
	Name string
	Description string
	Run func(*[]data.UserArg) (bool, error)
	Args []Arg
}

// Arg is the struct that defines every argument received by any Action.
type Arg struct {
	ID string
	Name string
	Description string
	Content interface{}
	ContentType string
}