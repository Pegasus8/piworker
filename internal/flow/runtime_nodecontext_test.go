package flow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nodeCtxKey struct{}

// ctxRecorder remembers the contexts its trigger Start and its processing
// node Process were given.
type ctxRecorder struct {
	mu        sync.Mutex
	startCtx  context.Context
	processed []context.Context
}

func (r *ctxRecorder) processCtxs() []context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]context.Context(nil), r.processed...)
}

type ctxTrigger struct {
	*node.BaseNode
	rec *ctxRecorder
}

func (n *ctxTrigger) Process(context.Context, *Message) ([]*Message, error) { return nil, nil }
func (n *ctxTrigger) Ports() (inputs, outputs []Port) {
	return nil, []Port{{ID: "output", DataType: DataTypeAny}}
}
func (n *ctxTrigger) Stop() error { return nil }
func (n *ctxTrigger) Start(ctx context.Context, out chan<- *Message) error {
	n.rec.mu.Lock()
	n.rec.startCtx = ctx
	n.rec.mu.Unlock()
	msg := NewMessage("go", DataTypeString)
	msg.SourcePort = "output"
	out <- msg
	return nil
}

type ctxProcess struct {
	*node.BaseNode
	rec *ctxRecorder
}

func (n *ctxProcess) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	n.rec.mu.Lock()
	n.rec.processed = append(n.rec.processed, ctx)
	n.rec.mu.Unlock()
	return nil, nil
}
func (n *ctxProcess) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

func ctxRegistry(t *testing.T, rec *ctxRecorder) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "ctx-trigger", Category: NodeCategoryInput, Risk: node.RiskSafe,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]interface{}) (node.Node, error) {
			return &ctxTrigger{BaseNode: node.NewBaseNode(cfg), rec: rec}, nil
		}))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "ctx-process", Category: NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]interface{}) (node.Node, error) {
			return &ctxProcess{BaseNode: node.NewBaseNode(cfg), rec: rec}, nil
		}))
	return reg
}

// TestWithNodeContextWrapsStartAndProcess: the wrapper's values reach the
// context of every trigger Start and every Process of the runtime, next to
// the runtime's own (the call registry for action-reply).
func TestWithNodeContextWrapsStartAndProcess(t *testing.T) {
	rec := &ctxRecorder{}
	f, _ := chainFlow("ctx-trigger", "ctx-process")
	rt, err := NewFlowRuntime(f, ctxRegistry(t, rec), WithNodeContext(func(ctx context.Context) context.Context {
		return context.WithValue(ctx, nodeCtxKey{}, "wrapped")
	}))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	defer func() { _ = rt.Stop() }()

	require.Eventually(t, func() bool { return len(rec.processCtxs()) == 1 }, 2*time.Second, 5*time.Millisecond)
	rec.mu.Lock()
	startCtx := rec.startCtx
	rec.mu.Unlock()
	for _, ctx := range []context.Context{startCtx, rec.processCtxs()[0]} {
		assert.Equal(t, "wrapped", ctx.Value(nodeCtxKey{}))
		assert.NotNil(t, ctx.Value(callRegistryKey{}), "the runtime's own values stay")
		assert.NotNil(t, flowvars.FlowFromContext(ctx), "a flow store is always present")
	}
	require.NoError(t, rt.Stop())
	assert.Error(t, rec.processCtxs()[0].Err(), "the node context is still cancelled by Stop")
}

// countingFlowPersister counts how often the flow variable stores are loaded.
type countingFlowPersister struct{}

func (countingFlowPersister) LoadVars() (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (countingFlowPersister) SaveVar(string, interface{}) error { return nil }
func (countingFlowPersister) DeleteVar(string) error            { return nil }

// TestWithNodeContextFlowStoreSkipsPersistentStore: when the wrapper supplies
// the flow variable store, the runtime neither loads nor caches the flow's
// persistent store (a sandboxed run must not touch it), and the nodes get the
// supplied one.
func TestWithNodeContextFlowStoreSkipsPersistentStore(t *testing.T) {
	var mu sync.Mutex
	loads := map[string]int{}
	prev := flowvars.DefaultFlows.Factory
	flowvars.DefaultFlows.Factory = func(id string) flowvars.Persister {
		mu.Lock()
		loads[id]++
		mu.Unlock()
		return countingFlowPersister{}
	}
	t.Cleanup(func() { flowvars.DefaultFlows.Factory = prev })
	loadsOf := func(id string) int { mu.Lock(); defer mu.Unlock(); return loads[id] }

	rec := &ctxRecorder{}
	f, _ := chainFlow("ctx-trigger", "ctx-process")
	t.Cleanup(func() { flowvars.DefaultFlows.Forget(f.ID) })
	isolated := flowvars.NewStore()
	rt, err := NewFlowRuntime(f, ctxRegistry(t, rec), WithNodeContext(func(ctx context.Context) context.Context {
		return flowvars.WithFlowStore(ctx, isolated)
	}))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	require.Eventually(t, func() bool { return len(rec.processCtxs()) == 1 }, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, rt.Stop())

	assert.Same(t, isolated, flowvars.FlowFromContext(rec.processCtxs()[0]))
	assert.Zero(t, loadsOf(f.ID), "the persistent flow store was not loaded")

	// Without a wrapper the runtime still loads the persistent store.
	rec2 := &ctxRecorder{}
	f2, _ := chainFlow("ctx-trigger", "ctx-process")
	t.Cleanup(func() { flowvars.DefaultFlows.Forget(f2.ID) })
	rt2, err := NewFlowRuntime(f2, ctxRegistry(t, rec2))
	require.NoError(t, err)
	require.NoError(t, rt2.Start(context.Background()))
	require.NoError(t, rt2.Stop())
	assert.Equal(t, 1, loadsOf(f2.ID))
}
