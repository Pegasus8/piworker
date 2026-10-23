package builtin

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
)

// i2cBusCache caches one open bus per name (a bus is shared, not per-message).
var (
	i2cBusMu    sync.Mutex
	i2cBusCache = map[string]i2c.Bus{}
)

// i2cOpen returns a shared I2C bus, initializing the periph host on first use
// via periphInit (which registers every bus driver, including I2C).
func i2cOpen(name string) (i2c.Bus, error) {
	if err := periphInit(); err != nil {
		return nil, fmt.Errorf("I2C host init failed: %w", err)
	}
	i2cBusMu.Lock()
	defer i2cBusMu.Unlock()
	if b, ok := i2cBusCache[name]; ok {
		return b, nil
	}
	b, err := i2creg.Open(name)
	if err != nil {
		return nil, err
	}
	i2cBusCache[name] = b
	return b, nil
}

// I2CAction reads bytes from an I2C device, optionally after writing a register
// address. Works on a Raspberry Pi (or any host periph.io supports).
type I2CAction struct {
	*node.BaseNode

	busName     string
	address     uint16
	register    byte
	hasRegister bool
	length      int
}

// NewI2CAction creates an I2CAction from configuration.
func NewI2CAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	addrStr := strings.TrimSpace(base.GetConfigString("address", ""))
	if addrStr == "" {
		return nil, fmt.Errorf("address is required (e.g. 0x76)")
	}
	addr, err := strconv.ParseUint(addrStr, 0, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", addrStr, err)
	}

	a := &I2CAction{
		BaseNode: base,
		busName:  base.GetConfigString("bus", ""),
		address:  uint16(addr),
		length:   base.GetConfigInt("length", 1),
	}
	if a.length < 1 {
		a.length = 1
	}

	if regStr := strings.TrimSpace(base.GetConfigString("register", "")); regStr != "" {
		reg, err := strconv.ParseUint(regStr, 0, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid register %q: %w", regStr, err)
		}
		a.register = byte(reg)
		a.hasRegister = true
	}
	return a, nil
}

// Process reads from the device and returns the bytes.
func (a *I2CAction) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	bus, err := i2cOpen(a.busName)
	if err != nil {
		return nil, node.Transient(fmt.Errorf("failed to open I2C bus: %w", err))
	}
	dev := &i2c.Dev{Bus: bus, Addr: a.address}

	var write []byte
	if a.hasRegister {
		write = []byte{a.register}
	}
	read := make([]byte, a.length)
	if err := dev.Tx(write, read); err != nil {
		return nil, node.Transient(fmt.Errorf("I2C transaction failed: %w", err))
	}

	ints := make([]int, len(read))
	for i, b := range read {
		ints[i] = int(b)
	}

	out := msg.Clone()
	out.Payload = map[string]interface{}{"bytes": ints, "hex": hex.EncodeToString(read)}
	out.PayloadType = types.DataTypeObject
	out.SourcePort = "output"
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (a *I2CAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}}
}

// Validate checks the configuration.
func (a *I2CAction) Validate() error {
	if a.length < 1 {
		return fmt.Errorf("length must be >= 1")
	}
	return nil
}

func i2cActionConfigSchema() node.ConfigSchema {
	minOne := float64(1)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"bus":      {Type: "string", Title: "Bus", Description: "I2C bus name; empty uses the first bus"},
			"address":  {Type: "string", Title: "Address", Description: "Device address, e.g. 0x76"},
			"register": {Type: "string", Title: "Register", Description: "Optional register to read from, e.g. 0xD0"},
			"length":   {Type: "number", Title: "Length", Description: "Number of bytes to read", Default: 1, Minimum: &minOne},
		},
		Required: []string{"address", "length"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (a *I2CAction) GetConfigSchema() node.ConfigSchema {
	return i2cActionConfigSchema()
}

// I2CActionInfo returns the node type info for registration.
func I2CActionInfo() node.NodeTypeInfo {
	schema := i2cActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-i2c",
		Name:        "I2C Read",
		Description: "Read bytes from an I2C device",
		Documentation: `## I2C Read

Reads ` + "`length`" + ` bytes from an I2C device, optionally after writing a
` + "`register`" + ` address first. Runs on a Raspberry Pi (or any host periph.io
supports); elsewhere the bus isn't found and the node errors clearly.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| bus | string | (first) | I2C bus name |
| address | string | - | Device address (e.g. 0x76) |
| register | string | - | Optional register to read from (e.g. 0xD0) |
| length | number | 1 | Bytes to read |

## Output payload

` + "`{ bytes: [...], hex: \"...\" }`" + `

## Use Cases

- Read a sensor's chip id or raw registers
- Poll a custom I2C peripheral
`,
		Category: types.NodeCategoryOutput,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}},
		Config:   &schema,
		Icon:     "cpu",
	}
}

func init() {
	info := I2CActionInfo()
	if err := node.Register(info, NewI2CAction); err != nil {
		panic("failed to register i2c action: " + err.Error())
	}
}
