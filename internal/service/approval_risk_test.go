package service

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A confirm:admin call records the tool's risk at request time (ADR-0015
// action item 3), so the editor shows the risk of the tool the admin is
// asked about, even after a redeploy changed it.
func TestApprovalRecordsTheToolRisk(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	require.NotEmpty(t, info.Risk)
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	res, err := e.call(agent, info, map[string]any{"door": "left"})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusApprovalRequired, res.Status)

	a, err := e.store.GetAgentApproval(res.RunID)
	require.NoError(t, err)
	assert.Equal(t, string(info.Risk), a.ToolRisk)
}
