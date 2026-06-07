// Package builtin provides the standard set of built-in nodes for PiWorker.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// ManualTrigger is a trigger that fires when explicitly triggered via API.
// It's useful for testing flows and for manual automation triggers.
type ManualTrigger struct {
	*node.BaseNode

	payload     string
	payloadType string // "json" or "text"
	topic       string

	mu      sync.Mutex
	running bool
	out     chan<- *types.Message
	ctx     context.Context
	count   uint64
}

// NewManualTrigger creates a new ManualTrigger with the given configuration.
func NewManualTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	trigger := &ManualTrigger{
		BaseNode:    base,
		payload:     base.GetConfigString("payload", "{}"),
		payloadType: base.GetConfigString("payloadType", "json"),
		topic:       base.GetConfigString("topic", "manual"),
	}

	// Validate payload type
	if trigger.payloadType != "json" && trigger.payloadType != "text" {
		trigger.payloadType = "json"
	}

	// Validate JSON payload if type is json
	if trigger.payloadType == "json" && trigger.payload != "" {
		var js interface{}
		if err := json.Unmarshal([]byte(trigger.payload), &js); err != nil {
			return nil, fmt.Errorf("invalid JSON payload: %w", err)
		}
	}

	return trigger, nil
}

// Process is not used for trigger nodes but must be implemented.
func (t *ManualTrigger) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	// Trigger nodes don't process incoming messages
	return nil, nil
}

// Ports returns the port definitions for this node.
func (t *ManualTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{
		{
			ID:       "output",
			Name:     "Output",
			DataType: types.DataTypeAny,
			Multiple: true,
		},
	}
}

// Validate checks if the configuration is valid.
func (t *ManualTrigger) Validate() error {
	if t.payloadType == "json" && t.payload != "" {
		var js interface{}
		if err := json.Unmarshal([]byte(t.payload), &js); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	}
	return nil
}

// Start prepares the manual trigger to receive inject signals.
func (t *ManualTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("trigger already running")
	}

	t.running = true
	t.out = out
	t.ctx = ctx
	t.count = 0

	return nil
}

// Stop stops the manual trigger.
func (t *ManualTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	t.running = false
	t.out = nil
	t.ctx = nil

	return nil
}

// Inject triggers the node, sending a message with the configured payload.
// This method is called by the API handler when a user clicks "Inject".
// It can optionally receive a custom payload to override the configured one.
func (t *ManualTrigger) Inject(customPayload interface{}) error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return fmt.Errorf("trigger not running")
	}

	out := t.out
	ctx := t.ctx
	t.count++
	count := t.count
	t.mu.Unlock()

	// Determine payload to use
	var payload interface{}
	var dataType types.DataType

	if customPayload != nil {
		payload = customPayload
		dataType = types.DataTypeAny
	} else if t.payloadType == "json" {
		var parsed interface{}
		if err := json.Unmarshal([]byte(t.payload), &parsed); err != nil {
			return fmt.Errorf("failed to parse JSON payload: %w", err)
		}
		payload = parsed
		dataType = types.DataTypeObject
	} else {
		payload = t.payload
		dataType = types.DataTypeString
	}

	// Wrap payload in an object with metadata
	msgPayload := map[string]interface{}{
		"data":      payload,
		"count":     count,
		"timestamp": time.Now().UnixMilli(),
	}

	msg := types.NewMessage(msgPayload, dataType)
	msg.SourcePort = "output"
	msg.Topic = t.topic

	// Send the message
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled")
	case out <- msg:
		return nil
	default:
		return fmt.Errorf("output channel full")
	}
}

// IsRunning returns whether the trigger is currently running.
func (t *ManualTrigger) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

// GetConfigSchema returns the configuration schema for UI.
func (t *ManualTrigger) GetConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"payload": {
				Type:        "string",
				Title:       "Payload",
				Description: "Default payload to send when triggered (JSON or text)",
				Default:     "{}",
			},
			"payloadType": {
				Type:        "string",
				Title:       "Payload Type",
				Description: "Type of the payload",
				Default:     "json",
				Enum:        []string{"json", "text"},
			},
			"topic": {
				Type:        "string",
				Title:       "Topic",
				Description: "Message topic for routing",
				Default:     "manual",
			},
		},
		Required: []string{},
	}
}

// ManualTriggerInfo returns the node type info for registration.
func ManualTriggerInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type:        "trigger-manual",
		Name:        "Manual Inject",
		Description: "Trigger flow manually with a button click",
		Documentation: `## Manual Inject

Triggers the flow manually via a button click or API call. Perfect for testing flows and one-off operations.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| payload | string | {} | Default payload (JSON or text) |
| payloadType | enum | json | Type of payload: "json" or "text" |
| topic | string | manual | Message topic for routing |

## How to Trigger

**From the UI:**
Use the inject button in the flow editor.

**Via API:**
` + "```bash" + `
POST /api/flows/{flowId}/inject/{nodeId}
Content-Type: application/json

{"data": "custom payload"}
` + "```" + `

## Output

Each injection produces:

- **payload.data** - The configured or custom payload
- **payload.count** - Injection count since flow started
- **payload.timestamp** - Unix timestamp of injection
- **topic** - Configured topic (default: "manual")

## Example Output

` + "```json" + `
{
  "payload": {
    "data": {"message": "Hello"},
    "count": 1,
    "timestamp": 1704067200000
  },
  "topic": "manual"
}
` + "```" + `

## Use Cases

- Testing and debugging flows
- Manual data entry triggers
- On-demand operations
- Development and prototyping
`,
		Category: types.NodeCategoryInput,
		Inputs:   nil,
		Outputs: []types.Port{
			{
				ID:       "output",
				Name:     "Output",
				DataType: types.DataTypeAny,
				Multiple: true,
			},
		},
		Icon: "play",
	}
}

func init() {
	// Register the manual trigger with the default registry
	info := ManualTriggerInfo()
	if err := node.Register(info, NewManualTrigger); err != nil {
		panic("failed to register manual trigger: " + err.Error())
	}
}
