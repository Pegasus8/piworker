// Package flow provides the core data models and runtime for flow-based automation.
package flow

import (
	"errors"
	"fmt"
)

// Common errors for flow operations.
var (
	// ErrNoTriggerNodes indicates a flow has no trigger/input nodes.
	ErrNoTriggerNodes = errors.New("flow must have at least one trigger node")

	// ErrFlowNotFound indicates the requested flow does not exist.
	ErrFlowNotFound = errors.New("flow not found")

	// ErrFlowAlreadyRunning indicates an attempt to start an already running flow.
	ErrFlowAlreadyRunning = errors.New("flow is already running")

	// ErrFlowNotRunning indicates an attempt to stop a flow that isn't running.
	ErrFlowNotRunning = errors.New("flow is not running")

	// ErrNodeNotFound indicates the requested node does not exist.
	ErrNodeNotFound = errors.New("node not found")

	// ErrInvalidNodeType indicates an unregistered node type was referenced.
	ErrInvalidNodeType = errors.New("invalid node type")

	// ErrInvalidConnection indicates a connection configuration is invalid.
	ErrInvalidConnection = errors.New("invalid connection")

	// ErrRuntimeStopped indicates the runtime has been stopped.
	ErrRuntimeStopped = errors.New("runtime has been stopped")
)

// ValidationError represents a flow validation error with details.
type ValidationError struct {
	Message string
}

// NewValidationError creates a new ValidationError with a formatted message.
func NewValidationError(format string, args ...interface{}) *ValidationError {
	return &ValidationError{
		Message: fmt.Sprintf(format, args...),
	}
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s", e.Message)
}

// NodeExecutionError represents an error that occurred during node execution.
type NodeExecutionError struct {
	NodeID   string
	NodeType string
	Err      error
}

// NewNodeExecutionError creates a new NodeExecutionError.
func NewNodeExecutionError(nodeID, nodeType string, err error) *NodeExecutionError {
	return &NodeExecutionError{
		NodeID:   nodeID,
		NodeType: nodeType,
		Err:      err,
	}
}

// Error implements the error interface.
func (e *NodeExecutionError) Error() string {
	return fmt.Sprintf("node execution error [%s:%s]: %v", e.NodeType, e.NodeID, e.Err)
}

// Unwrap returns the underlying error.
func (e *NodeExecutionError) Unwrap() error {
	return e.Err
}

// RouterError represents an error that occurred during message routing.
type RouterError struct {
	SourceNode string
	TargetNode string
	Err        error
}

// NewRouterError creates a new RouterError.
func NewRouterError(sourceNode, targetNode string, err error) *RouterError {
	return &RouterError{
		SourceNode: sourceNode,
		TargetNode: targetNode,
		Err:        err,
	}
}

// Error implements the error interface.
func (e *RouterError) Error() string {
	return fmt.Sprintf("router error [%s -> %s]: %v", e.SourceNode, e.TargetNode, e.Err)
}

// Unwrap returns the underlying error.
func (e *RouterError) Unwrap() error {
	return e.Err
}
