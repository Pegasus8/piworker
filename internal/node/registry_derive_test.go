package node

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubNode is what the overriding factories of the Derive tests build.
type stubNode struct {
	mockNode
	of string
}

func (n *stubNode) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}

func deriveTestRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, info := range []NodeTypeInfo{
		{Type: "safe-node", Name: "Safe", Category: types.NodeCategoryProcessing, Risk: RiskSafe},
		{Type: "send-node", Name: "Send", Category: types.NodeCategoryOutput, Risk: RiskExternal},
	} {
		require.NoError(t, r.Register(info, mockNodeFactory))
	}
	return r
}

// TestDeriveOverridesFactoriesOnly: the derived registry has the same types
// (same infos); override picks each type's factory, nil keeping the original.
func TestDeriveOverridesFactoriesOnly(t *testing.T) {
	orig := deriveTestRegistry(t)
	var seen []string
	derived := orig.Derive(func(info NodeTypeInfo, factory NodeFactory) NodeFactory {
		seen = append(seen, info.Type)
		require.NotNil(t, factory)
		if info.Risk == RiskSafe {
			return nil // keep the original
		}
		return func(cfg map[string]interface{}) (Node, error) {
			return &stubNode{mockNode: mockNode{BaseNode: NewBaseNode(cfg)}, of: info.Type}, nil
		}
	})

	assert.ElementsMatch(t, []string{"safe-node", "send-node"}, seen, "override is asked once per type")
	assert.Equal(t, orig.Count(), derived.Count())
	for _, typ := range []string{"safe-node", "send-node"} {
		a, _ := orig.Get(typ)
		b, ok := derived.Get(typ)
		require.True(t, ok)
		assert.Equal(t, a, b, "infos are the same")
	}

	n, err := derived.Create("safe-node", nil)
	require.NoError(t, err)
	assert.IsType(t, &mockNode{}, n, "nil keeps the original factory")
	n, err = derived.Create("send-node", nil)
	require.NoError(t, err)
	require.IsType(t, &stubNode{}, n)
	assert.Equal(t, "send-node", n.(*stubNode).of)
}

// TestDeriveLeavesOriginalUnchanged: deriving (and changing the derived
// registry afterwards) never touches the original.
func TestDeriveLeavesOriginalUnchanged(t *testing.T) {
	orig := deriveTestRegistry(t)
	derived := orig.Derive(func(NodeTypeInfo, NodeFactory) NodeFactory {
		return func(cfg map[string]interface{}) (Node, error) { return &stubNode{}, nil }
	})

	for _, typ := range []string{"safe-node", "send-node"} {
		n, err := orig.Create(typ, nil)
		require.NoError(t, err)
		assert.IsType(t, &mockNode{}, n, "the original still builds real nodes")
	}

	require.NoError(t, derived.Register(NodeTypeInfo{Type: "extra", Risk: RiskSafe}, mockNodeFactory))
	derived.Unregister("safe-node")
	assert.False(t, orig.Has("extra"))
	assert.True(t, orig.Has("safe-node"))
	assert.Equal(t, 2, orig.Count())
}

// TestDeriveNilOverrideCopies: a nil override copies the registry.
func TestDeriveNilOverrideCopies(t *testing.T) {
	orig := deriveTestRegistry(t)
	derived := orig.Derive(nil)
	assert.Equal(t, orig.Count(), derived.Count())
	n, err := derived.Create("send-node", nil)
	require.NoError(t, err)
	assert.IsType(t, &mockNode{}, n)
}

// TestDeriveDoesNotProbeAgain: Derive keeps the infos as registered and never
// calls a factory itself (a probe of an overriding factory could have side
// effects, and the schema was already derived when the type was registered).
func TestDeriveDoesNotProbeAgain(t *testing.T) {
	orig := deriveTestRegistry(t)
	calls := 0
	orig.Derive(func(NodeTypeInfo, NodeFactory) NodeFactory {
		return func(map[string]interface{}) (Node, error) { calls++; return &stubNode{}, nil }
	})
	assert.Zero(t, calls)
}
