package builtin

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/expr-lang/expr/vm"
	"github.com/rs/zerolog/log"
)

// JoinProcess correlates messages arriving on its single input and emits a merged
// message. Because a node can't tell which input port a message arrived on, it
// correlates by a key (an expression over payload/meta) and either:
//   - mode "key":    buffers payloads sharing a key, emits an array at `count`.
//   - mode "source": buffers one payload per configured source node, emits an
//     object {sourceNode: payload} once all sources have arrived for a key.
//
// Stateful; the runtime's serial per-node executor lets the mutex stay simple.
// Pending groups are bounded: a group not completed within `timeout` is
// dropped, and at most `maxKeys` groups are kept (oldest evicted first), so
// keys that never complete can't grow memory without limit.
type JoinProcess struct {
	*node.BaseNode

	mode    string
	keyProg *vm.Program
	count   int
	sources []string
	timeout time.Duration
	maxKeys int

	mu     sync.Mutex
	groups map[string]*joinGroup
	now    func() time.Time
}

// joinGroup is one pending correlation group.
type joinGroup struct {
	created  time.Time
	items    []interface{}          // key mode
	bySource map[string]interface{} // source mode
}

const (
	defaultJoinTimeout = 5 * time.Minute
	defaultJoinMaxKeys = 1000
)

// NewJoinProcess creates a JoinProcess from configuration.
func NewJoinProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	mode := strings.ToLower(base.GetConfigString("mode", "key"))
	if mode != "key" && mode != "source" {
		return nil, fmt.Errorf("invalid mode %q (use key or source)", mode)
	}

	keySrc := base.GetConfigString("key", "meta.correlationID")
	keyProg, err := compileExpr(keySrc)
	if err != nil {
		return nil, fmt.Errorf("invalid key expression: %w", err)
	}

	timeoutMs := base.GetConfigInt("timeoutMs", int(defaultJoinTimeout/time.Millisecond))
	timeout := msDuration(timeoutMs, defaultJoinTimeout, time.Second, 7*24*time.Hour)
	maxKeys := base.GetConfigInt("maxKeys", defaultJoinMaxKeys)
	if maxKeys <= 0 {
		maxKeys = defaultJoinMaxKeys
	}

	j := &JoinProcess{
		BaseNode: base,
		mode:     mode,
		keyProg:  keyProg,
		timeout:  timeout,
		maxKeys:  maxKeys,
		groups:   make(map[string]*joinGroup),
		now:      time.Now,
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

	if j.mode == "source" && !slices.Contains(j.sources, msg.SourceNode) {
		return nil, nil // not one of the awaited sources
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	now := j.now()
	j.evictExpired(now)

	g := j.groups[key]
	if g == nil {
		if len(j.groups) >= j.maxKeys {
			j.evictOldest()
		}
		g = &joinGroup{created: now}
		j.groups[key] = g
	}

	if j.mode == "key" {
		g.items = append(g.items, msg.Payload)
		if len(g.items) < j.count {
			return nil, nil
		}
		delete(j.groups, key)
		return []*types.Message{j.emit(msg, g.items, types.DataTypeArray)}, nil
	}

	// mode == "source"
	if g.bySource == nil {
		g.bySource = make(map[string]interface{})
	}
	g.bySource[msg.SourceNode] = msg.Payload
	for _, s := range j.sources {
		if _, ok := g.bySource[s]; !ok {
			return nil, nil // still waiting for some source
		}
	}
	delete(j.groups, key)
	return []*types.Message{j.emit(msg, g.bySource, types.DataTypeObject)}, nil
}

// evictExpired drops groups older than the timeout. Called with j.mu held.
func (j *JoinProcess) evictExpired(now time.Time) {
	for k, g := range j.groups {
		if now.Sub(g.created) > j.timeout {
			delete(j.groups, k)
			log.Debug().Str("key", k).Msg("join: dropped incomplete group after timeout")
		}
	}
}

// evictOldest drops the oldest pending group. Called with j.mu held.
func (j *JoinProcess) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for k, g := range j.groups {
		if oldestKey == "" || g.created.Before(oldest) {
			oldestKey, oldest = k, g.created
		}
	}
	delete(j.groups, oldestKey)
	log.Warn().Str("key", oldestKey).Int("maxKeys", j.maxKeys).Msg("join: too many pending groups; dropped the oldest")
}

// pending returns the number of incomplete groups held.
func (j *JoinProcess) pending() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.groups)
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
				Type:            "string",
				Title:           "Mode",
				Description:     "key: collect N by a key; source: one per source node",
				Default:         "key",
				Enum:            []string{"key", "source"},
				CaseInsensitive: true,
				AgentVisible:    true,
			},
			"key": {
				Type:        "string",
				Title:       "Correlation key",
				Description: "Expression for the grouping key (e.g. payload.orderId or meta.correlationID)",
				Default:     "meta.correlationID",
			},
			"count": {
				Type:         "number",
				Title:        "Count (key mode)",
				Description:  "Emit an array once this many messages share a key",
				Default:      2,
				AgentVisible: true,
			},
			"sources": {
				Type:         "string",
				Title:        "Sources (source mode)",
				Description:  "Comma-separated source node IDs to await before emitting an object",
				AgentVisible: true,
			},
			"timeoutMs": {
				Type:         "number",
				Title:        "Timeout (ms)",
				Description:  "Drop an incomplete group this long after its first message, in milliseconds",
				Default:      int(defaultJoinTimeout / time.Millisecond),
				Unit:         "milliseconds",
				AgentVisible: true,
			},
			"maxKeys": {
				Type:         "number",
				Title:        "Max pending keys",
				Description:  "Maximum incomplete groups kept; the oldest is dropped beyond this",
				Default:      defaultJoinMaxKeys,
				AgentVisible: true,
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
| timeoutMs | number | 300000 | Drop an incomplete group after this long |
| maxKeys | number | 1000 | Max incomplete groups; oldest dropped beyond this |

## Use Cases

- Pair a request with its later response by an id (key)
- Combine readings from several sensor branches into one record (source)
`,
		Category: types.NodeCategoryProcessing,
		Risk:     node.RiskSafe,
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
