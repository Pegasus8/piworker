package flow

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func passPorts(n *Node) *Node {
	n.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
	n.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	return n
}

// structuralFlow returns trigger -> a -> b with valid ports.
func structuralFlow() (*Flow, *Node, *Node, *Node) {
	f := NewFlow("structural")
	trig := NewNode("trigger-interval", NodeCategoryInput)
	trig.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	a := passPorts(NewNode("process-x", NodeCategoryProcessing))
	b := passPorts(NewNode("process-x", NodeCategoryProcessing))
	f.AddNode(*trig)
	f.AddNode(*a)
	f.AddNode(*b)
	f.AddConnection(*NewConnection(trig.ID, "output", a.ID, "input"))
	f.AddConnection(*NewConnection(a.ID, "output", b.ID, "input"))
	return f, trig, a, b
}

func TestFlowValidateStructure(t *testing.T) {
	t.Run("baseline is valid", func(t *testing.T) {
		f, _, _, _ := structuralFlow()
		assert.NoError(t, f.Validate(modelTestRegistry()))
	})

	t.Run("duplicate node IDs", func(t *testing.T) {
		f, _, a, _ := structuralFlow()
		dup := *passPorts(NewNode("process-x", NodeCategoryProcessing))
		dup.ID = a.ID
		f.AddNode(dup)
		err := f.Validate(modelTestRegistry())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate node ID")
	})

	t.Run("empty node ID", func(t *testing.T) {
		f, _, _, _ := structuralFlow()
		n := *passPorts(NewNode("process-x", NodeCategoryProcessing))
		n.ID = ""
		f.AddNode(n)
		err := f.Validate(modelTestRegistry())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty ID")
	})

	t.Run("duplicate connection IDs", func(t *testing.T) {
		f, trig, _, b := structuralFlow()
		c := *NewConnection(trig.ID, "output", b.ID, "input")
		c.ID = f.Connections[0].ID
		f.AddConnection(c)
		err := f.Validate(modelTestRegistry())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate connection ID")
	})

	t.Run("self loop", func(t *testing.T) {
		f, _, a, _ := structuralFlow()
		f.AddConnection(*NewConnection(a.ID, "output", a.ID, "input"))
		err := f.Validate(modelTestRegistry())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "itself")
	})

	t.Run("cycle", func(t *testing.T) {
		f, _, a, b := structuralFlow()
		f.AddConnection(*NewConnection(b.ID, "output", a.ID, "input"))
		err := f.Validate(modelTestRegistry())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cycle")
	})

	t.Run("diamond is not a cycle", func(t *testing.T) {
		f, trig, _, b := structuralFlow()
		f.AddConnection(*NewConnection(trig.ID, "output", b.ID, "input"))
		assert.NoError(t, f.Validate(modelTestRegistry()))
	})
}

// TestFlowValidateIgnoresClientCategory checks trigger-ness comes
// from the registry, not from the client-supplied Category field.
func TestFlowValidateIgnoresClientCategory(t *testing.T) {
	reg := createTestRegistry()

	t.Run("action mislabelled as input is not a trigger", func(t *testing.T) {
		f := NewFlow("liar")
		n := NewNode("mock-action", NodeCategoryInput)
		f.AddNode(*n)
		assert.ErrorIs(t, f.Validate(reg), ErrNoTriggerNodes)
	})

	t.Run("trigger mislabelled as processing is a trigger", func(t *testing.T) {
		f := NewFlow("modest")
		n := NewNode("mock-trigger", NodeCategoryProcessing)
		n.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
		f.AddNode(*n)
		assert.NoError(t, f.Validate(reg))
	})
}

// TestDeployDeterminesTriggersFromInstances checks Deploy uses the instantiated
// node (implements node.TriggerNode), not the Category field.
func TestDeployDeterminesTriggersFromInstances(t *testing.T) {
	reg := createTestRegistry()
	m := NewRuntimeManager(reg, zerolog.Nop())
	defer m.StopAll()

	liar := NewFlow("liar")
	liar.AddNode(*NewNode("mock-action", NodeCategoryInput))
	assert.ErrorIs(t, m.Deploy(context.Background(), liar), ErrNoTriggerNodes)

	modest := NewFlow("modest")
	modest.AddNode(*NewNode("mock-trigger", NodeCategoryProcessing))
	assert.NoError(t, m.Deploy(context.Background(), modest))
}

// modelTestRegistry registers the node types the model tests use, so that
// trigger-ness is resolved from the registry as in production.
func modelTestRegistry() *node.Registry {
	reg := createTestRegistry()
	for typ, cat := range map[string]NodeCategory{
		"trigger-interval": NodeCategoryInput,
		"action-log":       NodeCategoryOutput,
		"process-x":        NodeCategoryProcessing,
	} {
		_ = reg.Register(node.NodeTypeInfo{Type: typ, Name: typ, Category: cat}, newMockActionNode)
	}
	return reg
}
