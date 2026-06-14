package builtin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// RateLimitProcess passes at most `limit` messages per sliding time window,
// dropping the rest. State is guarded by a mutex; although the runtime drains a
// node's messages serially, the lock keeps the node correct on its own.
type RateLimitProcess struct {
	*node.BaseNode

	limit  int
	window time.Duration

	mu     sync.Mutex
	stamps []time.Time
	now    func() time.Time
}

// NewRateLimitProcess creates a RateLimitProcess from configuration.
func NewRateLimitProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	limit := base.GetConfigInt("limit", 1)
	if limit < 1 {
		limit = 1
	}
	windowMs := base.GetConfigInt("windowMs", 1000)
	if windowMs < 1 {
		windowMs = 1000
	}
	return &RateLimitProcess{
		BaseNode: base,
		limit:    limit,
		window:   time.Duration(windowMs) * time.Millisecond,
		now:      time.Now,
	}, nil
}

// Process forwards the message if the rate is under the limit, else drops it.
func (p *RateLimitProcess) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cutoff := p.now().Add(-p.window)
	kept := p.stamps[:0]
	for _, t := range p.stamps {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	p.stamps = kept

	if len(p.stamps) >= p.limit {
		return nil, nil // dropped: over the rate limit
	}

	p.stamps = append(p.stamps, p.now())
	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("rateLimited", false)
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (p *RateLimitProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (p *RateLimitProcess) Validate() error {
	if p.limit < 1 {
		return fmt.Errorf("limit must be >= 1")
	}
	if p.window <= 0 {
		return fmt.Errorf("window must be positive")
	}
	return nil
}

func rateLimitProcessConfigSchema() node.ConfigSchema {
	minOne := 1.0
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"limit": {
				Type:        "number",
				Title:       "Limit",
				Description: "Maximum messages allowed per window",
				Default:     1,
				Minimum:     &minOne,
			},
			"windowMs": {
				Type:        "number",
				Title:       "Window (ms)",
				Description: "Sliding window length in milliseconds",
				Default:     1000,
				Minimum:     &minOne,
			},
		},
		Required: []string{"limit", "windowMs"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (p *RateLimitProcess) GetConfigSchema() node.ConfigSchema {
	return rateLimitProcessConfigSchema()
}

// RateLimitProcessInfo returns the node type info for registration.
func RateLimitProcessInfo() node.NodeTypeInfo {
	schema := rateLimitProcessConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-ratelimit",
		Name:        "Rate Limit",
		Description: "Pass at most N messages per time window, dropping the rest",
		Documentation: `## Rate Limit

Throttles a stream: at most ` + "`limit`" + ` messages pass per sliding ` + "`windowMs`" + `
window; messages over the limit are dropped.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| limit | number | 1 | Max messages per window |
| windowMs | number | 1000 | Window length in milliseconds |

## Use Cases

- Protect a downstream API from bursts
- Collapse chatty sensor readings to one per second
- Debounce a noisy trigger (limit 1)
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "gauge",
	}
}

func init() {
	info := RateLimitProcessInfo()
	if err := node.Register(info, NewRateLimitProcess); err != nil {
		panic("failed to register ratelimit process: " + err.Error())
	}
}
