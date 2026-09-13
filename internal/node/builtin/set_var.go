package builtin

import (
	"context"
	"fmt"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/vars"
	"github.com/expr-lang/expr/vm"
)

// SetVarProcess stores a computed value into the shared variable store and
// passes the message through unchanged.
type SetVarProcess struct {
	*node.BaseNode
	scope string

	key       string
	valueExpr string
	program   *vm.Program
}

// NewSetVarProcess creates a new SetVarProcess from configuration.
func NewSetVarProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	scope := base.GetConfigString("scope", "global")
	if scope != "global" && scope != "flow" {
		return nil, fmt.Errorf("invalid variable scope %q", scope)
	}

	key := base.GetConfigString("key", "")
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}

	valueExpr := base.GetConfigString("value", "payload")
	if valueExpr == "" {
		valueExpr = "payload"
	}

	program, err := compileExpr(valueExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid value expression: %w", err)
	}

	return &SetVarProcess{BaseNode: base, scope: scope, key: key, valueExpr: valueExpr, program: program}, nil
}

// Process evaluates the value expression and stores it under the key.
func (p *SetVarProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	store, err := vars.Scoped(ctx, p.scope)
	if err != nil {
		return nil, err
	}
	result, err := evalExpr(ctx, p.program, msg)
	if err != nil {
		return nil, fmt.Errorf("value evaluation failed: %w", err)
	}

	if err := store.Set(p.key, result); err != nil {
		return nil, fmt.Errorf("failed to persist variable %q: %w", p.key, err)
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("varSet", p.key)
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (p *SetVarProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (p *SetVarProcess) Validate() error {
	if p.key == "" {
		return fmt.Errorf("key is required")
	}
	if p.program == nil {
		return fmt.Errorf("value expression failed to compile")
	}
	return nil
}

func setVarConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"scope": {Type: "string", Title: "Variable scope", Description: "Global is shared by all flows; Flow is isolated to this saved flow.", Default: "global", Enum: []string{"global", "flow"}},
			"key": {
				Type:        "string",
				Title:       "Variable Name",
				Description: "Name of the variable to store",
			},
			"value": {
				Type:        "string",
				Title:       "Value Expression",
				Description: "Expression evaluated to compute the stored value (e.g. payload.count)",
				Default:     "payload",
			},
		},
		Required: []string{"key"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (p *SetVarProcess) GetConfigSchema() node.ConfigSchema {
	return setVarConfigSchema()
}

// SetVarProcessInfo returns the node type info for registration.
func SetVarProcessInfo() node.NodeTypeInfo {
	schema := setVarConfigSchema()
	return node.NodeTypeInfo{
		Type:        "set-var",
		Name:        "Set Variable",
		Description: "Store a value into a named variable",
		Documentation: `## Set Variable

Stores a computed value in the selected scope. **Global** (the default) shares
values across flows; **Flow** keeps them in this saved flow only. The message
passes through unchanged. Manage both scopes from **Variables** in the editor.

Use vars["threshold"] for globals and flowVars["threshold"] for this flow
in value expressions. Values persist across restarts and changes are visible on
the next read. Playground tests use copies and never change stored values.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| scope | string | global | global or flow; no fallback between scopes |
| key | string | - | Variable name (required) |
| value | string | payload | Expression evaluated to compute the value |

## Examples

` + "```" + `
key: lastTemperature
value: payload.temperature
` + "```" + `
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "variable",
	}
}

func init() {
	info := SetVarProcessInfo()
	if err := node.Register(info, NewSetVarProcess); err != nil {
		panic("failed to register set-var process: " + err.Error())
	}
}
