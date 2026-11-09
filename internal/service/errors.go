package service

import (
	"errors"
	"time"

	"github.com/Pegasus8/piworker/internal/toolschema"
)

// Kinds of tool errors. A *ToolError matches its kind with errors.Is. They
// are tool-level outcomes (an MCP isError result, an HTTP 4xx), not internal
// failures.
var (
	// ErrToolNotFound: no running tool by that reference, or one the agent
	// may not call (hidden tools are indistinguishable from unknown ones).
	ErrToolNotFound = errors.New("unknown tool")
	// ErrPermissionDenied: the tool is visible to the agent but its token
	// lacks what calling it needs (the call scope, the privileged flag).
	ErrPermissionDenied = errors.New("permission denied")
	// ErrReconfirmRequired: the tool changed since it was granted (or its
	// grant is suspended); the admin must re-confirm the grant.
	ErrReconfirmRequired = errors.New("the tool changed since it was granted")
	// ErrInvalidArguments: the arguments do not satisfy the input schema;
	// ToolError.Issues lists why.
	ErrInvalidArguments = errors.New("invalid arguments")
	// ErrBusy: a concurrency limit is reached (the tool's, the global or the
	// token's), or the admin's approval queue for the token is full.
	ErrBusy = errors.New("busy")
	// ErrCooldown: the tool's cooldown since its last run has not passed;
	// ToolError.RetryAfter says how long remains.
	ErrCooldown = errors.New("cooldown")
	// ErrRunNotFound: no run (or approval) with that ID belongs to the actor.
	ErrRunNotFound = errors.New("unknown run")
	// ErrUnavailable: the call cannot be served now (the flow stopped, or the
	// service lacks what the call needs).
	ErrUnavailable = errors.New("unavailable")
	// ErrFlowNotFound: no stored flow with that ID (read views).
	ErrFlowNotFound = errors.New("unknown flow")
)

// ToolError is a refused call.
type ToolError struct {
	Kind       error
	Message    string
	Issues     []toolschema.Issue
	RetryAfter time.Duration
}

func toolError(kind error, msg string) *ToolError { return &ToolError{Kind: kind, Message: msg} }

func (e *ToolError) Error() string {
	if e.Message == "" {
		return e.Kind.Error()
	}
	return e.Message
}

// Unwrap makes errors.Is(err, kind) hold.
func (e *ToolError) Unwrap() error { return e.Kind }

// Code is a stable short name of the kind ("not_found", "forbidden",
// "reconfirm_required", "invalid_arguments", "busy", "cooldown",
// "run_not_found", "unavailable", "flow_not_found").
func (e *ToolError) Code() string { return errorCode(e.Kind) }

func errorCode(kind error) string {
	switch kind {
	case ErrToolNotFound:
		return "not_found"
	case ErrPermissionDenied:
		return "forbidden"
	case ErrReconfirmRequired:
		return "reconfirm_required"
	case ErrInvalidArguments:
		return "invalid_arguments"
	case ErrBusy:
		return "busy"
	case ErrCooldown:
		return "cooldown"
	case ErrRunNotFound:
		return "run_not_found"
	case ErrUnavailable:
		return "unavailable"
	case ErrFlowNotFound:
		return "flow_not_found"
	}
	return "failed"
}
