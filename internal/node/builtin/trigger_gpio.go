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

// gpioWaitTimeout bounds each WaitForEdge so the goroutine can observe Stop.
const gpioWaitTimeout = time.Second

// GPIOTrigger emits a message when an input pin changes level (edge).
type GPIOTrigger struct {
	*node.BaseNode

	pin  string
	edge string
	pull string

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewGPIOTrigger creates a GPIOTrigger from configuration.
func NewGPIOTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	pin := base.GetConfigString("pin", "")
	if pin == "" {
		return nil, fmt.Errorf("pin is required (e.g. GPIO17)")
	}
	return &GPIOTrigger{
		BaseNode: base,
		pin:      pin,
		edge:     base.GetConfigString("edge", "both"),
		pull:     base.GetConfigString("pull", "none"),
		stopCh:   make(chan struct{}),
	}, nil
}

// Process is unused for trigger nodes.
func (t *GPIOTrigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}

// Start configures the pin as an input and watches for edges.
func (t *GPIOTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	pin, err := gpioPin(t.pin)
	if err != nil {
		return err
	}
	if err := pin.In(parsePull(t.pull), parseEdge(t.edge)); err != nil {
		return fmt.Errorf("failed to configure %s as input: %w", t.pin, err)
	}

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
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Bytes("stack", debug.Stack()).Msg("gpio trigger goroutine panicked")
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.stopCh:
				return
			default:
			}

			if !pin.WaitForEdge(gpioWaitTimeout) {
				continue // timeout: loop to re-check stop/ctx
			}

			msg := types.NewMessage(map[string]interface{}{
				"pin":   t.pin,
				"level": bool(pin.Read()),
				"edge":  t.edge,
			}, types.DataTypeObject)
			msg.SourcePort = "output"
			msg.Topic = "gpio"

			select {
			case <-ctx.Done():
				return
			case <-t.stopCh:
				return
			case out <- msg:
			default:
				metrics.IncrementMessagesDropped()
				log.Warn().Str("pin", t.pin).Msg("gpio edge dropped: output channel full")
			}
		}
	}()
	return nil
}

// Stop stops watching the pin.
func (t *GPIOTrigger) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	close(t.stopCh)
	t.running = false
	t.mu.Unlock()
	t.wg.Wait()
	return nil
}

// Ports returns the port definitions.
func (t *GPIOTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}}
}

// Validate checks the configuration.
func (t *GPIOTrigger) Validate() error {
	if t.pin == "" {
		return fmt.Errorf("pin is required")
	}
	return nil
}

func gpioTriggerConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"pin": {
				Type:        "string",
				Title:       "Pin",
				Description: "GPIO pin name, e.g. GPIO17",
			},
			"edge": {
				Type:        "string",
				Title:       "Edge",
				Description: "Which edge to trigger on",
				Default:     "both",
				Enum:        []string{"rising", "falling", "both"},
			},
			"pull": {
				Type:        "string",
				Title:       "Pull resistor",
				Description: "Internal pull resistor",
				Default:     "none",
				Enum:        []string{"none", "up", "down"},
			},
		},
		Required: []string{"pin", "edge"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (t *GPIOTrigger) GetConfigSchema() node.ConfigSchema {
	return gpioTriggerConfigSchema()
}

// GPIOTriggerInfo returns the node type info for registration.
func GPIOTriggerInfo() node.NodeTypeInfo {
	schema := gpioTriggerConfigSchema()
	return node.NodeTypeInfo{
		Type:        "trigger-gpio",
		Name:        "GPIO Edge",
		Description: "Trigger when a GPIO input pin changes level",
		Documentation: `## GPIO Edge

Watches a GPIO input pin and emits a message on each configured edge. Runs on a
Raspberry Pi (or any host periph.io supports); elsewhere Start fails clearly.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| pin | string | - | Pin name, e.g. GPIO17 |
| edge | enum | both | rising, falling or both |
| pull | enum | none | none, up or down |

## Output payload

` + "`{ pin, level, edge }`" + ` — level is the pin reading at the edge.

## Use Cases

- React to a button press
- Count pulses from a sensor
`,
		Category: types.NodeCategoryInput,
		Inputs:   nil,
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}},
		Config:   &schema,
		Icon:     "toggle-left",
	}
}

func init() {
	info := GPIOTriggerInfo()
	if err := node.Register(info, NewGPIOTrigger); err != nil {
		panic("failed to register gpio trigger: " + err.Error())
	}
}
