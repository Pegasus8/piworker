package node

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/toolschema"
)

// Confirmation policies of a tool (ToolSpec.Confirm, ADR-0012).
const (
	// ToolConfirmNone: the tool runs when called.
	ToolConfirmNone = "none"
	// ToolConfirmClient asks MCP clients to confirm with their user first.
	// It is UX only: it does not stop an agent that calls /mcp directly.
	ToolConfirmClient = "client"
	// ToolConfirmAdmin holds every call until the admin approves it in the
	// editor (server-side gate).
	ToolConfirmAdmin = "admin"
)

// ToolConfirmModes lists the confirmation policies, least strict first.
func ToolConfirmModes() []string {
	return []string{ToolConfirmNone, ToolConfirmClient, ToolConfirmAdmin}
}

// ToolSpec is what a callable trigger (trigger-tool) publishes about itself:
// the tool an agent sees and the limits the service layer applies to calls.
// It is read from a node instance after the flow validated its config. The
// effective risk and annotations of the tool are computed per flow, not
// declared here (ADR-0013).
type ToolSpec struct {
	// Name is the MCP tool name, unique across running flows.
	Name string
	// Title and Description are shown to agents.
	Title       string
	Description string
	// InputSchema validates the call arguments (the closed subset).
	InputSchema *toolschema.Schema
	// OutputSchema is the optional JSON Schema (an object schema) of the
	// reply, advertised as is; nil when the tool declares none.
	OutputSchema json.RawMessage
	// Timeout is how long a call waits for the reply before it reports
	// "running".
	Timeout time.Duration
	// Confirm is one of ToolConfirmNone, ToolConfirmClient, ToolConfirmAdmin.
	Confirm string
	// Idempotent is the admin's idempotentHint.
	Idempotent bool
	// MaxConcurrent caps the calls of this tool running at once. 0 means
	// the admin left it unset: the default is ToolDefaultMaxConcurrent, or
	// ToolReadOnlyMaxConcurrent for read-only tools, decided by the flow's
	// risk analysis (see EffectiveMaxConcurrent).
	MaxConcurrent int
	// Cooldown is the minimum time between the end of a call and the next.
	Cooldown time.Duration
}

// Defaults of ToolSpec.MaxConcurrent when the admin leaves it unset.
const (
	// ToolDefaultMaxConcurrent: calls of a tool run one at a time.
	ToolDefaultMaxConcurrent = 1
	// ToolReadOnlyMaxConcurrent: a read-only tool runs up to 4 calls at once.
	ToolReadOnlyMaxConcurrent = 4
)

// EffectiveMaxConcurrent returns the concurrency limit of the tool: the
// admin's MaxConcurrent, or the default for a read-only tool (readOnly comes
// from the flow's risk analysis) or any other tool when it is unset.
func (s ToolSpec) EffectiveMaxConcurrent(readOnly bool) int {
	switch {
	case s.MaxConcurrent > 0:
		return s.MaxConcurrent
	case readOnly:
		return ToolReadOnlyMaxConcurrent
	default:
		return ToolDefaultMaxConcurrent
	}
}

// ToolProvider is implemented by nodes that publish an MCP tool.
type ToolProvider interface {
	ToolSpec() ToolSpec
}

// ErrInvalidToolArguments is matched (errors.Is) by the error a tool
// trigger's Emit returns for arguments outside its input schema, through any
// wrapping (FlowRuntime.Call wraps it). errors.As with *ToolArgumentsError
// gives the problems.
var ErrInvalidToolArguments = errors.New("invalid tool arguments")

// ToolArgumentsError lists why a tool's arguments were refused, each issue
// located by a JSON pointer into the arguments ("" for the whole value).
type ToolArgumentsError struct {
	Issues []toolschema.Issue
}

func (e *ToolArgumentsError) Error() string {
	if len(e.Issues) == 0 {
		return ErrInvalidToolArguments.Error()
	}
	parts := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		parts[i] = issue.String()
	}
	return ErrInvalidToolArguments.Error() + ": " + strings.Join(parts, "; ")
}

// Unwrap makes errors.Is(err, ErrInvalidToolArguments) hold.
func (e *ToolArgumentsError) Unwrap() error { return ErrInvalidToolArguments }
