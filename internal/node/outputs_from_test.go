package node

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classifyInfo is a type whose "categories" setting adds output ports.
func classifyInfo() NodeTypeInfo {
	return NodeTypeInfo{
		Type: "test-classify", Name: "Classify", Category: types.NodeCategoryProcessing, Risk: RiskSafe,
		Inputs:      []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
		Outputs:     []types.Port{{ID: "other", Name: "Other", DataType: types.DataTypeAny}},
		OutputsFrom: "categories",
		Config: &ConfigSchema{Properties: map[string]ConfigProperty{
			"categories": {Type: "array", Title: "Categories", AgentVisible: true},
			"name":       {Type: "string", Title: "Name"},
		}},
	}
}

func TestRegisterAcceptsOutputsFromAnArraySetting(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(classifyInfo(), mockNodeFactory))
	info, ok := reg.Get("test-classify")
	require.True(t, ok)
	assert.Equal(t, "categories", info.OutputsFrom)
}

// OutputsFrom must name an array setting of the type's schema: anything
// else could never give the editor and the runtime the same ports.
func TestRegisterRefusesABadOutputsFrom(t *testing.T) {
	cases := map[string]func(*NodeTypeInfo){
		"undeclared setting": func(i *NodeTypeInfo) { i.OutputsFrom = "labels" },
		"not an array":       func(i *NodeTypeInfo) { i.OutputsFrom = "name" },
		"no schema":          func(i *NodeTypeInfo) { i.Config = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			info := classifyInfo()
			mutate(&info)
			err := NewRegistry().Register(info, func(map[string]any) (Node, error) { return &mockNode{BaseNode: NewBaseNode(nil)}, nil })
			require.Error(t, err)
			assert.Contains(t, err.Error(), "outputsFrom")
		})
	}
}

// The schema derived from a probe instance counts too.
func TestRegisterChecksOutputsFromAgainstTheDerivedSchema(t *testing.T) {
	info := classifyInfo()
	schema := *info.Config
	info.Config = nil
	factory := func(cfg map[string]any) (Node, error) {
		return &schemaNode{mockNode: mockNode{BaseNode: NewBaseNode(cfg)}, schema: schema}, nil
	}
	require.NoError(t, NewRegistry().Register(info, factory))
}

type schemaNode struct {
	mockNode
	schema ConfigSchema
}

func (n *schemaNode) GetConfigSchema() ConfigSchema { return n.schema }

func TestRegisterRefusesUnknownOptions(t *testing.T) {
	info := classifyInfo()
	info.OutputsFrom = ""
	info.Config.Properties["name"] = ConfigProperty{Type: "string", Title: "Name", Options: "llm-modles"}
	err := NewRegistry().Register(info, mockNodeFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"llm-modles"`)

	for _, source := range []string{OptionsConnections, OptionsZigbeeDevices, OptionsZigbeeProperties, OptionsZigbeeSettable,
		OptionsLLMConnections, OptionsLLMModels, OptionsTools, OptionsKnowledgeCollections} {
		info := classifyInfo()
		info.Type = "test-" + source
		info.Config.Properties["name"] = ConfigProperty{Type: "string", Title: "Name", Options: source}
		assert.NoError(t, NewRegistry().Register(info, mockNodeFactory), source)
	}
}

func TestOptionsSources(t *testing.T) {
	assert.Equal(t, "llm-connections", OptionsLLMConnections)
	assert.Equal(t, "llm-models", OptionsLLMModels)
	assert.Equal(t, "tools", OptionsTools)
	assert.Equal(t, "knowledge-collections", OptionsKnowledgeCollections)
}

// ConfigOutputs: the static outputs, then one port per item of the
// OutputsFrom setting (ID and name = the item), skipping what could never be
// a port of its own.
func TestConfigOutputs(t *testing.T) {
	info := classifyInfo()
	got := ConfigOutputs(info, map[string]any{"categories": []any{"bug", "question", "", 7, "bug", "other", "error", "praise"}})
	ids := make([]string, len(got))
	for i, p := range got {
		ids[i] = p.ID
		if i > 0 {
			assert.Equal(t, p.ID, p.Name)
			assert.Equal(t, types.DataTypeAny, p.DataType)
		}
	}
	assert.Equal(t, []string{"other", "bug", "question", "praise"}, ids)

	assert.Equal(t, []string{"other", "a"}, portIDs(ConfigOutputs(info, map[string]any{"categories": []string{"a"}})))
	assert.Equal(t, []string{"other"}, portIDs(ConfigOutputs(info, nil)))
	assert.Equal(t, []string{"other"}, portIDs(ConfigOutputs(info, map[string]any{"categories": "a"})))

	plain := classifyInfo()
	plain.OutputsFrom = ""
	assert.Equal(t, []string{"other"}, portIDs(ConfigOutputs(plain, map[string]any{"categories": []any{"a"}})))

	// The registered info's slice is never shared with the result.
	out := ConfigOutputs(info, map[string]any{"categories": []any{"a"}})
	out[0].ID = "changed"
	assert.Equal(t, "other", info.Outputs[0].ID)
}

func portIDs(ports []types.Port) []string {
	ids := make([]string, len(ports))
	for i, p := range ports {
		ids[i] = p.ID
	}
	return ids
}

func TestConfigOutputsIsNeverNil(t *testing.T) {
	assert.NotNil(t, ConfigOutputs(NodeTypeInfo{Type: "sink"}, nil))
}
