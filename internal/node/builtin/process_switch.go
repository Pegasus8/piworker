package builtin

import (
	"context"
	"fmt"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// SwitchProcess routes a message to its "true" or "false" output port based on a
// boolean condition (an if/else branch).
type SwitchProcess struct {
	*node.BaseNode

	condition string
	program   *vm.Program
}

// NewSwitchProcess creates a new SwitchProcess from configuration.
func NewSwitchProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	condition := base.GetConfigString("condition", "true")
	if condition == "" {
		condition = "true"
	}

	program, err := compileExpr(condition, expr.AsBool())
	if err != nil {
		return nil, fmt.Errorf("invalid condition: %w", err)
	}

	return &SwitchProcess{BaseNode: base, condition: condition, program: program}, nil
}

// Process evaluates the condition and routes the message to the matching port.
func (p *SwitchProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	result, err := evalExpr(ctx, p.program, msg)
	if err != nil {
		return nil, fmt.Errorf("switch evaluation failed: %w", err)
	}

	out := msg.Clone()
	if pass, _ := result.(bool); pass {
		out.SourcePort = "true"
	} else {
		out.SourcePort = "false"
	}
	return []*types.Message{out}, nil
}

// Ports returns the port definitions: one input, two outputs (true/false).
func (p *SwitchProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{
			{ID: "true", Name: "True", DataType: types.DataTypeAny, Multiple: true},
			{ID: "false", Name: "False", DataType: types.DataTypeAny, Multiple: true},
		}
}

// Validate checks the configuration.
func (p *SwitchProcess) Validate() error {
	if p.program == nil {
		return fmt.Errorf("condition failed to compile")
	}
	return nil
}

func switchProcessConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"condition": {
				Type:        "string",
				Title:       "Condition",
				Description: "Boolean expression; true routes to the True port, false to the False port",
				Default:     "true",
			},
		},
		Required: []string{"condition"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (p *SwitchProcess) GetConfigSchema() node.ConfigSchema {
	return switchProcessConfigSchema()
}

// SwitchProcessInfo returns the node type info for registration.
func SwitchProcessInfo() node.NodeTypeInfo {
	schema := switchProcessConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-switch",
		Name:        "Switch (If/Else)",
		Description: "Route a message to one of two outputs based on a condition",
		Documentation: `## Switch (If/Else)

Evaluates a boolean condition and routes the message to the **True** output when
it holds, or the **False** output otherwise. Connect different branches of your
flow to each output.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| condition | string | true | Boolean expression over payload/meta/topic |

## Outputs

| Port | When |
|------|------|
| true | condition evaluated to true |
| false | condition evaluated to false |

## Examples

` + "```" + `
payload.temperature > 30
payload.user.role == "admin"
` + "```" + `
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs: []types.Port{
			{ID: "true", Name: "True", DataType: types.DataTypeAny, Multiple: true},
			{ID: "false", Name: "False", DataType: types.DataTypeAny, Multiple: true},
		},
		Config: &schema,
		Icon:   "git-branch",
	}
}

func init() {
	info := SwitchProcessInfo()
	if err := node.Register(info, NewSwitchProcess); err != nil {
		panic("failed to register switch process: " + err.Error())
	}
}
