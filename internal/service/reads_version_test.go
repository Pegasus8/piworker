package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The read views carry the flow's version: an agent's draft of a stored
// flow names the version it is based on (pw_save_draft baseVersion), so it
// must be able to read it rather than probe for it.
func TestFlowViewsCarryTheVersion(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("versioned"))
	f.Name = "versioned again"
	require.NoError(t, e.store.UpdateFlow(f)) // an editor save: version 2

	agent := readsAgent(storage.ScopeRead)
	view, err := e.reads.GetFlow(context.Background(), agent, f.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, view.Version)

	list, err := e.reads.ListFlows(context.Background(), agent)
	require.NoError(t, err)
	require.Len(t, list.Flows, 1)
	assert.Equal(t, 2, list.Flows[0].Version)

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, view)), &raw))
	assert.Equal(t, float64(2), raw["version"])
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, list.Flows[0])), &raw))
	assert.Equal(t, float64(2), raw["version"])
}
