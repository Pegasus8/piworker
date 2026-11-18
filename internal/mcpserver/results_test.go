package mcpserver

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withEmptySlices turns nil lists into empty ones at any depth, and never
// writes into what it was given: the service may share slices and pointees
// with its own state.
func TestWithEmptySlices(t *testing.T) {
	steps := []flow.TraceStep{{NodeID: "a"}, {NodeID: "b", Outputs: []flow.PortPreview{{Port: "o"}}}}
	in := flow.DryRunResult{Trace: steps}
	out := withEmptySlices(in)

	assert.NotNil(t, out.Issues)
	assert.NotNil(t, out.WouldExecute)
	assert.NotNil(t, out.MissingSecrets)
	require.Len(t, out.Trace, 2)
	assert.NotNil(t, out.Trace[0].Outputs, "nested in a slice")
	assert.Equal(t, []flow.PortPreview{{Port: "o"}}, out.Trace[1].Outputs)

	assert.Nil(t, in.Issues, "the input is not modified")
	assert.Nil(t, steps[0].Outputs, "nor the slices it shares")

	view := service.DraftView{Flow: service.DraftFlowView{Nodes: []service.NodeView{{ID: "n"}}}}
	fixed := withEmptySlices(view)
	assert.NotNil(t, fixed.Flow.Connections, "nested in a struct")
	assert.Nil(t, fixed.Flow.Nodes[0].Config, "maps are left as they are (free-form)")
	assert.Nil(t, view.Flow.Connections)

	p := &service.DraftFlowView{}
	type withPointer struct{ P *service.DraftFlowView }
	got := withEmptySlices(withPointer{P: p})
	assert.NotNil(t, got.P.Nodes, "behind a pointer")
	assert.Nil(t, p.Nodes, "the pointee is copied, not modified")
}
