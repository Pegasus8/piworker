package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor shows the tool's name and risk as recorded with the request
// (ADR-0015), so the approvals API returns them with each approval.
func TestApprovalsReportTheRecordedTool(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	tok, _, err := env.store.CreateAPIToken(storage.NewAPIToken{Name: "agent", Scopes: []string{storage.ScopeCall}}, time.Now())
	require.NoError(t, err)
	_, err = env.store.CreateAgentApproval(storage.AgentApproval{
		Kind: storage.ApprovalKindToolCall, TokenID: tok.ID, FlowID: "flow", Ref: "trigger",
		ToolName: "valves.set", ToolFingerprint: "sha256:ab", ToolRisk: storage.ApprovalToolRiskPrivileged,
		Args: json.RawMessage(`{"valve":"open"}`),
	}, time.Now())
	require.NoError(t, err)

	w := env.do("GET", "/api/agent/approvals", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var raw struct {
		Data struct {
			Approvals []map[string]any `json:"approvals"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	require.Len(t, raw.Data.Approvals, 1)
	got := raw.Data.Approvals[0]
	assert.Equal(t, "valves.set", got["toolName"])
	assert.Equal(t, "privileged", got["toolRisk"])
}
