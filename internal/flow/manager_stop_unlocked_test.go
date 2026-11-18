package flow

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// managerReaderNode reads the manager from inside Process once its context
// ends, as an agent node does when a stop released the tool call it waited
// for and it goes on to its next tool call (Tools, GetRuntime).
type managerReaderNode struct {
	*node.BaseNode
	manager atomic.Pointer[RuntimeManager]
	entered atomic.Bool
}

func (n *managerReaderNode) Process(ctx context.Context, _ *Message) ([]*Message, error) {
	n.entered.Store(true)
	<-ctx.Done()
	m := n.manager.Load()
	m.Tools()
	m.GetRuntime("another-flow")
	return nil, ctx.Err()
}

func (n *managerReaderNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

// readerSetup deploys emit -> reader with a long stop timeout and waits
// until the reader runs.
func readerSetup(t *testing.T) (*RuntimeManager, *FlowRuntime, *Flow) {
	t.Helper()
	reader := &managerReaderNode{BaseNode: node.NewBaseNode(nil)}
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"reader": func(map[string]interface{}) (node.Node, error) { return reader, nil },
	})
	f, _ := chainFlow("emit", "reader")
	m := NewRuntimeManager(reg, zerolog.Nop(), WithRuntimeOptions(WithStopTimeout(3*time.Second)))
	reader.manager.Store(m)
	require.NoError(t, m.Deploy(context.Background(), f))
	require.Eventually(t, reader.entered.Load, time.Second, 2*time.Millisecond)
	rt, ok := m.GetRuntime(f.ID)
	require.True(t, ok)
	return m, rt, f
}

// Undeploy waits for the flow to drain without holding the manager's lock:
// a node that reads the manager after its context ended (an agent node
// going on to its next tool call) is not stuck behind the stop, so the
// flow drains at once instead of at the stop timeout.
func TestUndeployDrainsANodeThatReadsTheManager(t *testing.T) {
	m, rt, f := readerSetup(t)
	start := time.Now()
	require.NoError(t, m.Undeploy(f.ID))
	assert.Less(t, time.Since(start), time.Second, "the stop waited for its own lock")
	assert.True(t, rt.Drained())
	_, ok := m.GetRuntime(f.ID)
	assert.False(t, ok)
	require.NoError(t, m.Deploy(context.Background(), f), "a drained flow deploys again")
	m.StopAll()
}

// StopAll, likewise.
func TestStopAllDrainsANodeThatReadsTheManager(t *testing.T) {
	m, rt, _ := readerSetup(t)
	start := time.Now()
	m.StopAll()
	assert.Less(t, time.Since(start), time.Second, "the stop waited for its own lock")
	assert.True(t, rt.Drained())
	assert.Empty(t, m.ListRunning())
}

// A flow being stopped by StopAll can be undeployed meanwhile (and the
// other way round): it is stopped once, the running gauge falls by one, and
// the flow cannot be deployed again before it drained.
func TestConcurrentStopsStopAFlowOnce(t *testing.T) {
	stubborn := &stubbornNode{BaseNode: node.NewBaseNode(nil), release: make(chan struct{})}
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"stubborn": func(map[string]interface{}) (node.Node, error) { return stubborn, nil },
	})
	f, _ := chainFlow("emit", "stubborn")
	m := NewRuntimeManager(reg, zerolog.Nop(), WithRuntimeOptions(WithStopTimeout(100*time.Millisecond)))
	before := gaugeValue(t)
	require.NoError(t, m.Deploy(context.Background(), f))
	require.Eventually(t, stubborn.entered.Load, time.Second, 2*time.Millisecond)

	var wg sync.WaitGroup
	wg.Go(m.StopAll)
	wg.Go(func() { assert.NoError(t, m.Undeploy(f.ID)) })
	wg.Go(m.StopAll)
	wg.Wait()

	assert.Equal(t, before, gaugeValue(t), "stopped once")
	assert.Empty(t, m.ListRunning())
	assert.ErrorIs(t, m.Deploy(context.Background(), f), ErrFlowStillStopping)
	close(stubborn.release)
	require.Eventually(t, func() bool { return m.Deploy(context.Background(), f) == nil }, 2*time.Second, 10*time.Millisecond)
	m.StopAll()
}

// A second Stop of a runtime being stopped does not stop it again.
func TestStopOfAStoppingRuntime(t *testing.T) {
	stubborn := &stubbornNode{BaseNode: node.NewBaseNode(nil), release: make(chan struct{})}
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"stubborn": func(map[string]interface{}) (node.Node, error) { return stubborn, nil },
	})
	f, _ := chainFlow("emit", "stubborn")
	rt, err := NewFlowRuntime(f, reg, WithStopTimeout(200*time.Millisecond))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	require.Eventually(t, stubborn.entered.Load, time.Second, 2*time.Millisecond)

	first := make(chan error, 1)
	go func() { first <- rt.Stop() }()
	require.Eventually(t, func() bool { return rt.ctx.Err() != nil }, time.Second, time.Millisecond, "the first stop began")
	assert.ErrorIs(t, rt.Stop(), ErrFlowNotRunning, "already stopping")
	require.NoError(t, <-first)
	close(stubborn.release)
	require.Eventually(t, rt.Drained, 2*time.Second, 5*time.Millisecond)
}
