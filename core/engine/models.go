package engine

import (
	"github.com/Pegasus8/piworker/core/data"
	"time"
)

type TaskID = string

type Engine struct {
	OnStart                func() bool
	OnBackendInit          func() bool
	OnStatsLoopInit        func() bool
	OnShutdown             func()
	OnTaskLoopInit         func(id TaskID) bool
	OnTriggerActivation    func(id TaskID, trigger *data.UserTrigger) bool
	OnActionRun            func(id TaskID, action *data.UserAction, executionDuration time.Duration) bool
	OnTaskExecutionFail    func(id TaskID, err error) bool
	OnTaskExecutionSuccess func(id TaskID, executionDuration time.Duration) bool
	OnEvent                func(event *data.Event) bool
}

type Run interface {
	Start()
}

// NewEngine returns a new instance of the `Engine` struct.
func NewEngine() *Engine {
	e := &Engine{}

	/*
		The following methods should always return `true`. Otherwise the Engine
		will be stopped.
	*/

	e.OnStart = func() bool {
		return true
	}

	e.OnBackendInit = func() bool {
		return true
	}

	e.OnStatsLoopInit = func() bool {
		return true
	}

	e.OnTriggerActivation = func(_ TaskID, _ *data.UserTrigger) bool {
		return true
	}

	e.OnShutdown = func() {}

	e.OnTaskLoopInit = func(_ TaskID) bool {
		return true
	}

	e.OnTaskExecutionFail = func(_ TaskID, _ error) bool {
		return true
	}

	e.OnTaskExecutionSuccess = func(_ TaskID, _ time.Duration) bool {
		return true
	}

	e.OnEvent = func(_ *data.Event) bool {
		return true
	}

	return e
}
