package node

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubToolCaller records the calls it is asked for.
type stubToolCaller struct {
	listed []AgentCaller
	called []string
}

func (s *stubToolCaller) AgentTools(caller AgentCaller) []AgentTool {
	s.listed = append(s.listed, caller)
	return []AgentTool{{Name: "lights_on", Description: "Turns the lights on.", InputSchema: json.RawMessage(`{"type":"object"}`), Risk: RiskExternal, Confirm: ToolConfirmNone}}
}

func (s *stubToolCaller) CallAgentTool(_ context.Context, _ AgentCaller, name string, _ map[string]any) (ToolCallOutcome, error) {
	s.called = append(s.called, name)
	return ToolCallOutcome{Status: ToolCallOK, Result: json.RawMessage(`{"ok":true}`)}, nil
}

// Outside a runtime that gives one, the tool caller lists nothing and
// refuses every call with an error saying why; it is never nil.
func TestToolCallerFromContextDefaultsToNone(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"background": context.Background(),
		"nil caller": WithToolCaller(context.Background(), nil),
		"nil ctx":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			tc := ToolCallerFromContext(ctx)
			require.NotNil(t, tc)
			assert.Empty(t, tc.AgentTools(AgentCaller{FlowID: "f", NodeID: "n", Tools: []string{"lights_on"}}))
			out, err := tc.CallAgentTool(context.Background(), AgentCaller{FlowID: "f", NodeID: "n", Tools: []string{"lights_on"}}, "lights_on", nil)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrNoToolCaller))
			assert.Contains(t, err.Error(), "no flow tools")
			assert.Empty(t, out.Status)
		})
	}
}

func TestToolCallerFromContextReturnsTheRuntimes(t *testing.T) {
	stub := &stubToolCaller{}
	ctx := WithToolCaller(context.Background(), stub)
	tc := ToolCallerFromContext(ctx)
	caller := AgentCaller{FlowID: "f", NodeID: "agent", Tools: []string{"lights_on"}}
	tools := tc.AgentTools(caller)
	require.Len(t, tools, 1)
	assert.Equal(t, "lights_on", tools[0].Name)
	out, err := tc.CallAgentTool(context.Background(), caller, "lights_on", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, ToolCallOK, out.Status)
	assert.Equal(t, []string{"lights_on"}, stub.called)
	assert.Equal(t, []AgentCaller{caller}, stub.listed)
}

// A sandboxed execution (dry-run, node test) never calls tools, whatever
// its context carries.
func TestToolCallerFromContextRefusesInASandbox(t *testing.T) {
	stub := &stubToolCaller{}
	ctx := WithSandbox(WithToolCaller(context.Background(), stub), NewSandbox(1<<10, 1<<20))
	tc := ToolCallerFromContext(ctx)
	assert.Empty(t, tc.AgentTools(AgentCaller{FlowID: "f", NodeID: "n", Tools: []string{"lights_on"}}))
	_, err := tc.CallAgentTool(ctx, AgentCaller{FlowID: "f", NodeID: "n", Tools: []string{"lights_on"}}, "lights_on", nil)
	assert.ErrorIs(t, err, ErrNoToolCaller)
	assert.Empty(t, stub.called)
	assert.Empty(t, stub.listed)
}

func TestNodeInfoFromContext(t *testing.T) {
	assert.Equal(t, NodeInfo{}, NodeInfoFromContext(context.Background()))
	var none context.Context // nil is allowed
	assert.Equal(t, NodeInfo{}, NodeInfoFromContext(none))
	ctx := WithNodeInfo(context.Background(), NodeInfo{FlowID: "flow-1", NodeID: "agent"})
	assert.Equal(t, NodeInfo{FlowID: "flow-1", NodeID: "agent"}, NodeInfoFromContext(ctx))
	inner := WithNodeInfo(ctx, NodeInfo{FlowID: "flow-1", NodeID: "other"})
	assert.Equal(t, "other", NodeInfoFromContext(inner).NodeID, "the innermost wins")
}
