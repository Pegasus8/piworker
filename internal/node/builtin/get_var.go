package builtin

import (
	"context"
	"fmt"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/vars"
)

// GetVarProcess reads a value from the shared variable store into the payload.
type GetVarProcess struct {
	*node.BaseNode

	key          string
	defaultValue string
	hasDefault   bool
}

// NewGetVarProcess creates a new GetVarProcess from configuration.
func NewGetVarProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	key := base.GetConfigString("key", "")
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}

	_, hasDefault := config["default"]

	return &GetVarProcess{
		BaseNode:     base,
		key:          key,
		defaultValue: base.GetConfigString("default", ""),
		hasDefault:   hasDefault,
	}, nil
}

// Process reads the variable and sets it as the message payload.
func (p *GetVarProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	value, ok := vars.DefaultStore.Get(p.key)
	if !ok {
		if p.hasDefault {
			value = p.defaultValue
		} else {
			value = nil
		}
	}

	out := msg.Clone()
	out.Payload = value
	out.PayloadType = inferDataType(value)
	out.SourcePort = "output"
	out.SetMeta("varGet", p.key)
	out.SetMeta("varFound", ok)
	return []*types.Message{out}, nil
}

// inferDataType maps a Go value to a DataType.
func inferDataType(v interface{}) types.DataType {
	switch v.(type) {
	case string:
		return types.DataTypeString
	case int, int64, float64, float32:
		return types.DataTypeNumber
	case bool:
		return types.DataTypeBoolean
	case []interface{}:
		return types.DataTypeArray
	case map[string]interface{}:
		return types.DataTypeObject
	default:
		return types.DataTypeAny
	}
}

// Ports returns the port definitions.
func (p *GetVarProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (p *GetVarProcess) Validate() error {
	if p.key == "" {
		return fmt.Errorf("key is required")
	}
	return nil
}

func getVarConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"key": {
				Type:        "string",
				Title:       "Variable Name",
				Description: "Name of the variable to read",
			},
			"default": {
				Type:        "string",
				Title:       "Default",
				Description: "Value used when the variable is not set",
				Default:     "",
			},
		},
		Required: []string{"key"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (p *GetVarProcess) GetConfigSchema() node.ConfigSchema {
	return getVarConfigSchema()
}

// GetVarProcessInfo returns the node type info for registration.
func GetVarProcessInfo() node.NodeTypeInfo {
	schema := getVarConfigSchema()
	return node.NodeTypeInfo{
		Type:        "get-var",
		Name:        "Get Variable",
		Description: "Read a named variable into the payload",
		Documentation: `## Get Variable

Reads a process-wide variable (set by **Set Variable**) and places its value in
the message payload.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| key | string | - | Variable name (required) |
| default | string | - | Value used when the variable is unset |

## Output

- **payload** - the variable's value (or the default)
- **meta.varFound** - whether the variable existed
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "variable",
	}
}

func init() {
	info := GetVarProcessInfo()
	if err := node.Register(info, NewGetVarProcess); err != nil {
		panic("failed to register get-var process: " + err.Error())
	}
}
