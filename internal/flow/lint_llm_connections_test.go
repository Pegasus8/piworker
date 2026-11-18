package flow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// llmConnNode names an LLM connection in its "connection" setting, like
// process-llm (node.LLMConnectionRequirer).
type llmConnNode struct{ name string }

func (n *llmConnNode) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}
func (n *llmConnNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, nil
}
func (n *llmConnNode) Validate() error { return nil }
func (n *llmConnNode) RequiredLLMConnections() map[string]string {
	return map[string]string{"connection": n.name}
}

// llmConnRegistry has a settings-free trigger and "process-llm-test", which
// names an LLM connection.
func llmConnRegistry(t *testing.T) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "trigger-x", Name: "X", Description: "x", Category: types.NodeCategoryInput,
		Outputs: []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}, Risk: node.RiskSafe},
		func(map[string]any) (node.Node, error) { return connTrigger{}, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "process-llm-test", Name: "L", Description: "l", Category: types.NodeCategoryProcessing,
		Inputs: []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, Risk: node.RiskExternal,
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"connection": {Type: "string", Title: "Connection", AgentVisible: true, Destination: true, Options: node.OptionsLLMConnections}}}},
		func(cfg map[string]any) (node.Node, error) {
			s, _ := cfg["connection"].(string)
			return &llmConnNode{name: s}, nil
		}))
	return reg
}

func llmConnFlow(nodes ...flow.Node) *flow.Flow {
	f := flow.NewFlow("llm conns")
	trig := flow.NewNode("trigger-x", flow.NodeCategoryInput)
	trig.ID = "t"
	trig.Outputs = []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
	f.Nodes = append([]flow.Node{*trig}, nodes...)
	for _, n := range nodes {
		f.Connections = append(f.Connections, lintConn("c-"+n.ID, "t", "output", n.ID, "input"))
	}
	return f
}

func llmConnNodeDef(id, connection string) flow.Node {
	n := flow.NewNode("process-llm-test", flow.NodeCategoryProcessing)
	n.ID = id
	n.Inputs = []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}
	n.Config = map[string]any{"connection": connection}
	return *n
}

// Deploy and Lint refuse a node naming an LLM connection that is not
// defined (node.LLMConnectionRequirer), in the store of the context
// (llm.FromContext), with the same collector; like an undefined broker
// connection it is an environment issue: it blocks a deploy, not a dry-run
// or a draft (the admin may define it).
func TestLintAndDeployCheckRequiredLLMConnections(t *testing.T) {
	reg := llmConnRegistry(t)
	f := llmConnFlow(llmConnNodeDef("ask", "pw_lint_claude"))

	empty := llm.WithStore(context.Background(), llm.NewStore())
	issues := flow.LintContext(empty, f, reg)
	require.Equal(t, []string{"error:" + flow.IssueUndefinedLLMConnection + "@/nodes/ask/config/connection"}, issueKeys(issues))
	assert.Equal(t, "undefined_llm_connection", flow.IssueUndefinedLLMConnection, "codes are stable")
	assert.Contains(t, issues[0].Message, `LLM connection "pw_lint_claude"`)
	assert.True(t, flow.EnvironmentIssue(issues[0]))
	assert.Empty(t, flow.BlockingIssues(issues))

	m := flow.NewRuntimeManager(reg, zerolog.Nop())
	err := m.Deploy(empty, f)
	require.Error(t, err)
	var verr *flow.ValidationError
	assert.ErrorAs(t, err, &verr, "400, not 500")
	assert.Contains(t, err.Error(), `"pw_lint_claude"`)
	assert.Contains(t, err.Error(), "node ask (process-llm-test)")
	_, running := m.GetRuntime(f.ID)
	assert.False(t, running)

	store := llm.NewStore()
	require.NoError(t, store.Set(llm.Connection{Name: "pw_lint_claude", Provider: llm.ProviderOpenAI, BaseURL: "http://127.0.0.1:1/v1"}))
	defined := llm.WithStore(context.Background(), store)
	assert.Empty(t, flow.LintContext(defined, f, reg))
	require.NoError(t, m.Deploy(defined, f))
	require.NoError(t, m.Undeploy(f.ID))
}

// Lint without a context store checks the process-wide one
// (llm.DefaultStore), the store a deploy from the editor resolves in.
func TestLintChecksTheDefaultLLMStore(t *testing.T) {
	reg := llmConnRegistry(t)
	f := llmConnFlow(llmConnNodeDef("ask", "pw_lint_default_only"))
	require.Equal(t, []string{"error:" + flow.IssueUndefinedLLMConnection + "@/nodes/ask/config/connection"}, issueKeys(flow.Lint(f, reg)))

	require.NoError(t, llm.DefaultStore.Set(llm.Connection{Name: "pw_lint_default_only", Provider: llm.ProviderOpenAI}))
	t.Cleanup(func() { _ = llm.DefaultStore.Delete("pw_lint_default_only") })
	assert.Empty(t, flow.Lint(f, reg))
}

