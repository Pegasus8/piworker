package builtin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolFlowSpec describes a test flow: a trigger-tool "tool", optionally a
// process-transform "shape", and an action-reply "reply".
type toolFlowSpec struct {
	toolName  string
	toolCfg   map[string]any // overrides of validToolConfig
	transform string         // expression; "" means no transform node
	replyMode string         // "" means no reply node
}

func buildToolFlow(spec toolFlowSpec) *flow.Flow {
	f := flow.NewFlow("tool flow")
	f.ID = uuid.NewString()

	cfg := validToolConfig(spec.toolName)
	for k, v := range spec.toolCfg {
		cfg[k] = v
	}
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID, tool.Config = "tool", cfg
	f.AddNode(*tool)

	last := "tool"
	if spec.transform != "" {
		shape := flow.NewNode("process-transform", flow.NodeCategoryProcessing)
		shape.ID, shape.Config = "shape", map[string]any{"expression": spec.transform}
		f.AddNode(*shape)
		f.AddConnection(*flow.NewConnection(last, "output", "shape", "input"))
		last = "shape"
	}
	if spec.replyMode != "" {
		reply := flow.NewNode("action-reply", flow.NodeCategoryOutput)
		reply.ID, reply.Config = "reply", map[string]any{"mode": spec.replyMode}
		f.AddNode(*reply)
		f.AddConnection(*flow.NewConnection(last, "output", "reply", "input"))
	}
	return f
}

func newToolManager(t *testing.T) *flow.RuntimeManager {
	t.Helper()
	m := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(m.StopAll)
	return m
}

