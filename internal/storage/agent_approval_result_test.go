package storage

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An approved tool call keeps the CallResult of its execution, so the agent
// can fetch it later (pw_get_result).
func TestSetAgentApprovalResult(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{})
	a, err := s.CreateAgentApproval(newToolCallApproval(tok.ID), testNow)
	require.NoError(t, err)

	got, err := s.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Result, "no result before it ran")

	result := json.RawMessage(`{"status":"completed","runId":"run-1","result":{"ok":true},"durationMs":12}`)
	require.ErrorIs(t, s.SetAgentApprovalResult(a.ID, result), ErrInvalidAgentApproval, "a pending call never ran")

	_, err = s.DecideAgentApproval(a.ID, true, "admin", testNow.Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, s.SetAgentApprovalResult(a.ID, result))

	got, err = s.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.JSONEq(t, string(result), string(got.Result))
	list, err := s.ListAgentApprovals(AgentApprovalFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.JSONEq(t, string(result), string(list[0].Result))

	data, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"result":{`)

	// Replacing it is allowed (a running result followed by the final one).
	final := json.RawMessage(`{"status":"error","runId":"run-1","durationMs":30}`)
	require.NoError(t, s.SetAgentApprovalResult(a.ID, final))
	got, err = s.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.JSONEq(t, string(final), string(got.Result))
}

func TestSetAgentApprovalResultValidates(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{})
	a, err := s.CreateAgentApproval(newToolCallApproval(tok.ID), testNow)
	require.NoError(t, err)
	_, err = s.DecideAgentApproval(a.ID, true, "admin", testNow.Add(time.Minute))
	require.NoError(t, err)

	require.ErrorIs(t, s.SetAgentApprovalResult("missing", json.RawMessage(`{}`)), ErrAgentApprovalNotFound)
	require.ErrorIs(t, s.SetAgentApprovalResult(a.ID, json.RawMessage(`{`)), ErrInvalidAgentApproval)
	require.ErrorIs(t, s.SetAgentApprovalResult(a.ID, nil), ErrInvalidAgentApproval)
	big := json.RawMessage(`"` + strings.Repeat("x", MaxApprovalResultBytes) + `"`)
	require.ErrorIs(t, s.SetAgentApprovalResult(a.ID, big), ErrInvalidAgentApproval)

	rejected, err := s.CreateAgentApproval(newToolCallApproval(tok.ID), testNow)
	require.NoError(t, err)
	_, err = s.DecideAgentApproval(rejected.ID, false, "admin", testNow.Add(time.Minute))
	require.NoError(t, err)
	require.ErrorIs(t, s.SetAgentApprovalResult(rejected.ID, json.RawMessage(`{}`)), ErrInvalidAgentApproval, "a rejected call never runs")

	draft, err := s.CreateAgentApproval(AgentApproval{Kind: ApprovalKindDraftDeploy, TokenID: tok.ID, Ref: "draft-1"}, testNow)
	require.NoError(t, err)
	_, err = s.DecideAgentApproval(draft.ID, true, "admin", testNow.Add(time.Minute))
	require.NoError(t, err)
	require.ErrorIs(t, s.SetAgentApprovalResult(draft.ID, json.RawMessage(`{}`)), ErrInvalidAgentApproval, "only tool calls have a call result")
}

func TestAgentApprovalResultMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = old.Exec(`
		CREATE TABLE agent_approvals (
			id         TEXT PRIMARY KEY,
			kind       TEXT NOT NULL,
			token_id   TEXT NOT NULL,
			flow_id    TEXT NOT NULL DEFAULT '',
			ref        TEXT NOT NULL,
			args_json  TEXT,
			summary    TEXT NOT NULL DEFAULT '',
			status     TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			decided_at INTEGER,
			decided_by TEXT NOT NULL DEFAULT '',
			expires_at INTEGER NOT NULL
		);
		INSERT INTO agent_approvals (id, kind, token_id, ref, status, created_at, expires_at)
		VALUES ('legacy', 'tool_call', 'tok', 'trigger-1', 'pending', 1, 2);
	`)
	require.NoError(t, err)
	require.NoError(t, old.Close())

	for i := range 2 { // the second open runs the migration again
		store, err := NewSQLiteStore(path)
		require.NoError(t, err, "open %d", i)
		got, err := store.GetAgentApproval("legacy")
		require.NoError(t, err)
		assert.Nil(t, got.Result)
		assert.Empty(t, got.ToolName)
		assert.Empty(t, got.ToolFingerprint)
		require.NoError(t, store.Close())
	}
}

// A tool call records which tool the admin is asked about (its name and
// fingerprint), so an approval never runs a tool that changed since.
func TestAgentApprovalRecordsTheTool(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{})
	req := newToolCallApproval(tok.ID)
	req.ToolName = "gpio.set"
	req.ToolFingerprint = "sha256:" + strings.Repeat("ab", 32)
	req.ToolRisk = ApprovalToolRiskPrivileged
	a, err := s.CreateAgentApproval(req, testNow)
	require.NoError(t, err)
	assert.Equal(t, req.ToolName, a.ToolName)
	assert.Equal(t, req.ToolFingerprint, a.ToolFingerprint)

	got, err := s.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.Equal(t, req.ToolName, got.ToolName)
	assert.Equal(t, req.ToolFingerprint, got.ToolFingerprint)
	list, err := s.ListAgentApprovals(AgentApprovalFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, req.ToolName, list[0].ToolName)
	assert.Equal(t, req.ToolFingerprint, list[0].ToolFingerprint)

	data, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"toolName":"gpio.set"`)
	assert.Contains(t, string(data), `"toolFingerprint":"sha256:`)

	long := newToolCallApproval(tok.ID)
	long.ToolName = strings.Repeat("x", MaxApprovalToolRefLength+1)
	_, err = s.CreateAgentApproval(long, testNow)
	require.ErrorIs(t, err, ErrInvalidAgentApproval)
	long = newToolCallApproval(tok.ID)
	long.ToolFingerprint = strings.Repeat("x", MaxApprovalToolRefLength+1)
	_, err = s.CreateAgentApproval(long, testNow)
	require.ErrorIs(t, err, ErrInvalidAgentApproval)
}
