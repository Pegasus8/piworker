package builtin

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog/log"
)

// SysMetricTrigger fires when a system metric (CPU load, memory %, temperature)
// crosses a threshold. It edge-triggers: it fires once when the metric enters
// the exceeding state, not on every poll.
type SysMetricTrigger struct {
	*node.BaseNode

	metric     string // "cpu", "memory", "temperature"
	threshold  float64
	comparison string // "above", "below"
	interval   time.Duration

	run triggerRun
}

// NewSysMetricTrigger creates a new SysMetricTrigger from configuration.
func NewSysMetricTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	metric := base.GetConfigString("metric", "cpu")
	switch metric {
	case "cpu", "memory", "temperature":
	default:
		metric = "cpu"
	}

	comparison := base.GetConfigString("comparison", "above")
	if comparison != "above" && comparison != "below" {
		comparison = "above"
	}

	interval := secDuration(base.GetConfigInt("interval", 10), time.Second, time.Second, maxTriggerInterval)

	return &SysMetricTrigger{
		BaseNode:   base,
		metric:     metric,
		threshold:  base.GetConfigFloat("threshold", 80),
		comparison: comparison,
		interval:   interval,
	}, nil
}

// Process is unused for trigger nodes but must be implemented.
func (t *SysMetricTrigger) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	return nil, nil
}

// Ports returns the port definitions.
func (t *SysMetricTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}}
}

// Validate checks the configuration.
func (t *SysMetricTrigger) Validate() error {
	if t.interval < time.Second {
		return fmt.Errorf("interval must be at least 1 second")
	}
	return nil
}

// Start begins polling the metric.
func (t *SysMetricTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	stopCh, done, err := t.run.begin()
	if err != nil {
		return err
	}

	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Bytes("stack", debug.Stack()).Msg("sysmetric trigger goroutine panicked")
			}
		}()
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()

		exceeded := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				value, err := readSystemMetric(t.metric)
				if err != nil {
					log.Warn().Err(err).Str("metric", t.metric).Msg("sysmetric read failed")
					continue
				}

				nowExceeded := t.compare(value)
				if nowExceeded && !exceeded {
					msg := types.NewMessage(map[string]interface{}{
						"metric":     t.metric,
						"value":      value,
						"threshold":  t.threshold,
						"comparison": t.comparison,
						"timestamp":  time.Now().UnixMilli(),
					}, types.DataTypeObject)
					msg.SourcePort = "output"
					msg.Topic = "sysmetric"

					select {
					case <-ctx.Done():
						return
					case <-stopCh:
						return
					case out <- msg:
					default:
						metrics.IncrementMessagesDropped()
						log.Warn().Str("metric", t.metric).Msg("sysmetric message dropped: channel full")
					}
				}
				exceeded = nowExceeded
			}
		}
	}()

	return nil
}

// Stop stops polling.
func (t *SysMetricTrigger) Stop() error {
	t.run.end()
	return nil
}

func (t *SysMetricTrigger) compare(value float64) bool {
	if t.comparison == "below" {
		return value < t.threshold
	}
	return value > t.threshold
}

// readSystemMetric reads the requested metric from the OS. It targets Linux
// (/proc, /sys) and returns an error on platforms where the source is absent.
func readSystemMetric(metric string) (float64, error) {
	switch metric {
	case "cpu":
		return readLoadAvg()
	case "memory":
		return readMemoryUsedPercent()
	case "temperature":
		return readCPUTemperature()
	default:
		return 0, fmt.Errorf("unknown metric %q", metric)
	}
}

// readLoadAvg returns the 1-minute load average from /proc/loadavg.
func readLoadAvg() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/loadavg format")
	}
	return strconv.ParseFloat(fields[0], 64)
}

// readMemoryUsedPercent returns used memory as a percentage from /proc/meminfo.
func readMemoryUsedPercent() (float64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	var total, available float64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, _ = strconv.ParseFloat(fields[1], 64)
		case "MemAvailable:":
			available, _ = strconv.ParseFloat(fields[1], 64)
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("could not read MemTotal")
	}
	return (total - available) / total * 100, nil
}

// readCPUTemperature returns the CPU temperature in °C from the thermal zone.
func readCPUTemperature() (float64, error) {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0, err
	}
	milli, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0, err
	}
	return milli / 1000, nil
}

func sysMetricConfigSchema() node.ConfigSchema {
	min := float64(1)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"metric": {
				Type:         "string",
				Title:        "Metric",
				Description:  "System metric to watch",
				Default:      "cpu",
				Enum:         []string{"cpu", "memory", "temperature"},
				AgentVisible: true,
			},
			"comparison": {
				Type:         "string",
				Title:        "Comparison",
				Description:  "Fire when the value is above or below the threshold",
				Default:      "above",
				Enum:         []string{"above", "below"},
				AgentVisible: true,
			},
			"threshold": {
				Type:         "number",
				Title:        "Threshold",
				Description:  "CPU load, memory % (0-100), or temperature in °C",
				Default:      80,
				AgentVisible: true,
			},
			"interval": {
				Type:         "number",
				Title:        "Poll Interval (s)",
				Description:  "How often to sample the metric, in seconds",
				Default:      10,
				Minimum:      &min,
				Unit:         "seconds",
				AgentVisible: true,
			},
		},
		Required: []string{"metric", "threshold"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (t *SysMetricTrigger) GetConfigSchema() node.ConfigSchema {
	return sysMetricConfigSchema()
}

// SysMetricTriggerInfo returns the node type info for registration.
func SysMetricTriggerInfo() node.NodeTypeInfo {
	schema := sysMetricConfigSchema()
	return node.NodeTypeInfo{
		Type:        "trigger-sysmetric",
		Name:        "System Metric",
		Description: "Triggers when CPU, memory, or temperature crosses a threshold",
		Documentation: `## System Metric

Polls a system metric and fires once when it crosses the configured threshold
(edge-triggered, not on every sample). Reads from ` + "`/proc`" + ` and ` + "`/sys`" + `, so it
targets Linux / Raspberry Pi.

## Metrics

| Metric | Source | Unit |
|--------|--------|------|
| cpu | /proc/loadavg | 1-min load average |
| memory | /proc/meminfo | used percent (0-100) |
| temperature | /sys/class/thermal/thermal_zone0/temp | °C |

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| metric | enum | cpu | Metric to watch |
| comparison | enum | above | Fire when above/below the threshold |
| threshold | number | 80 | Threshold value |
| interval | number | 10 | Poll interval in seconds |

## Use Cases

- Alert when the Pi overheats
- Throttle work when load is high
- Notify on low free memory
`,
		Category: types.NodeCategoryInput,
		Risk:     node.RiskSafe,
		Inputs:   nil,
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}},
		Config:   &schema,
		Icon:     "activity",
	}
}

func init() {
	info := SysMetricTriggerInfo()
	if err := node.Register(info, NewSysMetricTrigger); err != nil {
		panic("failed to register sysmetric trigger: " + err.Error())
	}
}
