package node

import (
	"encoding/json"
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
	// MaxConcurrent caps the calls of this tool running at once.
	MaxConcurrent int
	// Cooldown is the minimum time between the end of a call and the next.
	Cooldown time.Duration
}

// ToolProvider is implemented by nodes that publish an MCP tool.
type ToolProvider interface {
	ToolSpec() ToolSpec
}
