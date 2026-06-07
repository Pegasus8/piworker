package builtin

import (
	"context"
	"encoding/json"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// DebugProcess inspects and logs messages passing through the flow.
// It's useful for debugging and understanding message content.
type DebugProcess struct {
	*node.BaseNode

	name        string
	logPayload  bool
	logMeta     bool
	passThrough bool
	logger      zerolog.Logger
}

// NewDebugProcess creates a new DebugProcess with the given configuration.
func NewDebugProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	process := &DebugProcess{
		BaseNode:    base,
		name:        base.GetConfigString("name", "Debug"),
		logPayload:  base.GetConfigBool("logPayload", true),
		logMeta:     base.GetConfigBool("logMeta", true),
		passThrough: base.GetConfigBool("passThrough", true),
		logger:      log.With().Str("component", "debug-node").Logger(),
	}

	return process, nil
}

// Process inspects and logs the message, optionally passing it through.
func (p *DebugProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	// Build the log event
	event := p.logger.Info().
		Str("debugName", p.name).
		Str("messageID", msg.ID).
		Str("sourceNode", msg.SourceNode).
		Str("sourcePort", msg.SourcePort).
		Str("topic", msg.Topic).
		Str("payloadType", string(msg.PayloadType)).
		Time("timestamp", msg.Timestamp)

	// Log payload if enabled
	if p.logPayload {
		// Try to serialize payload for pretty printing
		if payloadBytes, err := json.Marshal(msg.Payload); err == nil {
			event.RawJSON("payload", payloadBytes)
		} else {
			event.Interface("payload", msg.Payload)
		}
	}

	// Log metadata if enabled
	if p.logMeta && len(msg.Meta) > 0 {
		if metaBytes, err := json.Marshal(msg.Meta); err == nil {
			event.RawJSON("meta", metaBytes)
		} else {
			event.Interface("meta", msg.Meta)
		}
	}

	event.Msg("Debug node received message")

	// If passThrough is enabled, forward the message
	if p.passThrough {
		outputMsg := msg.Clone()
		outputMsg.SourcePort = "output"
		outputMsg.SetMeta("debugged", true)
		outputMsg.SetMeta("debugName", p.name)
		return []*types.Message{outputMsg}, nil
	}

	// Otherwise, terminate the message here
	return nil, nil
}

// Ports returns the port definitions for this node.
func (p *DebugProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{
			{
				ID:       "input",
				Name:     "Input",
				DataType: types.DataTypeAny,
				Required: true,
			},
		}, []types.Port{
			{
				ID:       "output",
				Name:     "Output",
				DataType: types.DataTypeAny,
				Multiple: true,
			},
		}
}

// Validate checks if the configuration is valid.
func (p *DebugProcess) Validate() error {
	// No validation needed - all configs have safe defaults
	return nil
}

// GetConfigSchema returns the configuration schema for UI.
func (p *DebugProcess) GetConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"name": {
				Type:        "string",
				Title:       "Debug Name",
				Description: "Identifier shown in logs to distinguish multiple debug nodes",
				Default:     "Debug",
			},
			"logPayload": {
				Type:        "boolean",
				Title:       "Log Payload",
				Description: "Include the message payload in logs",
				Default:     true,
			},
			"logMeta": {
				Type:        "boolean",
				Title:       "Log Metadata",
				Description: "Include message metadata in logs",
				Default:     true,
			},
			"passThrough": {
				Type:        "boolean",
				Title:       "Pass Through",
				Description: "Forward the message to the next node (disable to stop flow here)",
				Default:     true,
			},
		},
		Required: []string{},
	}
}

// DebugProcessInfo returns the node type info for registration.
func DebugProcessInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type:        "process-debug",
		Name:        "Debug",
		Description: "Inspects and logs messages for debugging",
		Documentation: `## Debug

Inspects messages passing through the flow and logs their content. Similar to console.log() for flows.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| name | string | Debug | Identifier in logs |
| logPayload | boolean | true | Include payload in logs |
| logMeta | boolean | true | Include metadata in logs |
| passThrough | boolean | true | Forward message or stop here |

## Behavior

**Pass-through mode (default):**
- Logs the message
- Forwards it unchanged to the next node
- Adds debug metadata

**Termination mode (passThrough=false):**
- Logs the message
- Stops the flow at this point
- Useful for inspecting without side effects

## Logged Information

- **debugName** - Configured name
- **messageID** - Unique message ID
- **sourceNode** - Originating node
- **sourcePort** - Originating port
- **topic** - Message topic
- **payloadType** - Data type of payload
- **timestamp** - When message was created
- **payload** - Message data (if logPayload=true)
- **meta** - Metadata (if logMeta=true)

## Output

When passThrough=true, adds:

- **meta.debugged** - Always true
- **meta.debugName** - The configured name

## Use Cases

- Inspecting message content during development
- Verifying data transformations
- Debugging flow logic
- Terminating test flows
`,
		Category: types.NodeCategoryProcessing,
		Inputs: []types.Port{
			{
				ID:       "input",
				Name:     "Input",
				DataType: types.DataTypeAny,
				Required: true,
			},
		},
		Outputs: []types.Port{
			{
				ID:       "output",
				Name:     "Output",
				DataType: types.DataTypeAny,
				Multiple: true,
			},
		},
		Icon: "bug",
	}
}

func init() {
	info := DebugProcessInfo()
	if err := node.Register(info, NewDebugProcess); err != nil {
		panic("failed to register debug process: " + err.Error())
	}
}
