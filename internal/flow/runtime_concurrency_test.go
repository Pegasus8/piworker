package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingNode counts how many times Process is invoked and returns a fixed
// error, so tests can assert on retry behaviour.
type countingNode struct {
	*node.BaseNode
	calls int32
	err   error
}

func (n *countingNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	atomic.AddInt32(&n.calls, 1)
	if n.err != nil {
		return nil, n.err
	}
	out := msg.Clone()
	out.SourcePort = "output"
	return []*Message{out}, nil
}

func (n *countingNode) Ports() (inputs []Port, outputs []Port) {
	return []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		[]Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}

// buildRetryFlow wires a mock trigger that fires exactly one message into a
// countingNode, with a fast retry config for deterministic, quick tests.
func buildRetryFlow(t *testing.T, processErr error) (*FlowRuntime, *countingNode) {
	t.Helper()

	cn := &countingNode{BaseNode: node.NewBaseNode(nil), err: processErr}

	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "mock-trigger", Name: "Mock Trigger", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}},
	}, newMockTriggerNode))
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "counting", Name: "Counting", Category: NodeCategoryOutput,
		Inputs:  []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		Outputs: []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}},
	}, func(config map[string]interface{}) (node.Node, error) { return cn, nil }))

	f := NewFlow("retry")
	trigger := NewNode("mock-trigger", NodeCategoryInput)
	trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
	act := NewNode("counting", NodeCategoryOutput)
	act.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}
	act.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
	f.AddNode(*trigger)
	f.AddNode(*act)
	f.AddConnection(*NewConnection(trigger.ID, "output", act.ID, "input"))

	rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()), WithRetryConfig(RetryConfig{
		MaxRetries: 3, InitialWait: time.Millisecond, MaxWait: 3 * time.Millisecond, Multiplier: 2,
	}))
	require.NoError(t, err)

	mt := rt.triggers[trigger.ID].(*mockTriggerNode)
	mt.sendFunc = func(out chan<- *Message) {
		msg := NewMessage("x", DataTypeString)
		msg.SourcePort = "output"
		select {
		case out <- msg:
		case <-mt.stopCh:
		}
	}

	return rt, cn
}

// TestExecuteNodeRetryOnlyTransient verifies the idempotency-preserving retry
// policy: a plain (permanent) error runs the node exactly once, while an error
// wrapped with node.Transient is retried up to MaxRetries times.
func TestExecuteNodeRetryOnlyTransient(t *testing.T) {
	t.Run("permanent error is not retried", func(t *testing.T) {
		rt, cn := buildRetryFlow(t, errors.New("permanent"))
		require.NoError(t, rt.Start(context.Background()))
		time.Sleep(150 * time.Millisecond)
		require.NoError(t, rt.Stop())
		assert.Equal(t, int32(1), atomic.LoadInt32(&cn.calls),
			"a non-retryable error must execute the node exactly once (no duplicate side effects)")
	})

	t.Run("transient error is retried MaxRetries times", func(t *testing.T) {
		rt, cn := buildRetryFlow(t, node.Transient(errors.New("blip")))
		require.NoError(t, rt.Start(context.Background()))
		time.Sleep(250 * time.Millisecond)
		require.NoError(t, rt.Stop())
		assert.Equal(t, int32(4), atomic.LoadInt32(&cn.calls),
			"a transient error must be retried MaxRetries (3) times, i.e. 4 total attempts")
	})
}

// TestStopDuringRoutingNoPanic exercises the shutdown path that previously
// panicked the whole process: stopping a flow while messages are still being
// routed (send on a closed jobs/output/input channel). It must never panic.
func TestStopDuringRoutingNoPanic(t *testing.T) {
	logger := zerolog.Nop()

	for i := 0; i < 25; i++ {
		reg := createTestRegistry()
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, reg, WithLogger(logger))
		require.NoError(t, err)

		var triggerID string
		for id := range rt.triggers {
			triggerID = id
		}
		mt := rt.triggers[triggerID].(*mockTriggerNode)
		mt.sendFunc = func(out chan<- *Message) {
			for {
				msg := NewMessage("x", DataTypeString)
				msg.SourcePort = "output"
				select {
				case out <- msg:
				case <-mt.stopCh:
					return
				}
			}
		}

		require.NoError(t, rt.Start(context.Background()))
		time.Sleep(5 * time.Millisecond)
		// Stop races with in-flight routing; the assertion is simply that this
		// returns without panicking the process.
		require.NoError(t, rt.Stop())
	}
}
