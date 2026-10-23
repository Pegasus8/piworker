package storage

import (
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFlowVariablesIsolatedPersistentAndDeletedWithFlow(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	a, b := flow.NewFlow("a"), flow.NewFlow("b")
	require.NoError(t, s.CreateFlow(a))
	require.NoError(t, s.CreateFlow(b))
	pa, pb := s.FlowVariables(a.ID), s.FlowVariables(b.ID)
	require.NoError(t, pa.SaveVar("count", float64(4)))
	require.NoError(t, pb.SaveVar("count", false))
	require.NoError(t, s.SaveVar("count", "global"))
	values, err := s.FlowVariables(a.ID).LoadVars()
	require.NoError(t, err)
	require.Equal(t, float64(4), values["count"])
	values, err = pb.LoadVars()
	require.NoError(t, err)
	require.Equal(t, false, values["count"])
	require.NoError(t, s.DeleteFlow(a.ID))
	values, err = pa.LoadVars()
	require.NoError(t, err)
	require.Empty(t, values)
	require.Error(t, pa.SaveVar("count", 1))
	values, err = s.LoadVars()
	require.NoError(t, err)
	require.Equal(t, "global", values["count"])
}
