package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The SQLite store is what authoring and agent deploy run on.
var _ AuthoringStore = (*storage.SQLiteStore)(nil)

// The low agent_deploy level refuses the contract's eight alert kinds,
// credentialed_request_changed (H1) and global_variable_write; risk_raised
// is bounded by the level's risk cap instead.
func TestLowDeployRefusedAlerts(t *testing.T) {
	assert.Equal(t, []string{
		flow.AlertPrivilegedNodeAdded, flow.AlertDestinationChanged, flow.AlertDynamicDestination,
		flow.AlertNewHost, flow.AlertSecretAdded, flow.AlertSecretMoved, flow.AlertRiskSwitchOn,
		flow.AlertSensitiveLiteral, flow.AlertCredentialedRequestChanged, flow.AlertGlobalVariableWrite,
	}, LowDeployRefusedAlerts())
	assert.NotContains(t, LowDeployRefusedAlerts(), flow.AlertRiskRaised)
	kinds := LowDeployRefusedAlerts()
	kinds[0] = "changed"
	assert.Equal(t, flow.AlertPrivilegedNodeAdded, LowDeployRefusedAlerts()[0], "a copy")
}

func TestNewAuthoringDefaults(t *testing.T) {
	a := NewAuthoring(AuthoringOptions{})
	assert.Same(t, node.DefaultRegistry, a.registry)
	assert.NotNil(t, a.secrets)
	assert.NotNil(t, a.now)
}

// The JSON of the REST and MCP shapes is camelCase, as documented in
// docs/design/mcp.md §10.
func TestAuthoringJSONShapes(t *testing.T) {
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	keys := func(v any) []string {
		t.Helper()
		data, err := json.Marshal(v)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))
		out := []string{}
		for k := range m {
			out = append(out, k)
		}
		return out
	}
	assert.ElementsMatch(t, []string{"id", "flowId", "flowName", "isNew", "baseVersion", "currentVersion", "stale",
		"status", "risk", "alertCount", "issueCounts", "note", "tokenId", "tokenName", "createdAt", "decidedAt"},
		keys(DraftSummary{CreatedAt: at}))
	assert.ElementsMatch(t, []string{"id", "flowId", "flowName", "isNew", "baseVersion", "currentVersion", "stale",
		"status", "risk", "alertCount", "issueCounts", "note", "tokenId", "tokenName", "createdAt", "decidedAt",
		"issues", "running", "diff", "lastApprovedVersion", "diffApproved", "flow"},
		keys(DraftDetail{DraftSummary: DraftSummary{CreatedAt: at, Note: "short"}, Note: "the whole note"}))
	assert.ElementsMatch(t, []string{"drafts", "nextBefore"}, keys(DraftList{}))
	assert.ElementsMatch(t, []string{"flowId", "version", "deployed"}, keys(ApplyResult{}))
	assert.ElementsMatch(t, []string{"flowId", "version", "deployed", "deployError"},
		keys(ApplyResult{DeployError: &DeployError{Status: 409, Message: "busy"}}))
	assert.ElementsMatch(t, []string{"current", "lastAdminApproved", "versions"}, keys(VersionList{}))
	assert.ElementsMatch(t, []string{"version", "actorKind", "adminApproved", "createdAt", "deployed"}, keys(VersionEntry{}))
	assert.ElementsMatch(t, []string{"draftId", "status", "risk", "issues", "alerts", "flowId", "baseVersion"}, keys(DraftResult{}))
	assert.ElementsMatch(t, []string{"valid", "issues", "risk"}, keys(ValidateResult{}))
	assert.ElementsMatch(t, []string{"flowId", "version", "running", "risk"}, keys(DeployResult{}))

	// A flow with its marks keeps the flow's own fields.
	f := flow.NewFlow("marked")
	f.Version = 3
	marked := keys(FlowWithMarks{Flow: f, FlowMarks: FlowMarks{Deployment: &DeploymentView{Version: 2, ActorKind: "agent", TokenName: "claude", DeployedAt: at}}})
	assert.Subset(t, marked, []string{"id", "name", "version", "nodes", "connections", "state", "deployment", "agentChanged"})
	data, err := json.Marshal(FlowMarks{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"deployment":null,"agentChanged":false}`, string(data))
	assert.ElementsMatch(t, []string{"version", "actorKind", "tokenName", "deployedAt"},
		keys(DeploymentView{Version: 2, ActorKind: "agent", TokenName: "claude", DeployedAt: at}))

	// The detail's own note wins over the summary's preview.
	data, err = json.Marshal(DraftDetail{DraftSummary: DraftSummary{Note: "short"}, Note: "the whole note"})
	require.NoError(t, err)
	var detail struct {
		Note string `json:"note"`
	}
	require.NoError(t, json.Unmarshal(data, &detail))
	assert.Equal(t, "the whole note", detail.Note)
}
