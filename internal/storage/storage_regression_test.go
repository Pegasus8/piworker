package storage

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchFlowsEscapesLikeWildcards verifies that LIKE metacharacters in the
// search term are treated literally, not as wildcards.
func TestSearchFlowsEscapesLikeWildcards(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	require.NoError(t, err)
	defer store.Close()

	for _, name := range []string{"a_b", "axb"} {
		require.NoError(t, store.CreateFlow(flow.NewFlow(name)))
	}

	res, err := store.SearchFlows("a_b")
	require.NoError(t, err)
	require.Len(t, res, 1, "underscore must match literally, not as a wildcard")
	assert.Equal(t, "a_b", res[0].Name)
}

// TestUpdateFlowStatePreservesData verifies that updating only the state does not
// clobber the rest of the flow (the lost-update / blob-rewrite bug).
func TestUpdateFlowStatePreservesData(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	require.NoError(t, err)
	defer store.Close()

	f := flow.NewFlow("with-nodes")
	f.AddNode(*flow.NewNode("trigger-interval", flow.NodeCategoryInput))
	require.NoError(t, store.CreateFlow(f))

	require.NoError(t, store.UpdateFlowState(f.ID, flow.FlowStateRunning))

	got, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, got.State, "state must be updated inside the JSON blob too")
	require.Len(t, got.Nodes, 1, "UpdateFlowState must not clobber the flow's nodes")
}
