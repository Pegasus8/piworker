package flow

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubbornNode ignores context cancellation and only returns once released,
// like a node blocked in a syscall without a deadline.
type stubbornNode struct {
	*node.BaseNode
	entered atomic.Bool
	release chan struct{}
}

func (n *stubbornNode) Process(context.Context, *Message) ([]*Message, error) {
	n.entered.Store(true)
	<-n.release
	return nil, nil
}
func (n *stubbornNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

// TestRedeployRefusedWhileOldInstanceDrains: when Stop times out with an
// executor still running, the same flow must not be deployed a second time
// until the old instance has fully drained.
func TestRedeployRefusedWhileOldInstanceDrains(t *testing.T) {
	stubborn := &stubbornNode{BaseNode: node.NewBaseNode(nil), release: make(chan struct{})}
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"stubborn": func(map[string]interface{}) (node.Node, error) { return stubborn, nil },
	})
	f, _ := chainFlow("emit", "stubborn")

	m := NewRuntimeManager(reg, zerolog.Nop(), WithRuntimeOptions(WithStopTimeout(30*time.Millisecond)))
	defer m.StopAll()

	require.NoError(t, m.Deploy(context.Background(), f))
	require.Eventually(t, stubborn.entered.Load, time.Second, 2*time.Millisecond)

	require.NoError(t, m.Undeploy(f.ID))

	err := m.Deploy(context.Background(), f)
	require.ErrorIs(t, err, ErrFlowStillStopping, "the previous instance is still executing")

	close(stubborn.release)
	require.Eventually(t, func() bool { return m.Deploy(context.Background(), f) == nil }, 2*time.Second, 10*time.Millisecond,
		"once the old instance drains the flow can be deployed again")
}

// TestRedeployAfterStopAllRefusedWhileDraining covers the StopAll path, which
// keeps stopped runtimes in the manager.
func TestRedeployAfterStopAllRefusedWhileDraining(t *testing.T) {
	stubborn := &stubbornNode{BaseNode: node.NewBaseNode(nil), release: make(chan struct{})}
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"stubborn": func(map[string]interface{}) (node.Node, error) { return stubborn, nil },
	})
	f, _ := chainFlow("emit", "stubborn")

	m := NewRuntimeManager(reg, zerolog.Nop(), WithRuntimeOptions(WithStopTimeout(30*time.Millisecond)))
	require.NoError(t, m.Deploy(context.Background(), f))
	require.Eventually(t, stubborn.entered.Load, time.Second, 2*time.Millisecond)
	m.StopAll()

	rt, ok := m.GetRuntime(f.ID)
	require.True(t, ok)
	assert.False(t, rt.Drained())
	assert.ErrorIs(t, m.Deploy(context.Background(), f), ErrFlowStillStopping)

	close(stubborn.release)
	require.Eventually(t, rt.Drained, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, m.Deploy(context.Background(), f))
	m.StopAll()
}
