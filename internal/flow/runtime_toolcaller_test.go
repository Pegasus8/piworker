package flow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeToolCaller is a node.ToolCaller that counts the calls it gets.
type fakeToolCaller struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeToolCaller) AgentTools(node.AgentCaller) []node.AgentTool {
	return []node.AgentTool{{Name: "lights_on"}}
}

func (f *fakeToolCaller) CallAgentTool(context.Context, node.AgentCaller, string, map[string]any) (node.ToolCallOutcome, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return node.ToolCallOutcome{Status: node.ToolCallOK}, nil
}

func (f *fakeToolCaller) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// startRecorded runs the ctx-trigger → ctx-process flow of rt until the
// process node ran once, and returns the trigger's and the node's contexts
// and node IDs.
func startRecorded(t *testing.T, rt *FlowRuntime, rec *ctxRecorder) (startCtx, processCtx context.Context) {
	t.Helper()
	require.NoError(t, rt.Start(context.Background()))
	t.Cleanup(func() { _ = rt.Stop() })
	require.Eventually(t, func() bool { return len(rec.processCtxs()) == 1 }, 2*time.Second, 5*time.Millisecond)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.startCtx, rec.processed[0]
}

// Every node's context names the node and its flow (node.NodeInfo), trigger
// Start and Process alike, and carries the runtime's tool caller.
func TestRuntimeGivesNodesTheirIdentityAndToolCaller(t *testing.T) {
	rec := &ctxRecorder{}
	f, ids := chainFlow("ctx-trigger", "ctx-process")
	tc := &fakeToolCaller{}
	rt, err := NewFlowRuntime(f, ctxRegistry(t, rec), WithToolCaller(tc))
	require.NoError(t, err)
	startCtx, processCtx := startRecorded(t, rt, rec)

	assert.Equal(t, node.NodeInfo{FlowID: f.ID, NodeID: ids[0]}, node.NodeInfoFromContext(startCtx))
	assert.Equal(t, node.NodeInfo{FlowID: f.ID, NodeID: ids[1]}, node.NodeInfoFromContext(processCtx))
	for _, ctx := range []context.Context{startCtx, processCtx} {
		assert.Same(t, tc, node.ToolCallerFromContext(ctx))
	}
	require.NoError(t, rt.Stop())
	assert.Error(t, processCtx.Err(), "the node's context is still the runtime's: Stop cancels it")
	assert.Error(t, startCtx.Err())
}

// Without a tool caller the nodes get the one that lists nothing and refuses
// every call; they still know who they are.
func TestRuntimeWithoutToolCaller(t *testing.T) {
	rec := &ctxRecorder{}
	f, ids := chainFlow("ctx-trigger", "ctx-process")
	rt, err := NewFlowRuntime(f, ctxRegistry(t, rec))
	require.NoError(t, err)
	_, processCtx := startRecorded(t, rt, rec)

	assert.Equal(t, node.NodeInfo{FlowID: f.ID, NodeID: ids[1]}, node.NodeInfoFromContext(processCtx))
	tc := node.ToolCallerFromContext(processCtx)
	assert.Empty(t, tc.AgentTools(node.AgentCaller{FlowID: f.ID, NodeID: ids[1], Tools: []string{"lights_on"}}))
	_, err = tc.CallAgentTool(processCtx, node.AgentCaller{FlowID: f.ID, NodeID: ids[1], Tools: []string{"lights_on"}}, "lights_on", nil)
	assert.ErrorIs(t, err, node.ErrNoToolCaller)
}

// A node context wrapper that sandboxes the runtime (the dry-run's) keeps
// the tool caller away from the nodes, whatever the runtime was given.
func TestSandboxedRuntimeHasNoToolCaller(t *testing.T) {
	rec := &ctxRecorder{}
	f, _ := chainFlow("ctx-trigger", "ctx-process")
	tc := &fakeToolCaller{}
	rt, err := NewFlowRuntime(f, ctxRegistry(t, rec), WithToolCaller(tc),
		WithNodeContext(func(ctx context.Context) context.Context { return node.WithSandbox(ctx, newSandbox()) }))
	require.NoError(t, err)
	startCtx, processCtx := startRecorded(t, rt, rec)
	for _, ctx := range []context.Context{startCtx, processCtx} {
		_, err := node.ToolCallerFromContext(ctx).CallAgentTool(ctx, node.AgentCaller{}, "lights_on", nil)
		assert.ErrorIs(t, err, node.ErrNoToolCaller)
	}
	assert.Zero(t, tc.count())
}

// The composition root gives the manager its tool caller once the tool
// service exists (the service needs the manager first): every flow deployed
// afterwards carries it.
func TestManagerSetToolCaller(t *testing.T) {
	rec := &ctxRecorder{}
	reg := ctxRegistry(t, rec)
	m := NewRuntimeManager(reg, zerolog.Nop())
	t.Cleanup(m.StopAll)
	tc := &fakeToolCaller{}
	m.SetToolCaller(tc)

	f, ids := chainFlow("ctx-trigger", "ctx-process")
	require.NoError(t, m.Deploy(context.Background(), f))
	require.Eventually(t, func() bool { return len(rec.processCtxs()) == 1 }, 2*time.Second, 5*time.Millisecond)
	ctx := rec.processCtxs()[0]
	assert.Same(t, tc, node.ToolCallerFromContext(ctx))
	assert.Equal(t, node.NodeInfo{FlowID: f.ID, NodeID: ids[1]}, node.NodeInfoFromContext(ctx))
}

// A manager option works too, when the caller exists before the manager.
func TestManagerRuntimeOptionToolCaller(t *testing.T) {
	rec := &ctxRecorder{}
	tc := &fakeToolCaller{}
	m := NewRuntimeManager(ctxRegistry(t, rec), zerolog.Nop(), WithRuntimeOptions(WithToolCaller(tc)))
	t.Cleanup(m.StopAll)
	f, _ := chainFlow("ctx-trigger", "ctx-process")
	require.NoError(t, m.Deploy(context.Background(), f))
	require.Eventually(t, func() bool { return len(rec.processCtxs()) == 1 }, 2*time.Second, 5*time.Millisecond)
	assert.Same(t, tc, node.ToolCallerFromContext(rec.processCtxs()[0]))
}
