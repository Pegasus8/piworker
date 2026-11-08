package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFlowShowsTheGraph(t *testing.T) {
	e := newReadsEnv(t)
	f := manualFlow("graph")
	f.Nodes[0].Position = flow.Position{X: 10, Y: 20}
	f.Nodes[1].Enabled = false
	e.save(f)

	view, err := e.reads.GetFlow(context.Background(), readsAgent(storage.ScopeRead), f.ID)
	require.NoError(t, err)
	assert.Equal(t, f.ID, view.ID)
	assert.Equal(t, "graph", view.Name)
	assert.Equal(t, FlowStateStopped, view.State)
	assert.False(t, view.Truncated)
	require.Len(t, view.Nodes, 2)

	trig, logN := view.Nodes[0], view.Nodes[1]
	assert.Equal(t, "trigger", trig.ID)
	assert.Equal(t, "trigger-manual", trig.Type)
	manualInfo, _ := node.DefaultRegistry.Get("trigger-manual")
	assert.Equal(t, manualInfo.Name, trig.Name, "a node's name is its type's display name")
	assert.True(t, trig.Enabled)
	assert.Equal(t, flow.Position{X: 10, Y: 20}, trig.Position)
	assert.Equal(t, node.RiskSafe, trig.Risk)
	assert.Equal(t, map[string]any{}, trig.Config, "a node without config shows an empty config")

	assert.Equal(t, "log", logN.ID)
	assert.False(t, logN.Enabled)
	assert.Equal(t, map[string]any{"level": "info", "message": redactedMark}, logN.Config)

	require.Len(t, view.Connections, 1)
	c := view.Connections[0]
	assert.Equal(t, ConnectionView{ID: f.Connections[0].ID, SourceNode: "trigger", SourcePort: "output", TargetNode: "log", TargetPort: "input"}, c)
	assert.Empty(t, view.Tools)

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, view)), &raw))
	for _, key := range []string{"id", "name", "state", "nodes", "connections", "tools", "truncated"} {
		assert.Containsf(t, raw, key, "flow view field %s", key)
	}
	n0 := raw["nodes"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "type", "name", "enabled", "position", "risk", "config"} {
		assert.Containsf(t, n0, key, "node view field %s", key)
	}
	c0 := raw["connections"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "sourceNode", "sourcePort", "targetNode", "targetPort"} {
		assert.Containsf(t, c0, key, "connection view field %s", key)
	}
	assert.Equal(t, []any{}, raw["tools"], "tools is a list even when empty")
}

func TestGetFlowShowsTheRunningTools(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(readsToolFlow("tools", "reads_get_flow_tool"))
	e.run(f)

	view, err := e.reads.GetFlow(context.Background(), readsAgent(storage.ScopeRead), f.ID)
	require.NoError(t, err)
	assert.Equal(t, FlowStateRunning, view.State)
	require.Len(t, view.Tools, 1)
	_, tools := e.manager.Tools()
	require.Len(t, tools, 1)
	want := tools[0]
	got := view.Tools[0]
	assert.Equal(t, "reads_get_flow_tool", got.Name)
	assert.Equal(t, "tool", got.TriggerNodeID)
	assert.Equal(t, want.Risk, got.Risk)
	assert.Equal(t, want.Annotations, got.Annotations)
	assert.Equal(t, want.Fingerprint, got.Fingerprint)
	assert.Equal(t, want.Confirm, got.Confirm)
	assert.Equal(t, want.Timeout.Milliseconds(), got.TimeoutMs)

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, view)), &raw))
	t0 := raw["tools"].([]any)[0].(map[string]any)
	for _, key := range []string{"name", "triggerNodeId", "risk", "annotations", "fingerprint", "confirm", "timeoutMs"} {
		assert.Containsf(t, t0, key, "tool view field %s", key)
	}
	// The tool's AgentVisible settings are shown in its node's config.
	toolCfg := view.Nodes[0].Config
	assert.Equal(t, "reads_get_flow_tool", toolCfg["toolName"])
	assert.NotEqual(t, redactedMark, toolCfg["inputSchema"])
}

