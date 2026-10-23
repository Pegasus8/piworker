package builtin

import (
	"context"
	"fmt"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// FilterProcess passes a message through only when its condition is truthy,
// dropping it otherwise.
type FilterProcess struct {
	*node.BaseNode

	condition string
	program   *vm.Program
}

// NewFilterProcess creates a new FilterProcess from configuration.
func NewFilterProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	condition := base.GetConfigString("condition", "true")
	if condition == "" {
		condition = "true"
	}

	program, err := compileExpr(condition, expr.AsBool())
	if err != nil {
		return nil, fmt.Errorf("invalid condition: %w", err)
	}

	return &FilterProcess{BaseNode: base, condition: condition, program: program}, nil
}

// Process evaluates the condition and forwards the message only if it is true.
func (p *FilterProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	result, err := evalExpr(ctx, p.program, msg)
	if err != nil {
		return nil, fmt.Errorf("filter evaluation failed: %w", err)
	}

	if pass, _ := result.(bool); !pass {
		return nil, nil // dropped: condition false
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("filtered", true)
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (p *FilterProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (p *FilterProcess) Validate() error {
	if p.program == nil {
		return fmt.Errorf("condition failed to compile")
	}
	return nil
}

func filterProcessConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"condition": {
				Type:        "string",
				Title:       "Condition",
				Description: "Boolean expression; the message passes only when true (e.g. payload.value > 10)",
				Default:     "true",
			},
		},
		Required: []string{"condition"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (p *FilterProcess) GetConfigSchema() node.ConfigSchema {
	return filterProcessConfigSchema()
}

// FilterProcessInfo returns the node type info for registration.
func FilterProcessInfo() node.NodeTypeInfo {
	schema := filterProcessConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-filter",
		Name:        "Filter",
		Description: "Pass a message through only when a condition is true",
		Documentation: `## Filter

Forwards a message only when its boolean condition evaluates to true; otherwise
the message is dropped. Built on the same sandboxed expression engine as
Transform.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| condition | string | true | Boolean expression over payload/meta/topic |

## Examples

` + "```" + `
payload.temperature > 30
payload.status == "active"
len(payload.items) > 0
contains(payload.email, "@example.com")
` + "```" + `

## Use Cases

- Drop irrelevant events
- Gate actions behind thresholds
- Deduplicate by a condition
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "filter",
	}
}

func init() {
	info := FilterProcessInfo()
	if err := node.Register(info, NewFilterProcess); err != nil {
		panic("failed to register filter process: " + err.Error())
	}
}