// A running flow lists the LLM connections its enabled nodes name, sorted,
// each once (the API refuses to delete one while a running flow uses it);
// disabled nodes are not built, so they are left out.
func TestRuntimeLLMConnectionsUsed(t *testing.T) {
	reg := llmConnRegistry(t)
	store := llm.NewStore()
	for _, name := range []string{"claude", "ollama", "unused", "off"} {
		require.NoError(t, store.Set(llm.Connection{Name: name, Provider: llm.ProviderOpenAI}))
	}
	off := llmConnNodeDef("off", "off")
	off.Enabled = false
	f := llmConnFlow(llmConnNodeDef("a", "ollama"), llmConnNodeDef("b", "claude"), llmConnNodeDef("c", "ollama"), off)

	m := flow.NewRuntimeManager(reg, zerolog.Nop())
	require.NoError(t, m.Deploy(llm.WithStore(context.Background(), store), f))
	t.Cleanup(m.StopAll)
	rt, ok := m.GetRuntime(f.ID)
	require.True(t, ok)
	assert.Equal(t, []string{"claude", "ollama"}, rt.LLMConnectionsUsed())
	assert.Empty(t, rt.ConnectionsUsed(), "LLM connections are not broker connections")
}

// classifyNode is a test type whose "categories" setting adds output ports
// (like process-llm-classify), and whose factory refuses a config without
// a connection.
type classifyNode struct{ *node.BaseNode }

func classifyInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type: "process-classify-test", Name: "Classify", Description: "c", Category: types.NodeCategoryProcessing, Risk: node.RiskExternal,
		Inputs:      []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
		Outputs:     []types.Port{{ID: "other", Name: "Other", DataType: types.DataTypeAny}},
		OutputsFrom: "categories",
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"categories": {Type: "array", Title: "Categories", AgentVisible: true},
			"connection": {Type: "string", Title: "Connection", AgentVisible: true},
		}},
	}
}

func (n *classifyNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	return []*types.Message{msg}, nil
}
func (n *classifyNode) Ports() ([]types.Port, []types.Port) {
	info := classifyInfo()
	return info.Inputs, node.ConfigOutputs(info, n.GetConfig())
}
func (n *classifyNode) Validate() error { return nil }

// A node that cannot be instantiated (its config is refused) still has the
// ports its settings give it: Lint reports the config, not every edge that
// leaves one of its category ports as connection_unknown_port. A port the
// settings do not give is still unknown.
func TestLintPortsOfANodeThatCannotBeBuiltFollowItsSettings(t *testing.T) {
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "trigger-x", Name: "X", Description: "x", Category: types.NodeCategoryInput,
		Outputs: []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}, Risk: node.RiskSafe},
		func(map[string]any) (node.Node, error) { return connTrigger{}, nil }))
	require.NoError(t, reg.Register(classifyInfo(), func(cfg map[string]any) (node.Node, error) {
		if s, _ := cfg["connection"].(string); s == "" {
			return nil, errors.New("connection is required")
		}
		return &classifyNode{BaseNode: node.NewBaseNode(cfg)}, nil
	}))
	// The sinks are classifiers without categories: only "other".
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "process-llm-test", Name: "L", Description: "l", Category: types.NodeCategoryProcessing,
		Inputs: []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, Risk: node.RiskSafe},
		func(map[string]any) (node.Node, error) { return &classifyNode{BaseNode: node.NewBaseNode(nil)}, nil }))

	cls := flow.NewNode("process-classify-test", flow.NodeCategoryProcessing)
	cls.ID = "cls"
	cls.Config = map[string]any{"categories": []any{"bug", "question"}} // no connection: refused
	f := llmConnFlow(*cls, llmConnNodeDef("bugs", ""), llmConnNodeDef("misc", ""), llmConnNodeDef("praise", ""))
	f.Connections = []flow.Connection{
		lintConn("c-cls", "t", "output", "cls", "input"),
		lintConn("c-bug", "cls", "bug", "bugs", "input"),
		lintConn("c-other", "cls", "other", "misc", "input"),
		lintConn("c-praise", "cls", "praise", "praise", "input"),
	}
	assert.Equal(t, []string{
		"error:" + flow.IssueInvalidConfig + "@/nodes/cls",
		"error:" + flow.IssueConnectionUnknownPort + "@/connections/c-praise",
	}, issueKeys(flow.Lint(f, reg)))
}
