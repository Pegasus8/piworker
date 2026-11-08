package storage

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
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

	// Only the known risk levels (or none, for other kinds) are stored: an
	// unknown value would mislead the admin.
	for _, risk := range []string{"safe", "external", ""} {
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

// A tool call that names its tool is shown in the editor as a request made
// with the tool's risk recorded (ADR-0015), so it must record one: an empty
// risk would be taken for an approval stored before risks were recorded.
func TestAgentApprovalNamingItsToolNeedsItsRisk(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{})
	named := newToolCallApproval(tok.ID)
	named.ToolName = "gpio.set"
	named.ToolFingerprint = "sha256:ab"
	_, err := s.CreateAgentApproval(named, testNow)
	require.ErrorIs(t, err, ErrInvalidAgentApproval)
	onlyFingerprint := newToolCallApproval(tok.ID)
	onlyFingerprint.ToolFingerprint = "sha256:ab"
	_, err = s.CreateAgentApproval(onlyFingerprint, testNow)
	require.ErrorIs(t, err, ErrInvalidAgentApproval)

	named.ToolRisk = ApprovalToolRiskSafe
	_, err = s.CreateAgentApproval(named, testNow)
	require.NoError(t, err)
	// Without the tool's identity it is an approval of the old shape, which
	// approving never runs.
	_, err = s.CreateAgentApproval(newToolCallApproval(tok.ID), testNow)
	require.NoError(t, err)
}

// Databases created before tool_risk existed gain the column; their
// approvals report no recorded risk.
func TestAgentApprovalToolRiskMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wave2.db")
	old, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = old.Exec(`
		CREATE TABLE agent_approvals (
			id         TEXT PRIMARY KEY,
			kind       TEXT NOT NULL,
			token_id   TEXT NOT NULL,
			flow_id    TEXT NOT NULL DEFAULT '',
			ref        TEXT NOT NULL,
			tool_name  TEXT NOT NULL DEFAULT '',
			tool_fingerprint TEXT NOT NULL DEFAULT '',
			args_json  TEXT,
			summary    TEXT NOT NULL DEFAULT '',
			status     TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			decided_at INTEGER,
			decided_by TEXT NOT NULL DEFAULT '',
			expires_at INTEGER NOT NULL,
			result_json TEXT
		);
		INSERT INTO agent_approvals (id, kind, token_id, ref, tool_name, tool_fingerprint, status, created_at, expires_at)
		VALUES ('wave2', 'tool_call', 'tok', 'trigger-1', 'garage_open', 'sha256:ab', 'pending', 1, 2);
	`)
	require.NoError(t, err)
	require.NoError(t, old.Close())

	for i := range 2 { // the second open runs the migration again
		store, err := NewSQLiteStore(path)
		require.NoError(t, err, "open %d", i)
		got, err := store.GetAgentApproval("wave2")
		require.NoError(t, err)
		assert.Equal(t, "garage_open", got.ToolName)
		assert.Empty(t, got.ToolRisk)
		data, err := json.Marshal(got)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "toolRisk", "old approvals carry no recorded risk")
		require.NoError(t, store.Close())
	}
}
