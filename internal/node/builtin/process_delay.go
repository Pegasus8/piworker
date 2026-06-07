package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// DelayProcess delays messages by a configured amount of time.
type DelayProcess struct {
	*node.BaseNode

	delay time.Duration
}

// NewDelayProcess creates a new DelayProcess with the given configuration.
func NewDelayProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	// Parse delay in milliseconds
	delayMs := base.GetConfigInt("delay", 1000)
	if delayMs < 0 {
		delayMs = 0
	}
	if delayMs > 300000 { // Max 5 minutes
		delayMs = 300000
	}

	process := &DelayProcess{
		BaseNode: base,
		delay:    time.Duration(delayMs) * time.Millisecond,
	}

	return process, nil
}

// Process delays the message before passing it through.
func (p *DelayProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	if p.delay > 0 {
		// Create a timer for the delay
		timer := time.NewTimer(p.delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			// Context cancelled, abort
			return nil, ctx.Err()
		case <-timer.C:
			// Delay completed
		}
	}

	// Add delay info to metadata
	outputMsg := msg.Clone()
	outputMsg.SourcePort = "output"
	outputMsg.SetMeta("delayed", true)
	outputMsg.SetMeta("delayMs", p.delay.Milliseconds())

	return []*types.Message{outputMsg}, nil
}

// Ports returns the port definitions for this node.
func (p *DelayProcess) Ports() (inputs []types.Port, outputs []types.Port) {
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
func (p *DelayProcess) Validate() error {
	if p.delay < 0 {
		return fmt.Errorf("delay cannot be negative")
	}
	if p.delay > 5*time.Minute {
		return fmt.Errorf("delay cannot exceed 5 minutes")
	}
	return nil
}

// GetConfigSchema returns the configuration schema for UI.
func (p *DelayProcess) GetConfigSchema() node.ConfigSchema {
	minDelay := float64(0)
	maxDelay := float64(300000)

	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"delay": {
				Type:        "number",
				Title:       "Delay (ms)",
				Description: "Time to delay in milliseconds (max 5 minutes)",
				Default:     1000,
				Minimum:     &minDelay,
				Maximum:     &maxDelay,
			},
		},
		Required: []string{"delay"},
	}
}

// DelayProcessInfo returns the node type info for registration.
func DelayProcessInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type:        "process-delay",
		Name:        "Delay",
		Description: "Delays messages by a specified time",
		Documentation: `## Delay

Pauses message flow for a specified duration. The message passes through unchanged after the delay.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| delay | number | 1000 | Delay time in milliseconds (0-300000) |

## Behavior

- Message is held for the configured duration
- Payload passes through unchanged
- Metadata is preserved
- Respects flow cancellation (stops waiting if flow stops)
- Maximum delay: 5 minutes (300000ms)

## Output

The original message plus:

- **meta.delayed** - Always true
- **meta.delayMs** - Actual delay applied

## Use Cases

- Rate limiting outbound requests
- Debouncing rapid events
- Waiting for external systems
- Simulating real-world delays in testing
- Throttling API calls

## Example

` + "```yaml" + `
delay: 5000  # Wait 5 seconds
` + "```" + `

## Notes

- A delay no longer stalls the rest of the flow: each node runs in its own
  executor, so other nodes keep processing while a message waits here.
- Messages arriving at the *same* delay node are processed in order, so a burst
  is delayed sequentially rather than concurrently.
- If the flow is stopped during a delay, the message is discarded.
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
		Icon: "hourglass",
	}
}

func init() {
	info := DelayProcessInfo()
	if err := node.Register(info, NewDelayProcess); err != nil {
		panic("failed to register delay process: " + err.Error())
	}
}
