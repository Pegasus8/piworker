package shared

import (
	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/types"
)

// Action represents an action of a task. Each task can have multiple actions
// and they will be executed according to the order given by the user (see data.UserAction.Order).
type Action struct {
	ID                             string                                                                                                                `json:"ID"`
	Name                           string                                                                                                                `json:"name"`
	Description                    string                                                                                                                `json:"description"`
	Run                            func(previousResult *ChainedResult, parentAction *data.UserAction, parentTaskID string) (bool, *ChainedResult, error) `json:"-"`
	ReturnedChainResultDescription string                                                                                                                `json:"returnedChainResultDescription"`
	ReturnedChainResultType        types.PWType                                                                                                          `json:"returnedChainResultType"`
	Args                           []Arg                                                                                                                 `json:"args"`
}

// Arg is the struct that defines each argument received by an Action.
type Arg struct {
	ID          string       `json:"ID"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	ContentType types.PWType `json:"contentType"`
}

// ChainedResult is the struct used to communicate each consecutive action.
type ChainedResult struct {
	Result     string
	ResultType types.PWType
}
