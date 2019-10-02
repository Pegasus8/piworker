package actions

import (
	"github.com/Pegasus8/piworker/processment/data"
)

// Action is a struct used in every Action
type Action struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	Run func(*[]data.UserArg) (bool, error)  `json:"-"`
	Args []Arg `json:"args"`
}

// Arg is the struct that defines every argument received by any Action.
type Arg struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	Content interface{} `json:"content"`
	ContentType string `json:"content-type"`
}