package flow

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateFailNode builds fine but rejects its configuration in Validate.
type validateFailNode struct{ mockActionNode }

func (n *validateFailNode) Validate() error { return errors.New("url is required") }

// conflictTrigger fails Start because an exclusive resource is taken.
type conflictTrigger struct{ mockTriggerNode }

func (n *conflictTrigger) Start(ctx context.Context, out chan<- *Message) error {
	return fmt.Errorf("%w: webhook path \"/x\" is already registered", ErrResourceConflict)
}

// ADR-0006: a node whose factory or Validate rejects its configuration is an
// invalid flow (HTTP 400), so the error must carry ErrInvalidNodeConfig; a
// trigger that loses an exclusive resource is a conflict (HTTP 409).
func TestDeployErrorsAreClassified(t *testing.T) {
	reg := createTestRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "factory-fail", Name: "Factory fail", Category: NodeCategoryOutput,
		Inputs: []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
	}, func(map[string]any) (node.Node, error) { return nil, errors.New("bad interval") }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "validate-fail", Name: "Validate fail", Category: NodeCategoryOutput,
		Inputs: []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
	}, func(map[string]any) (node.Node, error) { return &validateFailNode{}, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "conflict-trigger", Name: "Conflict", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}},
	}, func(map[string]any) (node.Node, error) { return &conflictTrigger{}, nil }))

	flowWith := func(triggerType, actionType string) *Flow {
		f := NewFlow("f")
		tr := NewNode(triggerType, NodeCategoryInput)
		tr.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
		ac := NewNode(actionType, NodeCategoryOutput)
		ac.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}
		f.AddNode(*tr)
		f.AddNode(*ac)
		f.AddConnection(*NewConnection(tr.ID, "output", ac.ID, "input"))
		return f
	}

	for _, typ := range []string{"factory-fail", "validate-fail"} {
		t.Run(typ, func(t *testing.T) {
			m := NewRuntimeManager(reg, zerolog.Nop())
			err := m.Deploy(context.Background(), flowWith("mock-trigger", typ))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidNodeConfig)
			var nerr *NodeExecutionError
			assert.ErrorAs(t, err, &nerr, "the node that failed is still reported")
		})
	}

	t.Run("resource conflict", func(t *testing.T) {
		m := NewRuntimeManager(reg, zerolog.Nop())
		err := m.Deploy(context.Background(), flowWith("conflict-trigger", "mock-action"))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResourceConflict)
		assert.NotErrorIs(t, err, ErrInvalidNodeConfig)
	})
}
