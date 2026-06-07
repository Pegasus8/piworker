// Package builtin provides the standard set of built-in nodes for PiWorker.
package builtin

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog/log"
)

// IntervalTrigger fires messages at regular intervals.
type IntervalTrigger struct {
	*node.BaseNode

	interval time.Duration
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	count    uint64
	wg       sync.WaitGroup
}

// NewIntervalTrigger creates a new IntervalTrigger with the given configuration.
func NewIntervalTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	trigger := &IntervalTrigger{
		BaseNode: base,
		stopCh:   make(chan struct{}),
	}

	// Parse interval from config
	intervalMs := base.GetConfigInt("interval", 1000)
	if intervalMs < 100 {
		log.Warn().Int("requested", intervalMs).Msg("interval below 100ms minimum; clamping to 100ms")
		intervalMs = 100 // Minimum 100ms to prevent excessive triggering
	}
	trigger.interval = time.Duration(intervalMs) * time.Millisecond

	return trigger, nil
}

// Process is not used for trigger nodes but must be implemented.
func (t *IntervalTrigger) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	// Trigger nodes don't process incoming messages
	return nil, nil
}

// Ports returns the port definitions for this node.
func (t *IntervalTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{
		{
			ID:       "output",
			Name:     "Output",
			DataType: types.DataTypeObject,
			Multiple: true,
		},
	}
}

// Validate checks if the configuration is valid.
func (t *IntervalTrigger) Validate() error {
	if t.interval < 100*time.Millisecond {
		return fmt.Errorf("interval must be at least 100ms")
	}
	return nil
}

// Start begins the interval trigger.
func (t *IntervalTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("trigger already running")
	}
	t.running = true
	t.stopCh = make(chan struct{})
	t.mu.Unlock()

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()

		// The router no longer closes the output channel under the sender, so a
		// panic here means a genuine bug, not a benign closed-channel send. Log
		// it (with a stack trace) instead of swallowing it silently, otherwise
		// the trigger would die invisibly and simply stop firing forever.
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Interface("panic", r).
					Bytes("stack", debug.Stack()).
					Msg("interval trigger goroutine panicked")
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.stopCh:
				return
			case tickTime := <-ticker.C:
				t.mu.Lock()
				t.count++
				count := t.count
				t.mu.Unlock()

				msg := types.NewMessage(map[string]interface{}{
					"timestamp": tickTime.UnixMilli(),
					"count":     count,
				}, types.DataTypeObject)
				msg.SourcePort = "output"
				msg.Topic = "interval"

				// Check for stop/cancel before sending. A full channel applies
				// backpressure; rather than lose the tick silently we record a
				// drop metric and warn so the loss is observable.
				select {
				case <-ctx.Done():
					return
				case <-t.stopCh:
					return
				case out <- msg:
				default:
					metrics.IncrementMessagesDropped()
					log.Warn().Uint64("count", count).Msg("interval tick dropped: output channel full")
				}
			}
		}
	}()

	return nil
}

// Stop stops the interval trigger.
func (t *IntervalTrigger) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}

	close(t.stopCh)
	t.running = false
	t.mu.Unlock()

	// Wait for goroutine to exit (outside the lock to avoid deadlock)
	t.wg.Wait()
	return nil
}

// GetConfigSchema returns the configuration schema for UI.
func (t *IntervalTrigger) GetConfigSchema() node.ConfigSchema {
	min := float64(100)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"interval": {
				Type:        "number",
				Title:       "Interval (ms)",
				Description: "Time between triggers in milliseconds",
				Default:     1000,
				Minimum:     &min,
			},
		},
		Required: []string{"interval"},
	}
}

// IntervalTriggerInfo returns the node type info for registration.
func IntervalTriggerInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type:        "trigger-interval",
		Name:        "Interval Timer",
		Description: "Triggers at regular time intervals",
		Documentation: `## Interval Timer

Fires messages at regular time intervals. Useful for periodic tasks, polling, or scheduled operations.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| interval | number | 1000 | Time between triggers in milliseconds (min: 100ms) |

## Output

Each tick produces a message with:

- **payload.timestamp** - Unix timestamp in milliseconds when the tick occurred
- **payload.count** - Number of ticks since the trigger started (1, 2, 3, ...)
- **topic** - Always set to "interval"

## Example Output

` + "```json" + `
{
  "payload": {
    "timestamp": 1704067200000,
    "count": 42
  },
  "topic": "interval"
}
` + "```" + `

## Use Cases

- Periodic data polling
- Heartbeat signals
- Scheduled cleanup tasks
- Rate-limited operations
`,
		Category: types.NodeCategoryInput,
		Inputs:   nil,
		Outputs: []types.Port{
			{
				ID:       "output",
				Name:     "Output",
				DataType: types.DataTypeObject,
				Multiple: true,
			},
		},
		Icon: "timer",
	}
}

func init() {
	// Register the interval trigger with the default registry
	info := IntervalTriggerInfo()
	if err := node.Register(info, NewIntervalTrigger); err != nil {
		panic("failed to register interval trigger: " + err.Error())
	}
}
