package storage

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tool call records the tool's risk at request time (ADR-0015), so the
// editor shows the risk of what the admin approves, not of whatever the node
// publishes by the time they look.
func TestAgentApprovalRecordsTheToolRisk(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{})
	req := newToolCallApproval(tok.ID)
	req.ToolName = "gpio.set"
	req.ToolRisk = "privileged"
	a, err := s.CreateAgentApproval(req, testNow)
	require.NoError(t, err)
	assert.Equal(t, "privileged", a.ToolRisk)

	got, err := s.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.Equal(t, "privileged", got.ToolRisk)
	list, err := s.ListAgentApprovals(AgentApprovalFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "privileged", list[0].ToolRisk)

	data, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"toolRisk":"privileged"`)

	// Only the known risk levels are stored: an unknown value would mislead
	// the admin.
	for _, risk := range []string{"safe", "external"} {
		ok := newToolCallApproval(tok.ID)
		ok.ToolRisk = risk
		_, err := s.CreateAgentApproval(ok, testNow)
		require.NoError(t, err, risk)
	}
	for _, risk := range []string{"low", "Privileged", "unknown"} {
		bad := newToolCallApproval(tok.ID)
		bad.ToolRisk = risk
		_, err := s.CreateAgentApproval(bad, testNow)
		require.ErrorIs(t, err, ErrInvalidAgentApproval, risk)
	}
}

// A tool call records the tool it asks for (name, fingerprint and risk,
// ADR-0015): the editor shows that tool's risk and approving runs only that
// version, so an approval without them is refused when it is stored.
func TestAgentApprovalNeedsItsTool(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{})
	for name, clear := range map[string]func(*AgentApproval){
		"no name":        func(a *AgentApproval) { a.ToolName = "" },
		"no fingerprint": func(a *AgentApproval) { a.ToolFingerprint = "" },
		"no risk":        func(a *AgentApproval) { a.ToolRisk = "" },
	} {
		a := newToolCallApproval(tok.ID)
		clear(&a)
		_, err := s.CreateAgentApproval(a, testNow)
		require.ErrorIs(t, err, ErrInvalidAgentApproval, name)
	}
	_, err := s.CreateAgentApproval(newToolCallApproval(tok.ID), testNow)
	require.NoError(t, err)
}
