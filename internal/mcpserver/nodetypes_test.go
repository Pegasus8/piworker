package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	_ "github.com/Pegasus8/piworker/internal/node/builtin" // the real node types
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(f float64) *float64 { return &f }

func noFactory(map[string]any) (node.Node, error) { return nil, errors.New("not instantiable") }

// testNodeTypes is a registry of three crafted types, registered out of
// order: a trigger, a processing node without settings and an action with
// every marker.
func testNodeTypes(t *testing.T) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	in := types.Port{ID: "input", Name: "Input", DataType: types.DataTypeAny}
	out := types.Port{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}
	for _, info := range []node.NodeTypeInfo{
		{Type: "action-c", Name: "Call", Description: "Calls a URL.", Documentation: "# Call\nSends the payload.",
			Category: types.NodeCategoryOutput, Inputs: []types.Port{in}, Risk: node.RiskExternal, Icon: "globe",
			Config: &node.ConfigSchema{Required: []string{"url"}, Properties: map[string]node.ConfigProperty{
				"url":          {Type: "string", Title: "URL", Description: "Where to send.", Destination: true, AgentVisible: true},
				"allowPrivate": {Type: "boolean", Title: "Allow private", Default: false, RiskSwitch: true, AgentVisible: true},
				"method":       {Type: "string", Title: "Method", Enum: []string{"GET", "POST"}, Default: "GET", Operation: true, AgentVisible: true, CaseInsensitive: true},
				"password":     {Type: "string", Title: "Password", Sensitive: true},
			}}},
		{Type: "trigger-a", Name: "Tick", Description: "Ticks.", Documentation: "Ticks every interval.",
			Category: types.NodeCategoryInput, Outputs: []types.Port{out}, Risk: node.RiskSafe,
			Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
				"intervalMs": {Type: "integer", Title: "Interval", Description: "In milliseconds.", Minimum: ptr(1), Maximum: ptr(86400000),
					Unit: "milliseconds", Default: 1000, AgentVisible: true, Rate: true},
			}}},
		{Type: "process-b", Name: "Pass", Description: "Passes messages on.", Category: types.NodeCategoryProcessing,
			Inputs: []types.Port{in}, Outputs: []types.Port{out}, Risk: node.RiskSafe},
	} {
		require.NoError(t, reg.Register(info, noFactory))
	}
	return reg
}

// The list is ordered by category (triggers, processing, actions) and type,
// and shows each type's ports, risk, settings with their markers (present
// only when true) and documentation: registry metadata only.
func TestListNodeTypes(t *testing.T) {
	list := listNodeTypes(testNodeTypes(t), "output", "")
	require.Len(t, list.Types, 1)
	assert.JSONEq(t, `{"type":"action-c","name":"Call","category":"output","risk":"external","description":"Calls a URL.",
		"inputs":[{"id":"input","name":"Input","dataType":"any","multiple":false,"required":false}],"outputs":[],
		"config":{"required":["url"],"properties":{
			"allowPrivate":{"type":"boolean","title":"Allow private","default":false,"riskSwitch":true,"agentVisible":true},
			"method":{"type":"string","title":"Method","enum":["GET","POST"],"default":"GET","caseInsensitive":true,"operation":true,"agentVisible":true},
			"password":{"type":"string","title":"Password","sensitive":true},
			"url":{"type":"string","title":"URL","description":"Where to send.","destination":true,"agentVisible":true}}},
		"documentation":"# Call\nSends the payload."}`, jsonOf(t, list.Types[0]), "a category carries the documentation")

	list = listNodeTypes(testNodeTypes(t), "", "")
	assert.Equal(t, 3, list.Total)
	assert.False(t, list.Truncated)
	var order []string
	for _, v := range list.Types {
		order = append(order, v.Type)
	}
	assert.Equal(t, []string{"trigger-a", "process-b", "action-c"}, order)

	assert.JSONEq(t, `{"type":"action-c","name":"Call","category":"output","risk":"external","description":"Calls a URL.",
		"inputs":[{"id":"input","name":"Input","dataType":"any","multiple":false,"required":false}],"outputs":[],
		"config":{"required":["url"],"properties":{
			"allowPrivate":{"type":"boolean","title":"Allow private","default":false,"riskSwitch":true,"agentVisible":true},
			"method":{"type":"string","title":"Method","enum":["GET","POST"],"default":"GET","caseInsensitive":true,"operation":true,"agentVisible":true},
			"password":{"type":"string","title":"Password","sensitive":true},
			"url":{"type":"string","title":"URL","description":"Where to send.","destination":true,"agentVisible":true}}},
		"documentationOmitted":true}`, jsonOf(t, list.Types[2]), "without arguments: structures only")
	assert.JSONEq(t, `{"type":"trigger-a","name":"Tick","category":"input","risk":"safe","description":"Ticks.","inputs":[],
		"outputs":[{"id":"output","name":"Output","dataType":"object","multiple":true,"required":false}],
		"config":{"required":[],"properties":{"intervalMs":{"type":"integer","title":"Interval","description":"In milliseconds.",
			"default":1000,"minimum":1,"maximum":86400000,"unit":"milliseconds","agentVisible":true,"rate":true}}},
		"documentationOmitted":true}`, jsonOf(t, list.Types[0]))
	assert.False(t, list.Types[1].DocumentationOmitted, "a type without documentation omits none")
	assert.JSONEq(t, `{"properties":{},"required":[]}`, jsonOf(t, list.Types[1].Config), "no settings")
	assert.NotContains(t, jsonOf(t, list), "globe", "only what authoring needs")
	assert.Equal(t, jsonOf(t, list), jsonOf(t, listNodeTypes(testNodeTypes(t), "", "")), "deterministic")
}

