package actions

import (
	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/types"
)

// Action is a struct used in every Action
type Action struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	Run func(previousResult *ChainedResult, parentAction *data.UserAction, parentTaskName string) (bool, *ChainedResult, error)  `json:"-"`
	ReturnedChainResultDescription string `json:"returnedChainResultDescription"`
	ReturnedChainResultType types.PWType `json:"returnedChainResultType"`
	ArgumentToReplaceByCR string `json:"argumentToReplaceByCR"`
	Args []Arg `json:"args"`
}

// Arg is the struct that defines every argument received by any Action.
type Arg struct {
	ID string `json:"ID"`
	Name string `json:"name"`
	Description string `json:"description"`
	// Must be one type from here: https://bootstrap-vue.js.org/docs/components/form-input/#input-type
	ContentType types.PWType `json:"contentType"`
}

// ChainedResult is the struct used to communicate each consecutive action.
type ChainedResult struct {
	Result string
	ResultType types.PWType
}