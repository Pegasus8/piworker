package actions

import (
	"github.com/Pegasus8/piworker/processment/data"
)

// Action is a struct used in every Action
type Action struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	Run func(*ChainedResult, *[]data.UserArg) (bool, *ChainedResult, error)  `json:"-"`
	Args []Arg `json:"args"`
}

// Arg is the struct that defines every argument received by any Action.
type Arg struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	// Content interface{} `json:"content"`
	ContentType string `json:"content-type"`
}

// ChainedResult is the struct used to communicate each consecutive action.
type ChainedResult struct {
	Result interface{}
	ResultType interface{}
}