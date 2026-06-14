package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog/log"
)

const w1Devices = "/sys/bus/w1/devices"

// DS18B20Trigger polls a 1-Wire DS18B20 temperature sensor and emits readings.
// It reads /sys/bus/w1/devices/<id>/w1_slave directly (no extra dependency), the
// same sysfs approach as trigger-sysmetric.
type DS18B20Trigger struct {
	*node.BaseNode

	deviceID    string
	intervalSec int

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewDS18B20Trigger creates a DS18B20Trigger from configuration.
func NewDS18B20Trigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	interval := base.GetConfigInt("intervalSec", 10)
	if interval < 1 {
		interval = 10
	}
	return &DS18B20Trigger{
		BaseNode:    base,
		deviceID:    base.GetConfigString("deviceId", ""),
		intervalSec: interval,
		stopCh:      make(chan struct{}),
	}, nil
}

// Process is unused for trigger nodes.
func (t *DS18B20Trigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}

// parseW1Slave extracts the temperature (°C) from a w1_slave file body. The first
// line ends with "YES" when the CRC is valid; the second carries "t=<millicelsius>".
func parseW1Slave(content string) (float64, error) {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("malformed w1_slave content")
	}
	if !strings.HasSuffix(strings.TrimSpace(lines[0]), "YES") {
		return 0, fmt.Errorf("w1 CRC check failed")
	}
	idx := strings.LastIndex(lines[1], "t=")
	if idx < 0 {
		return 0, fmt.Errorf("no temperature reading in w1_slave")
	}
	milli, err := strconv.Atoi(strings.TrimSpace(lines[1][idx+2:]))
	if err != nil {
		return 0, fmt.Errorf("invalid temperature value: %w", err)
	}
	return float64(milli) / 1000.0, nil
}

// discoverDevice returns the first 28-* (DS18B20 family) device under w1Devices.
func discoverDevice() (string, error) {
	entries, err := os.ReadDir(w1Devices)
	if err != nil {
		return "", fmt.Errorf("1-wire bus not available: %w", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "28-") {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("no DS18B20 (28-*) device found")
}

func (t *DS18B20Trigger) read() (float64, error) {
	data, err := os.ReadFile(filepath.Join(w1Devices, t.deviceID, "w1_slave"))
	if err != nil {
		return 0, err
	}
	return parseW1Slave(string(data))
}

// Start begins polling the sensor.
func (t *DS18B20Trigger) Start(ctx context.Context, out chan<- *types.Message) error {
	if t.deviceID == "" {
		id, err := discoverDevice()
		if err != nil {
			return err
		}
		t.deviceID = id
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
				log.Error().Interface("panic", r).Bytes("stack", debug.Stack()).Msg("ds18b20 trigger panicked")
			}
		}()

		ticker := time.NewTicker(time.Duration(t.intervalSec) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.stopCh:
				return
			case <-ticker.C:
				celsius, err := t.read()
				if err != nil {
					log.Warn().Err(err).Str("device", t.deviceID).Msg("ds18b20 read failed")
					continue
				}
				msg := types.NewMessage(map[string]interface{}{
					"deviceId": t.deviceID,
					"celsius":  celsius,
				}, types.DataTypeObject)
				msg.SourcePort = "output"
				msg.Topic = "ds18b20"
				select {
				case <-ctx.Done():
					return
				case <-t.stopCh:
					return
				case out <- msg:
				default:
					metrics.IncrementMessagesDropped()
					log.Warn().Str("device", t.deviceID).Msg("ds18b20 reading dropped: output channel full")
				}
			}
		}
	}()
	return nil
}

// Stop stops polling.
func (t *DS18B20Trigger) Stop() error {
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
func (t *DS18B20Trigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}}
}

// Validate checks the configuration.
func (t *DS18B20Trigger) Validate() error {
	if t.intervalSec < 1 {
		return fmt.Errorf("intervalSec must be >= 1")
	}
	return nil
}

func ds18b20TriggerConfigSchema() node.ConfigSchema {
	minInterval := float64(1)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"deviceId": {
				Type:        "string",
				Title:       "Device ID",
				Description: "1-Wire device id (28-xxxx). Empty auto-discovers the first DS18B20.",
			},
			"intervalSec": {
				Type:        "number",
				Title:       "Poll interval (s)",
				Description: "How often to read the sensor, in seconds",
				Default:     10,
				Minimum:     &minInterval,
			},
		},
		Required: []string{"intervalSec"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (t *DS18B20Trigger) GetConfigSchema() node.ConfigSchema {
	return ds18b20TriggerConfigSchema()
}

// DS18B20TriggerInfo returns the node type info for registration.
func DS18B20TriggerInfo() node.NodeTypeInfo {
	schema := ds18b20TriggerConfigSchema()
	return node.NodeTypeInfo{
		Type:        "trigger-ds18b20",
		Name:        "DS18B20 Temperature",
		Description: "Poll a 1-Wire DS18B20 temperature sensor",
		Documentation: `## DS18B20 Temperature

Polls a 1-Wire DS18B20 sensor by reading ` + "`/sys/bus/w1/devices/<id>/w1_slave`" + `
every ` + "`intervalSec`" + ` seconds and emits ` + "`{deviceId, celsius}`" + `. Requires
1-Wire enabled on the Raspberry Pi; on other hosts the bus isn't found and the
node errors clearly.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| deviceId | string | (auto) | 28-xxxx device id; empty discovers the first |
| intervalSec | number | 10 | Poll interval in seconds |
`,
		Category: types.NodeCategoryInput,
		Inputs:   nil,
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}},
		Config:   &schema,
		Icon:     "thermometer",
	}
}

func init() {
	info := DS18B20TriggerInfo()
	if err := node.Register(info, NewDS18B20Trigger); err != nil {
		panic("failed to register ds18b20 trigger: " + err.Error())
	}
}
