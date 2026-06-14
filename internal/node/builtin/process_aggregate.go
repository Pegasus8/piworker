package builtin

import (
	"context"
	"fmt"
	"sync"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// AggregateProcess batches input payloads and emits them as a single array
// message once `count` have accumulated. Because a processing node can only emit
// by returning from Process, batching is count-based (not time-windowed).
type AggregateProcess struct {
	*node.BaseNode

	count int

	mu     sync.Mutex
	buffer []interface{}
}

// NewAggregateProcess creates an AggregateProcess from configuration.
func NewAggregateProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	count := base.GetConfigInt("count", 10)
	if count < 1 {
		count = 1
	}
	return &AggregateProcess{BaseNode: base, count: count}, nil
}

// Process buffers the payload and emits the batch once it is full.
func (p *AggregateProcess) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.buffer = append(p.buffer, msg.Payload)
	if len(p.buffer) < p.count {
		return nil, nil // still filling the batch
	}

	batch := p.buffer
	p.buffer = nil

	out := msg.Clone()
	out.Payload = batch
	out.PayloadType = types.DataTypeArray
	out.SourcePort = "output"
	out.SetMeta("aggregated", len(batch))
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (p *AggregateProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeArray, Multiple: true}}
}

// Validate checks the configuration.
func (p *AggregateProcess) Validate() error {
	if p.count < 1 {
		return fmt.Errorf("count must be >= 1")
	}
	return nil
}

func aggregateProcessConfigSchema() node.ConfigSchema {
	minOne := 1.0
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"count": {
				Type:        "number",
				Title:       "Batch size",
				Description: "Number of messages to collect before emitting them as one array",
				Default:     10,
				Minimum:     &minOne,
			},
		},
		Required: []string{"count"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (p *AggregateProcess) GetConfigSchema() node.ConfigSchema {
	return aggregateProcessConfigSchema()
}

// AggregateProcessInfo returns the node type info for registration.
func AggregateProcessInfo() node.NodeTypeInfo {
	schema := aggregateProcessConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-aggregate",
		Name:        "Aggregate",
		Description: "Collect N messages and emit them together as one array",
		Documentation: `## Aggregate

Buffers incoming message payloads and emits them as a single array once
` + "`count`" + ` have been collected, then starts a fresh batch.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| count | number | 10 | Messages to collect per batch |

## Notes

Batching is **count-based**: a processing node emits only by returning from
Process, so there is no time-window flush. Pair with an upstream trigger that
fires predictably, or keep the count small.

## Use Cases

- Bulk-insert N readings at once
- Send one digest notification per N events
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeArray, Multiple: true}},
		Config:   &schema,
		Icon:     "layers",
	}
}

func init() {
	info := AggregateProcessInfo()
	if err := node.Register(info, NewAggregateProcess); err != nil {
		panic("failed to register aggregate process: " + err.Error())
	}
}
