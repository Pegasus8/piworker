package actions

import (
	"github.com/Pegasus8/piworker/processment/data"
	"reflect"
)

// Action is a struct used in every Action
type Action struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	Run func(*ChainedResult, *data.UserAction) (bool, *ChainedResult, error)  `json:"-"`
	ReturnedChainResultDescription string `json:"returnedChainResultDescription"`
	ReturnedChainResultType reflect.Kind `json:"returnedChainResultType"`
	AcceptedChainResultDescription string `json:"acceptedChainResultDescription"`
	AcceptedChainResultType reflect.Kind `json:"acceptedChainResultType"`
	Args []Arg `json:"args"`
}

// Arg is the struct that defines every argument received by any Action.
type Arg struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	// Content interface{} `json:"content"`
	// Must be one type from here: https://bootstrap-vue.js.org/docs/components/form-input/#input-type
	ContentType string `json:"contentType"`
}

// ChainedResult is the struct used to communicate each consecutive action.
type ChainedResult struct {
	Result interface{}
	ResultType reflect.Kind
}