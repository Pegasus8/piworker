package flow

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lifecycleTrigger records Start/Stop calls and can be told to fail Start.
type lifecycleTrigger struct {
	*node.BaseNode
	mu       sync.Mutex
	startErr error
	running  bool
	started  int
	stopped  int
	out      chan<- *Message
}

func (t *lifecycleTrigger) Process(context.Context, *Message) ([]*Message, error) { return nil, nil }
func (t *lifecycleTrigger) Ports() (inputs, outputs []Port) {
	return nil, []Port{{ID: "output", DataType: DataTypeAny}}
}
func (t *lifecycleTrigger) Start(_ context.Context, out chan<- *Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.startErr != nil {
		return t.startErr
	}
	t.running, t.out = true, out
	t.started++
	return nil
}
func (t *lifecycleTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		t.stopped++
	}
	t.running = false
	return nil
}
func (t *lifecycleTrigger) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}
func (t *lifecycleTrigger) Inject(payload interface{}) error {
	t.mu.Lock()
	out := t.out
	t.mu.Unlock()
	m := NewMessage(payload, DataTypeAny)
	m.SourcePort = "output"
	out <- m
	return nil
}

func lifecycleRegistry(t *testing.T, triggers map[string]*lifecycleTrigger) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	for typ, tr := range triggers {
		tr := tr
		require.NoError(t, reg.Register(node.NodeTypeInfo{Type: typ, Name: typ, Category: NodeCategoryInput},
			func(map[string]interface{}) (node.Node, error) { return tr, nil }))
	}
	return reg
}

// TestTriggerStartFailureFailsStart: a trigger that fails to start (e.g. a
// duplicated webhook path) must fail Start/Deploy, and triggers already started
// must be stopped again.
func TestTriggerStartFailureFailsStart(t *testing.T) {
	good := &lifecycleTrigger{BaseNode: node.NewBaseNode(nil)}
	bad := &lifecycleTrigger{BaseNode: node.NewBaseNode(nil), startErr: errors.New("path already registered")}
	reg := lifecycleRegistry(t, map[string]*lifecycleTrigger{"good": good, "bad": bad})

	f := NewFlow("start-fail")
	// "a" sorts before "b": the good trigger starts first and must be rolled back.
	g := NewNode("good", NodeCategoryInput)
	g.ID = "a-good"
	b := NewNode("bad", NodeCategoryInput)
	b.ID = "b-bad"
	f.AddNode(*g)
	f.AddNode(*b)

	m := NewRuntimeManager(reg, zerolog.Nop())
	err := m.Deploy(context.Background(), f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path already registered")

	_, exists := m.GetRuntime(f.ID)
	assert.False(t, exists, "a flow whose trigger failed must not be registered as deployed")
	assert.Equal(t, 1, good.started)
	assert.Equal(t, 1, good.stopped, "already-started triggers are stopped on failure")
	assert.False(t, good.IsRunning())
}

// TestInjectRightAfterStart: once Start returns, every trigger is running, so an
// immediate manual inject never fails with "trigger is not running".
func TestInjectRightAfterStart(t *testing.T) {
	for i := 0; i < 50; i++ {
		tr := &lifecycleTrigger{BaseNode: node.NewBaseNode(nil)}
		reg := lifecycleRegistry(t, map[string]*lifecycleTrigger{"manual": tr})
		f := NewFlow("inject")
		n := NewNode("manual", NodeCategoryInput)
		f.AddNode(*n)

		rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()))
		require.NoError(t, err)
		require.NoError(t, rt.Start(context.Background()))
		require.NoError(t, rt.InjectTrigger(n.ID, "x"), "iteration %d", i)
		require.NoError(t, rt.Stop())
	}
}
