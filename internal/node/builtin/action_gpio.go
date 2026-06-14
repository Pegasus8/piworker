package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"periph.io/x/conn/v3/gpio"
)

// GPIOAction drives a GPIO output pin high or low, either to a fixed level or
// from the message payload's truthiness.
type GPIOAction struct {
	*node.BaseNode

	pin   string
	value string // "high" | "low" | "payload"
}

// NewGPIOAction creates a GPIOAction from configuration.
func NewGPIOAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	pin := base.GetConfigString("pin", "")
	if pin == "" {
		return nil, fmt.Errorf("pin is required (e.g. GPIO17)")
	}
	return &GPIOAction{
		BaseNode: base,
		pin:      pin,
		value:    strings.ToLower(base.GetConfigString("value", "high")),
	}, nil
}

// Process sets the pin level.
func (a *GPIOAction) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	pin, err := gpioPin(a.pin)
	if err != nil {
		return nil, err
	}

	level := parseLevel(a.value)
	if a.value == "payload" {
		level = gpio.Low
		if truthy(msg.Payload) {
			level = gpio.High
		}
	}

	if err := pin.Out(level); err != nil {
		return nil, fmt.Errorf("failed to set %s: %w", a.pin, err)
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("gpioPin", a.pin)
	out.SetMeta("gpioLevel", bool(level))
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (a *GPIOAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (a *GPIOAction) Validate() error {
	if a.pin == "" {
		return fmt.Errorf("pin is required")
	}
	return nil
}

func gpioActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"pin": {
				Type:        "string",
				Title:       "Pin",
				Description: "GPIO pin name, e.g. GPIO17",
			},
			"value": {
				Type:        "string",
				Title:       "Value",
				Description: "high, low, or payload (use the message payload's truthiness)",
				Default:     "high",
				Enum:        []string{"high", "low", "payload"},
			},
		},
		Required: []string{"pin", "value"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (a *GPIOAction) GetConfigSchema() node.ConfigSchema {
	return gpioActionConfigSchema()
}

// GPIOActionInfo returns the node type info for registration.
func GPIOActionInfo() node.NodeTypeInfo {
	schema := gpioActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-gpio",
		Name:        "GPIO Write",
		Description: "Set a GPIO output pin high or low",
		Documentation: `## GPIO Write

Drives a GPIO output pin. Runs on a Raspberry Pi (or any host periph.io
supports); on other hosts the pin is not found and the node errors clearly.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| pin | string | - | Pin name, e.g. GPIO17 |
| value | enum | high | high, low, or payload |

When ` + "`value`" + ` is ` + "`payload`" + `, the pin follows the message payload's
truthiness (e.g. true/1/"on" → high).

## Use Cases

- Turn a relay or LED on/off
- Drive a buzzer from a condition
`,
		Category: types.NodeCategoryOutput,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "toggle-right",
	}
}

func init() {
	info := GPIOActionInfo()
	if err := node.Register(info, NewGPIOAction); err != nil {
		panic("failed to register gpio action: " + err.Error())
	}
}