func TestGetFlowUnknownFlow(t *testing.T) {
	e := newReadsEnv(t)
	for _, id := range []string{"missing", ""} {
		_, err := e.reads.GetFlow(context.Background(), EditorActor(), id)
		te := requireKind(t, err, ErrFlowNotFound)
		assert.Equal(t, "flow_not_found", te.Code())
	}
}

// Secret values never appear, in any encoding, even when the admin pasted
// one into a visible property; {{secret.NAME}} references are names and stay.
func TestGetFlowRedactsSecretValues(t *testing.T) {
	e := newReadsEnv(t)
	const value = "Zq9-Secret/Value+7&k"
	require.NoError(t, e.secrets.Set("API_KEY", value))

	f := flow.NewFlow("leaky " + value)
	debug := flow.NewNode("process-debug", flow.NodeCategoryProcessing)
	debug.ID = "debug"
	debug.Config = map[string]any{"name": "token is " + value + ", reply " + secretLeaks(value)["json payload"]}
	setVar := flow.NewNode("set-var", flow.NodeCategoryProcessing)
	setVar.ID = "setvar"
	setVar.Config = map[string]any{"key": url.QueryEscape(value), "scope": "flow"}
	getVar := flow.NewNode("get-var", flow.NodeCategoryProcessing)
	getVar.ID = "getvar"
	getVar.Config = map[string]any{"key": base64.StdEncoding.EncodeToString([]byte("user:" + value))}
	join := flow.NewNode("process-join", flow.NodeCategoryProcessing)
	join.ID = "join"
	join.Config = map[string]any{"sources": "{{secret.API_KEY}}"}
	httpN := flow.NewNode("action-http", flow.NodeCategoryOutput)
	httpN.ID = "http"
	httpN.Config = map[string]any{
		"url":     "https://user:" + value + "@example.com/x?token=" + value,
		"headers": map[string]any{"Authorization": "Bearer {{secret.API_KEY}}"},
	}
	for _, n := range []*flow.Node{debug, setVar, getVar, join, httpN} {
		f.AddNode(*n)
	}
	e.save(f)

	view, err := e.reads.GetFlow(context.Background(), readsAgent(storage.ScopeRead), f.ID)
	require.NoError(t, err)
	out := leakText(t, view)
	for name, form := range secretForms(value) {
		assert.NotContainsf(t, out, form, "secret leaked as %s", name)
	}
	assert.Contains(t, out, "token is "+redactedMark)
	assert.Contains(t, out, "{{secret.API_KEY}}", "secret references are names, not values")
}

// secretLeaks are texts that leak a secret value in the encodings a leak
// could take: what the read canaries plant in recorded text.
func secretLeaks(v string) map[string]string {
	return map[string]string{
		"literal":    v,
		"query":      url.QueryEscape(v),
		"path":       url.PathEscape(v),
		"base64":     base64.StdEncoding.EncodeToString([]byte(v)),
		"base64url":  base64.URLEncoding.EncodeToString([]byte(v)),
		"basic auth": "Basic " + base64.StdEncoding.EncodeToString([]byte("user:"+v)),
		// A payload echoed in an error: JSON (HTML-escaped, as
		// json.Marshal writes it) and Go-quoted.
		"json payload": `{"payload":` + jsonString(v) + `}`,
		"go quoted":    fmt.Sprintf("payload %q", v),
	}
}

// jsonString is v as json.Marshal encodes it (HTML characters escaped).
func jsonString(v string) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// secretForms are the texts that must not appear where secretLeaks were
// planted: every encoding of the value, and the part of an embedded base64
// encoding that depends on the value alone.
func secretForms(v string) map[string]string {
	return map[string]string{
		"literal":       v,
		"query":         url.QueryEscape(v),
		"path":          url.PathEscape(v),
		"base64":        base64.StdEncoding.EncodeToString([]byte(v)),
		"base64url":     base64.URLEncoding.EncodeToString([]byte(v)),
		"base64 inside": strings.TrimRight(base64.StdEncoding.EncodeToString([]byte("user:"+v)), "=")[8:],
		"json escaped":  strings.Trim(jsonString(v), `"`),
		"go escaped":    strings.Trim(fmt.Sprintf("%q", v), `"`),
	}
}

