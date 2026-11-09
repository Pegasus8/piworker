// Package node provides the interface definitions and registry for flow nodes.
package node

import (
	"context"

	"github.com/Pegasus8/piworker/internal/types"
)

// Node is the base interface that all nodes must implement.
// A Node processes incoming messages and produces output messages.
type Node interface {
	// Process handles an incoming message and returns zero or more output messages.
	// The context can be used for cancellation and timeout handling.
	// Returning an error will stop the message from being propagated further.
	Process(ctx context.Context, msg *types.Message) ([]*types.Message, error)

	// Ports returns the input and output port definitions for this node.
	// This is used for flow validation and UI rendering.
	Ports() (inputs []types.Port, outputs []types.Port)

	// Validate checks if the node's configuration is valid.
	// This is called before the flow is deployed.
	Validate() error
}

// TriggerNode is a specialized node that can initiate flow execution.
// Trigger nodes are typically input nodes like timers, webhooks, or event listeners.
type TriggerNode interface {
	Node

	// Start begins the trigger's activity.
	// Messages should be sent to the provided channel.
	// The context is used for cancellation.
	// This method should return quickly; long-running operations should be in goroutines.
	Start(ctx context.Context, out chan<- *types.Message) error

	// Stop gracefully stops the trigger.
	// This should clean up any resources and stop any goroutines.
	Stop() error
}

// ConfigurableNode is a node that can be configured with settings.
type ConfigurableNode interface {
	Node

	// Configure applies configuration settings to the node.
	// This is called when the node is instantiated.
	Configure(config map[string]interface{}) error

	// GetConfig returns the current configuration of the node.
	GetConfig() map[string]interface{}

	// GetConfigSchema returns a JSON schema describing the configuration options.
	// This is used for UI rendering and validation.
	GetConfigSchema() ConfigSchema
}

// ConfigSchema describes the configuration options for a node.
type ConfigSchema struct {
	Properties map[string]ConfigProperty `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// ConfigProperty describes a single configuration property. CheckConfig
// enforces Type, Enum, Minimum and Maximum when a flow is deployed.
//
// Type "object" is a map of name/value pairs (the editor edits it as rows,
// e.g. HTTP headers); "json" is a free-form JSON object the editor edits as
// JSON text and stores parsed (e.g. a tool's input schema).
type ConfigProperty struct {
	Type        string      `json:"type"`                  // string, number, integer, boolean, array, object, json ("" = any)
	Title       string      `json:"title"`                 // Human-readable title
	Description string      `json:"description,omitempty"` // Help text; states the unit of numeric values
	Default     interface{} `json:"default,omitempty"`     // Default value
	Enum        []string    `json:"enum,omitempty"`        // For select inputs; matched exactly unless CaseInsensitive
	Minimum     *float64    `json:"minimum,omitempty"`     // For number inputs
	Maximum     *float64    `json:"maximum,omitempty"`     // For number inputs
	Unit        string      `json:"unit,omitempty"`        // Unit of a numeric value (e.g. "milliseconds"), quoted in errors
	// CaseInsensitive lets Enum values match in any case. Set it only when the
	// node itself normalises the value's case (e.g. strings.ToUpper).
	CaseInsensitive bool `json:"caseInsensitive,omitempty"`

	// Sensitive marks a credential (password, token, DSN, secret-bearing URL,
	// …). Its value is never echoed in errors, and agent-authored drafts may
	// only set it to a {{secret.NAME}} reference.
	Sensitive bool `json:"sensitive,omitempty"`
	// AgentVisible marks a value that is harmless to show to MCP agents.
	// The default (false) means the value is redacted in agent read views.
	AgentVisible bool `json:"agentVisible,omitempty"`
	// Destination marks where the node sends data or connects: URLs, hosts,
	// brokers, topics, recipients, chat ids, file paths written, webhook
	// paths, pins and buses written. The draft approval page and the low
	// agent-deploy policy flag changes to it (flow.Diff).
	Destination bool `json:"destination,omitempty"`
	// RiskSwitch marks an opt-in that widens what the node may do
	// (allowPrivate, allowDashArgs, TLS-verification bypasses, …). Turning
	// one on is flagged by flow.Diff, and agent drafts may not turn it on.
	RiskSwitch bool `json:"riskSwitch,omitempty"`
	// Operation marks what selects the action a request performs at its
	// destination (an HTTP method or body, an SQL statement), as opposed to
	// message content. flow.Diff flags a change to it on a node that uses a
	// credential (credentialed_request_changed): the approved credential
	// would be used for something else.
	Operation bool `json:"operation,omitempty"`
	// Rate marks what sets how often a trigger fires or how many messages
	// a node lets through: an interval, a cron expression, a rate limit, a
	// batch size, a cooldown. flow.Diff flags a change to it, or a node
	// with one added or removed, on a flow that reaches other services
	// (rate_changed): how often it sends is the admin's to approve.
	Rate bool `json:"rate,omitempty"`
}

// BaseNode provides a common implementation for simple nodes.
// Embed this in your node implementations to get default behavior.
type BaseNode struct {
	config map[string]interface{}
}

// NewBaseNode creates a new BaseNode with the given configuration.
func NewBaseNode(config map[string]interface{}) *BaseNode {
	if config == nil {
		config = make(map[string]interface{})
	}
	return &BaseNode{config: config}
}

// Configure applies configuration settings to the node.
func (n *BaseNode) Configure(config map[string]interface{}) error {
	n.config = config
	return nil
}

// GetConfig returns the current configuration.
func (n *BaseNode) GetConfig() map[string]interface{} {
	return n.config
}

// GetConfigString retrieves a string configuration value.
func (n *BaseNode) GetConfigString(key string, defaultValue string) string {
	if v, ok := n.config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultValue
}

// GetConfigInt retrieves an integer configuration value.
func (n *BaseNode) GetConfigInt(key string, defaultValue int) int {
	if v, ok := n.config[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return defaultValue
}

// GetConfigFloat retrieves a float configuration value.
func (n *BaseNode) GetConfigFloat(key string, defaultValue float64) float64 {
	if v, ok := n.config[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case float32:
			return float64(val)
		case int:
			return float64(val)
		case int64:
			return float64(val)
		}
	}
	return defaultValue
}

// GetConfigBool retrieves a boolean configuration value.
func (n *BaseNode) GetConfigBool(key string, defaultValue bool) bool {
	if v, ok := n.config[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultValue
}

// Validate performs basic validation. Override in specific implementations.
func (n *BaseNode) Validate() error {
	return nil
}
