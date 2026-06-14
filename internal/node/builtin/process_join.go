package builtin

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// JoinProcess correlates messages arriving on its single input and emits a merged
// message. Because a node can't tell which input port a message arrived on, it
// correlates by a key (an expression over payload/meta) and either:
//   - mode "key":    buffers payloads sharing a key, emits an array at `count`.
//   - mode "source": buffers one payload per configured source node, emits an
//     object {sourceNode: payload} once all sources have arrived for a key.
//
// Stateful; the runtime's serial per-node executor lets the mutex stay simple.
type JoinProcess struct {
	*node.BaseNode

	mode    string
	keyProg *vm.Program
	count   int
	sources []string

	mu       sync.Mutex
	byKey    map[string][]interface{}
	bySource map[string]map[string]interface{}
}

// NewJoinProcess creates a JoinProcess from configuration.
func NewJoinProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	mode := strings.ToLower(base.GetConfigString("mode", "key"))
	if mode != "key" && mode != "source" {
		return nil, fmt.Errorf("invalid mode %q (use key or source)", mode)
	}

	keySrc := base.GetConfigString("key", "meta.correlationID")
	keyProg, err := expr.Compile(keySrc,
		expr.AllowUndefinedVariables(),
		expr.MaxNodes(DefaultExprMaxNodes),
		expr.WithContext("ctx"),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid key expression: %w", err)
	}

	j := &JoinProcess{
		BaseNode: base,
		mode:     mode,
		keyProg:  keyProg,
		byKey:    make(map[string][]interface{}),
		bySource: make(map[string]map[string]interface{}),
	}

	if mode == "key" {
		j.count = base.GetConfigInt("count", 2)
		if j.count < 1 {
			j.count = 1
		}
	} else {
		j.sources = splitRecipients(base.GetConfigString("sources", ""))
		if len(j.sources) == 0 {
			return nil, fmt.Errorf("source mode requires a non-empty sources list")
		}
	}
	return j, nil
}

// Process buffers the message by its correlation key and emits when complete.
func (j *JoinProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	keyVal, err := evalExpr(ctx, j.keyProg, msg)
	if err != nil {
		return nil, fmt.Errorf("join key evaluation failed: %w", err)
	}
	key := tmplStringify(keyVal)

	j.mu.Lock()
	defer j.mu.Unlock()

	if j.mode == "key" {
		j.byKey[key] = append(j.byKey[key], msg.Payload)
		if len(j.byKey[key]) < j.count {
			return nil, nil
		}
		batch := j.byKey[key]
		delete(j.byKey, key)
		return []*types.Message{j.emit(msg, batch, types.DataTypeArray)}, nil
	}

	// mode == "source"
	if !slices.Contains(j.sources, msg.SourceNode) {
		return nil, nil // not one of the awaited sources
	}
	bucket := j.bySource[key]
	if bucket == nil {
		bucket = make(map[string]interface{})
		j.bySource[key] = bucket
	}
	bucket[msg.SourceNode] = msg.Payload
	for _, s := range j.sources {
		if _, ok := bucket[s]; !ok {
			return nil, nil // still waiting for some source
		}
	}
	delete(j.bySource, key)
	return []*types.Message{j.emit(msg, bucket, types.DataTypeObject)}, nil
}

func (j *JoinProcess) emit(msg *types.Message, payload interface{}, dt types.DataType) *types.Message {
	out := msg.Clone()
	out.Payload = payload
	out.PayloadType = dt
	out.SourcePort = "output"
	out.SetMeta("joined", j.mode)
	return out
}

// Ports returns the port definitions.
func (j *JoinProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (j *JoinProcess) Validate() error {
	if j.mode != "key" && j.mode != "source" {
		return fmt.Errorf("invalid mode %q", j.mode)
	}
	if j.mode == "source" && len(j.sources) == 0 {
		return fmt.Errorf("source mode requires sources")
	}
	return nil
}

func joinProcessConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"mode": {
				Type:        "string",
				Title:       "Mode",
				Description: "key: collect N by a key; source: one per source node",
				Default:     "key",
				Enum:        []string{"key", "source"},
			},
			"key": {
				Type:        "string",
				Title:       "Correlation key",
				Description: "Expression for the grouping key (e.g. payload.orderId or meta.correlationID)",
				Default:     "meta.correlationID",
			},
			"count": {
				Type:        "number",
				Title:       "Count (key mode)",
				Description: "Emit an array once this many messages share a key",
				Default:     2,
			},
			"sources": {
				Type:        "string",
				Title:       "Sources (source mode)",
				Description: "Comma-separated source node IDs to await before emitting an object",
			},
		},
		Required: []string{"mode", "key"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (j *JoinProcess) GetConfigSchema() node.ConfigSchema {
	return joinProcessConfigSchema()
}

// JoinProcessInfo returns the node type info for registration.
func JoinProcessInfo() node.NodeTypeInfo {
	schema := joinProcessConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-join",
		Name:        "Join",
		Description: "Correlate and merge messages by a key or by source node",
		Documentation: `## Join

Merges messages that arrive on its single input. A node can't tell which input
port a message came in on, so correlation is by a **key** expression.

## Modes

- **key**: buffer payloads sharing the key; emit an array once ` + "`count`" + ` collected.
- **source**: buffer one payload per node in ` + "`sources`" + `; emit an object
  ` + "`{sourceNode: payload}`" + ` once all sources have arrived for a key.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| mode | enum | key | key or source |
| key | string | meta.correlationID | Expression for the grouping key |
| count | number | 2 | key mode: messages per emit |
| sources | string | - | source mode: CSV of source node IDs |

## Use Cases

- Pair a request with its later response by an id (key)
- Combine readings from several sensor branches into one record (source)
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "merge",
	}
}

func init() {
	info := JoinProcessInfo()
	if err := node.Register(info, NewJoinProcess); err != nil {
		panic("failed to register join process: " + err.Error())
	}
}