func TestGetFlowBoundsVisibleValues(t *testing.T) {
	e := newReadsEnv(t)
	f := flow.NewFlow("big")
	debug := flow.NewNode("process-debug", flow.NodeCategoryProcessing)
	debug.ID = "debug"
	debug.Config = map[string]any{"name": strings.Repeat("é", maxVisibleValueBytes)}
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID = "tool"
	props := map[string]any{}
	for i := range 2000 {
		props[strings.Repeat("p", 10)+string(rune('a'+i%26))+strings.Repeat("x", i%50)] = map[string]any{"type": "string"}
	}
	tool.Config = map[string]any{"inputSchema": map[string]any{"type": "object", "properties": props}}
	f.AddNode(*debug)
	f.AddNode(*tool)
	e.save(f)

	view, err := e.reads.GetFlow(context.Background(), EditorActor(), f.ID)
	require.NoError(t, err)
	name := view.Nodes[0].Config["name"].(string)
	assert.LessOrEqual(t, len(name), maxVisibleValueBytes)
	assert.True(t, strings.HasSuffix(name, truncatedMark), "a cut value ends with the truncation marker")
	assert.True(t, strings.HasPrefix(name, "é"))
	assert.Equal(t, truncatedMark, view.Nodes[1].Config["inputSchema"], "a large visible object is replaced by the marker")
}

func TestGetFlowBoundsTheWholeView(t *testing.T) {
	e := newReadsEnv(t)
	f := flow.NewFlow("huge")
	value := strings.Repeat("v", maxVisibleValueBytes-64)
	for i := range maxViewNodes + 5 {
		n := flow.NewNode("process-debug", flow.NodeCategoryProcessing)
		n.Config = map[string]any{"name": value}
		f.AddNode(*n)
		if i > 0 {
			f.AddConnection(*flow.NewConnection(f.Nodes[i-1].ID, "output", n.ID, "input"))
		}
	}
	for range maxViewConnections {
		f.AddConnection(*flow.NewConnection(f.Nodes[0].ID, "output", f.Nodes[1].ID, "input"))
	}
	e.save(f)

	view, err := e.reads.GetFlow(context.Background(), EditorActor(), f.ID)
	require.NoError(t, err)
	assert.True(t, view.Truncated)
	assert.Len(t, view.Nodes, maxViewNodes)
	assert.Len(t, view.Connections, maxViewConnections)
	assert.Equal(t, maxViewNodes+5, view.TotalNodes)
	assert.Equal(t, len(f.Connections), view.TotalConnections)
	omitted := 0
	for _, n := range view.Nodes {
		if n.ConfigOmitted {
			omitted++
			assert.Nil(t, n.Config)
		}
	}
	assert.Positive(t, omitted, "configs beyond the budget are omitted")
	assert.False(t, view.Nodes[0].ConfigOmitted, "the first nodes keep their configs")
	assert.Less(t, len(mustJSON(t, view)), maxViewConfigBytes+maxViewNodes*1024+maxViewConnections*512)
}

func TestGetFlowIsDeterministic(t *testing.T) {
	e := newReadsEnv(t)
	f, _ := canaryFlow(t)
	e.save(f)
	first, err := e.reads.GetFlow(context.Background(), EditorActor(), f.ID)
	require.NoError(t, err)
	for range 5 {
		again, err := e.reads.GetFlow(context.Background(), EditorActor(), f.ID)
		require.NoError(t, err)
		require.Equal(t, mustJSON(t, first), mustJSON(t, again))
	}
}

// A property marked both AgentVisible and Sensitive (a schema bug the
// builtin policy test forbids) stays hidden: Sensitive wins.
func TestConfigViewSensitiveWinsOverVisible(t *testing.T) {
	schema := &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
		"both":    {Type: "string", AgentVisible: true, Sensitive: true},
		"visible": {Type: "number", AgentVisible: true},
		"list":    {Type: "array", AgentVisible: true},
	}}
	red := testRedactor(t, "4242")
	got := configView(red, schema, map[string]any{
		"both":    "credential",
		"visible": float64(14242),
		"list":    []string{"a", "x4242"},
	})
	assert.Equal(t, map[string]any{
		"both":    redactedMark,
		"visible": redactedMark, // its text holds a secret value
		"list":    []any{"a", "x" + redactedMark},
	}, got)
	assert.Equal(t, map[string]any{}, configView(red, nil, nil))
}