func TestListNodeTypesFilters(t *testing.T) {
	reg := testNodeTypes(t)
	list := listNodeTypes(reg, "processing", "")
	require.Len(t, list.Types, 1)
	assert.Equal(t, "process-b", list.Types[0].Type)
	assert.Equal(t, 1, list.Total)

	list = listNodeTypes(reg, "", "action-c")
	require.Len(t, list.Types, 1)
	assert.Equal(t, "action-c", list.Types[0].Type)

	assert.Empty(t, listNodeTypes(reg, "input", "action-c").Types, "both filters apply")
}

// The list's JSON stays within maxNodeTypeListBytes: every type's structure
// first, in order while it fits, then (with a filter) the documentation in
// order while it fits (documentationOmitted otherwise), each cut to
// maxNodeTypeDocBytes. Asking for one type gets its documentation.
func TestListNodeTypesIsBounded(t *testing.T) {
	reg := node.NewRegistry()
	for i := range 100 {
		require.NoError(t, reg.Register(node.NodeTypeInfo{
			Type: fmt.Sprintf("process-%03d", i), Name: "N", Description: strings.Repeat("d", 200), Category: types.NodeCategoryProcessing,
			Documentation: strings.Repeat("é", maxNodeTypeDocBytes), Risk: node.RiskSafe,
		}, noFactory))
	}
	list := listNodeTypes(reg, "processing", "")
	data := jsonOf(t, list)
	assert.LessOrEqual(t, len(data), maxNodeTypeListBytes)
	assert.True(t, list.Truncated)
	assert.Equal(t, 100, list.Total)
	assert.Len(t, list.Types, 100, "every structure fits")
	assert.False(t, list.Types[0].DocumentationOmitted)
	assert.LessOrEqual(t, len(list.Types[0].Documentation), maxNodeTypeDocBytes+len("…"))
	assert.True(t, strings.HasSuffix(list.Types[0].Documentation, "…"), "a cut documentation says so")
	assert.True(t, list.Types[99].DocumentationOmitted)
	assert.Empty(t, list.Types[99].Documentation)

	one := listNodeTypes(reg, "", "process-099")
	require.Len(t, one.Types, 1)
	assert.NotEmpty(t, one.Types[0].Documentation)
	assert.True(t, one.Truncated, "cut documentation")

	// So many types that not even their structures fit: the first ones are
	// listed, in order.
	huge := node.NewRegistry()
	for i := range 400 {
		require.NoError(t, huge.Register(node.NodeTypeInfo{Type: fmt.Sprintf("process-%03d", i), Description: strings.Repeat("d", 400),
			Category: types.NodeCategoryProcessing, Risk: node.RiskSafe}, noFactory))
	}
	list = listNodeTypes(huge, "", "")
	assert.LessOrEqual(t, len(jsonOf(t, list)), maxNodeTypeListBytes)
	assert.True(t, list.Truncated)
	assert.Equal(t, 400, list.Total)
	assert.Less(t, len(list.Types), 400)
	assert.Equal(t, "process-000", list.Types[0].Type)
}

// With the real registry every node type is listed within the bound, and
// the markers agents need to author safely are there.
func TestListNodeTypesOfTheBuiltins(t *testing.T) {
	list := listNodeTypes(node.DefaultRegistry, "", "")
	assert.Equal(t, node.DefaultRegistry.Count(), list.Total)
	assert.Len(t, list.Types, list.Total)
	assert.False(t, list.Truncated, "without arguments every structure is listed")
	assert.LessOrEqual(t, len(jsonOf(t, list)), maxNodeTypeListBytes)
	for _, v := range list.Types {
		assert.Empty(t, v.Documentation, v.Type)
	}
	// Each category is listed whole, with its documentation: an agent
	// reads the types category by category.
	for _, category := range categories {
		cat := listNodeTypes(node.DefaultRegistry, category, "")
		assert.False(t, cat.Truncated, category)
		assert.Len(t, cat.Types, cat.Total, category)
		assert.LessOrEqual(t, len(jsonOf(t, cat)), maxNodeTypeListBytes, category)
		for _, v := range cat.Types {
			assert.False(t, v.DocumentationOmitted, v.Type)
		}
	}
	byType := map[string]NodeTypeView{}
	for _, v := range list.Types {
		byType[v.Type] = v
	}
	http := byType["action-http"].Config.Properties
	assert.True(t, http["url"].Destination)
	assert.True(t, http["allowPrivate"].RiskSwitch)
	assert.True(t, http["method"].Operation)
	assert.Equal(t, "privileged", string(byType["action-command"].Risk))
	assert.Contains(t, byType, "trigger-tool", "listed for reference")
	for _, v := range list.Types {
		assert.NotEmpty(t, v.Description, v.Type)
	}
}

// pw_list_node_types answers the list through the endpoint (author scope).
func TestListNodeTypesTool(t *testing.T) {
	a := newAuthorEnv(t)
	res, _ := called(t, a.testEnv, authorToken, "pw_list_node_types", `{}`)
	assert.JSONEq(t, jsonOf(t, listNodeTypes(testNodeTypes(t), "", "")), structuredJSON(t, res))
	res, _ = called(t, a.testEnv, authorToken, "pw_list_node_types", `{"category":"output","type":"action-c"}`)
	var got NodeTypeList
	require.NoError(t, json.Unmarshal([]byte(structuredJSON(t, res)), &got))
	require.Len(t, got.Types, 1)
	assert.Equal(t, "action-c", got.Types[0].Type)
	assert.Empty(t, a.authoring.recorded(), "the registry alone: no service state")
}
