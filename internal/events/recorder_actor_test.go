package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withActor(e flow.NodeEvent, actor any) flow.NodeEvent {
	in := e.Input.Clone()
	in.SetMeta("actor", actor)
	e.Input = in
	return e
}

func TestRecorderRecordsRunActor(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()))

	rec.persist(withActor(runningEvent("called", "f1", "n1", "x"), "token:bot"), time.Now())
	rec.persist(runningEvent("plain", "f1", "n1", "x"), time.Now())

	assert.Equal(t, "token:bot", store.getRun("called").Actor)
	assert.Equal(t, "", store.getRun("plain").Actor)
}

func TestRecorderIgnoresNonStringActor(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()))

	rec.persist(withActor(runningEvent("r1", "f1", "n1", "x"), 42), time.Now())
	assert.Equal(t, "", store.getRun("r1").Actor)
}

// TestRecorderFillsActorFromLaterEvent: when the first event seen for a run
// carries no actor (e.g. a node built a fresh message), a later one fills it.
func TestRecorderFillsActorFromLaterEvent(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()))

	first := runningEvent("r1", "f1", "n1", "x")
	first.Input = nil
	rec.persist(first, time.Now())
	assert.Equal(t, "", store.getRun("r1").Actor)

	rec.persist(withActor(successEvent("r1", "f1", "n2", "x"), "editor"), time.Now())
	assert.Equal(t, "editor", store.getRun("r1").Actor)
	assert.Equal(t, 2, store.upserts("r1"), "the run is upserted again only to fill the actor")

	rec.persist(withActor(successEvent("r1", "f1", "n3", "x"), "editor"), time.Now())
	assert.Equal(t, 2, store.upserts("r1"), "no further upserts once the actor is known")
}

// callableTrigger is a minimal node.Emitter trigger for the end-to-end test.
type callableTrigger struct {
	*node.BaseNode
	mu  sync.Mutex
	out chan<- *types.Message
}

func (c *callableTrigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}
func (c *callableTrigger) Ports() (inputs, outputs []types.Port) {
	return nil, []types.Port{{ID: "output", DataType: types.DataTypeObject}}
}
func (c *callableTrigger) Start(_ context.Context, out chan<- *types.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.out = out
	return nil
}
func (c *callableTrigger) Stop() error { return nil }
func (c *callableTrigger) Emit(ctx context.Context, msg *types.Message) error {
	c.mu.Lock()
	out := c.out
	c.mu.Unlock()
	msg.SourcePort = "output"
	select {
	case out <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type sinkNode struct{ *node.BaseNode }

func (sinkNode) Process(context.Context, *types.Message) ([]*types.Message, error) { return nil, nil }
func (sinkNode) Ports() (inputs, outputs []types.Port) {
	return []types.Port{{ID: "input", DataType: types.DataTypeAny}}, nil
}

// TestCalledRunActorReachesHistory: the actor of FlowRuntime.Call ends up on
// the persisted run.
func TestCalledRunActorReachesHistory(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()), WithFlushInterval(5*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	recDone := make(chan struct{})
	go func() { rec.Run(ctx); close(recDone) }()
	defer func() { cancel(); <-recDone }()

	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "tool", Name: "tool", Category: types.NodeCategoryInput},
		func(map[string]interface{}) (node.Node, error) {
			return &callableTrigger{BaseNode: node.NewBaseNode(nil)}, nil
		}))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "sink", Name: "sink", Category: types.NodeCategoryOutput},
		func(map[string]interface{}) (node.Node, error) { return sinkNode{node.NewBaseNode(nil)}, nil }))

	f := flow.NewFlow("called")
	trig := flow.NewNode("tool", types.NodeCategoryInput)
	sink := flow.NewNode("sink", types.NodeCategoryOutput)
	f.AddNode(*trig)
	f.AddNode(*sink)
	f.AddConnection(*flow.NewConnection(trig.ID, "output", sink.ID, "input"))

	rt, err := flow.NewFlowRuntime(f, reg, flow.WithLogger(zerolog.Nop()), flow.WithObserver(rec.Observe))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	defer func() { _ = rt.Stop() }()

	res, err := rt.Call(context.Background(), flow.CallRequest{TriggerNodeID: trig.ID, Actor: "token:bot", Timeout: 3 * time.Second})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusCompletedNoReply, res.Status)

	require.Eventually(t, func() bool {
		run := store.getRun(res.RunID)
		return run.Status == flow.NodePhaseSuccess
	}, 3*time.Second, 5*time.Millisecond)
	run := store.getRun(res.RunID)
	assert.Equal(t, f.ID, run.FlowID)
	assert.Equal(t, "token:bot", run.Actor)
}
