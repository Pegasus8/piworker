package flow

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Admission (ADR-0037): a trigger's message enters the flow only while the
// flow has room for it, so overload is refused before a run starts instead
// of dropping messages in the middle of one.

// gatedCounter is a node that blocks until its gate opens and counts what it
// processed.
func gatedCounter(gate <-chan struct{}, done *atomic.Int32) node.NodeFactory {
	return funcFactory(func(ctx context.Context, _ *Message) ([]*Message, error) {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		done.Add(1)
		return nil, nil
	})
}

// A burst larger than a node's queue used to be dropped at that node. Now
// the trigger waits and every message runs.
func TestBurstLargerThanAQueueIsNotDropped(t *testing.T) {
	const burst = 40
	gate := make(chan struct{})
	var done atomic.Int32
	reg := testRegistry(t, burst, map[string]node.NodeFactory{"slow": gatedCounter(gate, &done)})
	f, _ := chainFlow("emit", "slow")
	rt := startRuntime(t, f, reg, WithNodeQueueLimit(4))

	// While the node is stuck the flow holds at most what it admitted.
	time.Sleep(100 * time.Millisecond)
	assert.LessOrEqual(t, rt.Inflight(), int64(4), "admission is bounded by the queue limit")
	assert.Zero(t, rt.DroppedMessages())

	close(gate)
	require.Eventually(t, func() bool { return done.Load() == burst }, 3*time.Second, 5*time.Millisecond,
		"every message of the burst runs (%d of %d)", done.Load(), burst)
	assert.Zero(t, rt.DroppedMessages(), "nothing was dropped")
	require.Eventually(t, func() bool { return rt.Inflight() == 0 }, time.Second, 5*time.Millisecond)
}

// WithMaxInflight sets the bound; 0 removes it (the node queues then drop
// what does not fit, as before).
func TestWithMaxInflight(t *testing.T) {
	gate := make(chan struct{})
	var done atomic.Int32
	reg := testRegistry(t, 20, map[string]node.NodeFactory{"slow": gatedCounter(gate, &done)})

	f, _ := chainFlow("emit", "slow")
	rt := startRuntime(t, f, reg, WithMaxInflight(2))
	time.Sleep(100 * time.Millisecond)
	assert.LessOrEqual(t, rt.Inflight(), int64(2))

	f2, _ := chainFlow("emit", "slow")
	open := startRuntime(t, f2, reg, WithNodeQueueLimit(3), WithMaxInflight(0))
	require.Eventually(t, func() bool { return open.DroppedMessages() > 0 }, 2*time.Second, 5*time.Millisecond,
		"without admission a full queue drops")
	close(gate)
}

// A flow stops promptly while its trigger waits for room.
func TestStopWhileATriggerWaitsForRoom(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	var done atomic.Int32
	reg := testRegistry(t, 50, map[string]node.NodeFactory{"slow": gatedCounter(gate, &done)})
	f, _ := chainFlow("emit", "slow")
	rt, err := NewFlowRuntime(f, reg, WithMaxInflight(1), WithStopTimeout(2*time.Second))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	require.NoError(t, rt.Stop())
	assert.Less(t, time.Since(start), time.Second)
	assert.True(t, rt.Drained())
}