func callTool(t *testing.T, m *flow.RuntimeManager, flowID string, args map[string]any) flow.CallResult {
	t.Helper()
	rt, ok := m.GetRuntime(flowID)
	require.True(t, ok, "flow %s is deployed", flowID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := rt.Call(ctx, flow.CallRequest{TriggerNodeID: "tool", Args: args, Actor: "editor", Timeout: 3 * time.Second})
	require.NoError(t, err)
	return res
}

func TestToolFlowCallCompletesWithTheReply(t *testing.T) {
	m := newToolManager(t)
	f := buildToolFlow(toolFlowSpec{
		toolName:  uniqueToolName(),
		transform: `{"door": payload.door, "moved": true, "tool": meta.toolName}`,
		replyMode: "result",
	})
	require.NoError(t, m.Deploy(context.Background(), f))

	res := callTool(t, m, f.ID, map[string]any{"door": "left"})
	assert.Equal(t, flow.CallStatusCompleted, res.Status, "%+v", res)
	assert.NotEmpty(t, res.RunID)
	assert.Nil(t, res.Error)
	tool := f.Nodes[0].Config["toolName"].(string)
	assert.JSONEq(t, `{"door":"left","moved":true,"tool":"`+tool+`"}`, string(res.Result))
}

func TestToolFlowErrorReply(t *testing.T) {
	m := newToolManager(t)
	f := buildToolFlow(toolFlowSpec{
		toolName:  uniqueToolName(),
		transform: `{"reason": "door " + payload.door + " is jammed"}`,
		replyMode: "error",
	})
	require.NoError(t, m.Deploy(context.Background(), f))

	res := callTool(t, m, f.ID, map[string]any{"door": "right"})
	assert.Equal(t, flow.CallStatusError, res.Status, "%+v", res)
	assert.JSONEq(t, `{"reason":"door right is jammed"}`, string(res.Result))
}

// Without an action-reply the run still finishes and the call says so.
func TestToolFlowWithoutReplyCompletesWithoutResult(t *testing.T) {
	m := newToolManager(t)
	f := buildToolFlow(toolFlowSpec{toolName: uniqueToolName(), transform: "payload"})
	require.NoError(t, m.Deploy(context.Background(), f))

	res := callTool(t, m, f.ID, map[string]any{"door": "left"})
	assert.Equal(t, flow.CallStatusCompletedNoReply, res.Status, "%+v", res)
	assert.Empty(t, res.Result)
}

// Arguments outside the schema never start a run, even when a caller skips
// validation.
func TestToolFlowRefusesInvalidArguments(t *testing.T) {
	m := newToolManager(t)
	f := buildToolFlow(toolFlowSpec{toolName: uniqueToolName(), replyMode: "result"})
	require.NoError(t, m.Deploy(context.Background(), f))
	rt, _ := m.GetRuntime(f.ID)

	res, err := rt.Call(context.Background(), flow.CallRequest{TriggerNodeID: "tool", Args: map[string]any{"door": "garden"}, Timeout: time.Second})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tool arguments")
	assert.Empty(t, res.RunID, "no run was started")
}

func TestToolFlowWithoutEdgesFailsDeploy(t *testing.T) {
	m := newToolManager(t)
	name := uniqueToolName()
	f := buildToolFlow(toolFlowSpec{toolName: name})

	err := m.Deploy(context.Background(), f)
	require.Error(t, err)
	var verr *flow.ValidationError
	assert.True(t, errors.As(err, &verr), "%v", err)
	assert.Contains(t, err.Error(), "no active connection")
	_, running := m.GetRuntime(f.ID)
	assert.False(t, running)
	assert.False(t, toolNames.taken(name), "a failed deploy holds no name")
}

func TestToolFlowDuplicateNameConflicts(t *testing.T) {
	m := newToolManager(t)
	name := uniqueToolName()
	first := buildToolFlow(toolFlowSpec{toolName: name, replyMode: "result"})
	second := buildToolFlow(toolFlowSpec{toolName: name, replyMode: "result"})

	require.NoError(t, m.Deploy(context.Background(), first))
	err := m.Deploy(context.Background(), second)
	require.Error(t, err)
	assert.True(t, errors.Is(err, flow.ErrResourceConflict), "%v", err)
	_, running := m.GetRuntime(second.ID)
	assert.False(t, running)

	// The first flow still answers under that name.
	assert.Equal(t, flow.CallStatusCompleted, callTool(t, m, first.ID, map[string]any{"door": "left"}).Status)

	require.NoError(t, m.Undeploy(first.ID))
	require.NoError(t, m.Deploy(context.Background(), second))
	assert.Equal(t, flow.CallStatusCompleted, callTool(t, m, second.ID, map[string]any{"door": "left"}).Status)
}

func TestToolFlowRestart(t *testing.T) {
	m := newToolManager(t)
	f := buildToolFlow(toolFlowSpec{toolName: uniqueToolName(), replyMode: "result"})
	for i := 0; i < 3; i++ {
		require.NoError(t, m.Deploy(context.Background(), f), "deploy %d", i)
		res := callTool(t, m, f.ID, map[string]any{"door": "right"})
		assert.Equal(t, flow.CallStatusCompleted, res.Status)
		assert.JSONEq(t, `{"door":"right"}`, string(res.Result))
		require.NoError(t, m.Undeploy(f.ID))
	}
}

func TestToolFlowInvalidSchemaIsInvalidConfig(t *testing.T) {
	m := newToolManager(t)
	f := buildToolFlow(toolFlowSpec{
		toolName:  uniqueToolName(),
		toolCfg:   map[string]any{"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"cmd": map[string]any{"type": "string", "pattern": "[a-z]+"}}}},
		replyMode: "result",
	})
	err := m.Deploy(context.Background(), f)
	require.Error(t, err)
	assert.True(t, errors.Is(err, flow.ErrInvalidNodeConfig), "%v", err)
	assert.Contains(t, err.Error(), "/properties/cmd/pattern")
}

// A trigger-tool exposes its parsed spec on the running instance, which is
// what the risk analysis and RuntimeManager.Tools() read.
func TestToolFlowSpecReadableFromTheNode(t *testing.T) {
	name := uniqueToolName()
	f := buildToolFlow(toolFlowSpec{toolName: name, replyMode: "result", toolCfg: map[string]any{"confirm": "admin"}})
	n, err := node.DefaultRegistry.Create(f.Nodes[0].Type, f.Nodes[0].Config)
	require.NoError(t, err)
	provider, ok := n.(node.ToolProvider)
	require.True(t, ok)
	assert.Equal(t, name, provider.ToolSpec().Name)
	assert.Equal(t, node.ToolConfirmAdmin, provider.ToolSpec().Confirm)
}
