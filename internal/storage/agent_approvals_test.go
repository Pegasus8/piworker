package storage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newToolCallApproval(tokenID string) AgentApproval {
	return AgentApproval{
		Kind:    ApprovalKindToolCall,
		TokenID: tokenID,
		FlowID:  "flow-1",
		Ref:     "trigger-1",
		Args:    json.RawMessage(`{"pin":17,"state":"on"}`),
		Summary: "Call tool gpio.set",
	}
}

func TestCreateAndGetAgentApproval(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{Name: "desk agent"})

	created, err := s.CreateAgentApproval(newToolCallApproval(tok.ID), testNow)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID, "an id is generated when none is given")
	assert.Equal(t, ApprovalPending, created.Status)
	assert.Equal(t, testNow, created.CreatedAt)
	assert.Equal(t, testNow.Add(ToolCallApprovalLifetime), created.ExpiresAt, "tool calls wait 15 minutes")

	got, err := s.GetAgentApproval(created.ID)
	require.NoError(t, err)
	assert.Equal(t, ApprovalKindToolCall, got.Kind)
	assert.Equal(t, tok.ID, got.TokenID)
	assert.Equal(t, "desk agent", got.TokenName)
	assert.Equal(t, "flow-1", got.FlowID)
	assert.Equal(t, "trigger-1", got.Ref)
	assert.JSONEq(t, `{"pin":17,"state":"on"}`, string(got.Args))
	assert.Equal(t, "Call tool gpio.set", got.Summary)
	assert.Equal(t, ApprovalPending, got.Status)
	assert.Nil(t, got.DecidedAt)
	assert.Empty(t, got.DecidedBy)
	assert.Equal(t, created.ExpiresAt, got.ExpiresAt)

	_, err = s.GetAgentApproval("missing")
	require.ErrorIs(t, err, ErrAgentApprovalNotFound)
}

func TestCreateAgentApprovalKeepsCallerIDAndDefaults(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	a := AgentApproval{ID: "run-123", Kind: ApprovalKindDraftDeploy, TokenID: "tok", Ref: "draft-9", Summary: "Deploy draft"}
	created, err := s.CreateAgentApproval(a, testNow)
	require.NoError(t, err)
	assert.Equal(t, "run-123", created.ID, "the run id doubles as the approval id")
	assert.Equal(t, testNow.Add(DraftDeployApprovalLifetime), created.ExpiresAt)
	assert.Nil(t, created.Args)
	got, err := s.GetAgentApproval("run-123")
	require.NoError(t, err)
	assert.Nil(t, got.Args)
	assert.Empty(t, got.TokenName, "unknown token ids still list")

	_, err = s.CreateAgentApproval(a, testNow)
	require.ErrorIs(t, err, ErrAgentApprovalExists)

	explicit := newToolCallApproval("tok")
	explicit.ExpiresAt = testNow.Add(time.Minute)
	explicit.Status = ApprovalApproved // ignored: new approvals are always pending
	created, err = s.CreateAgentApproval(explicit, testNow)
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(time.Minute), created.ExpiresAt)
	assert.Equal(t, ApprovalPending, created.Status)
}

func TestCreateAgentApprovalValidates(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	mut := map[string]func(*AgentApproval){
		"unknown kind": func(a *AgentApproval) { a.Kind = "shell" },
		"no token":     func(a *AgentApproval) { a.TokenID = "" },
		"no ref":       func(a *AgentApproval) { a.Ref = "" },
		"invalid args": func(a *AgentApproval) { a.Args = json.RawMessage(`{"a":`) },
		"oversized args": func(a *AgentApproval) {
			a.Args = json.RawMessage(`"` + strings.Repeat("x", MaxApprovalArgsBytes) + `"`)
		},
		"long summary": func(a *AgentApproval) { a.Summary = strings.Repeat("s", MaxApprovalSummaryLength+1) },
		"past expiry":  func(a *AgentApproval) { a.ExpiresAt = testNow },
	}
	for name, m := range mut {
		t.Run(name, func(t *testing.T) {
			a := newToolCallApproval("tok")
			m(&a)
			_, err := s.CreateAgentApproval(a, testNow)
			require.ErrorIs(t, err, ErrInvalidAgentApproval)
		})
	}
}

func TestListAgentApprovals(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	first, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow)
	require.NoError(t, err)
	second, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow.Add(time.Second))
	require.NoError(t, err)
	third, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow.Add(2*time.Second))
	require.NoError(t, err)
	_, err = s.DecideAgentApproval(second.ID, false, "admin", testNow.Add(3*time.Second))
	require.NoError(t, err)

	all, err := s.ListAgentApprovals(AgentApprovalFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []string{third.ID, second.ID, first.ID}, []string{all[0].ID, all[1].ID, all[2].ID}, "newest first")

	pending, err := s.ListAgentApprovals(AgentApprovalFilter{Status: ApprovalPending})
	require.NoError(t, err)
	require.Len(t, pending, 2)
	assert.Equal(t, third.ID, pending[0].ID)

	limited, err := s.ListAgentApprovals(AgentApprovalFilter{Limit: 1})
	require.NoError(t, err)
	require.Len(t, limited, 1)

	_, err = s.ListAgentApprovals(AgentApprovalFilter{Status: "bogus"})
	require.ErrorIs(t, err, ErrInvalidAgentApproval)

	empty := createTestStore(t)
	defer empty.Close()
	none, err := empty.ListAgentApprovals(AgentApprovalFilter{})
	require.NoError(t, err)
	assert.NotNil(t, none, "an empty list serializes as []")
}

func TestDecideAgentApproval(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	a, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow)
	require.NoError(t, err)

	decided, err := s.DecideAgentApproval(a.ID, true, "admin", testNow.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, ApprovalApproved, decided.Status)
	require.NotNil(t, decided.DecidedAt)
	assert.Equal(t, testNow.Add(time.Minute), *decided.DecidedAt)
	assert.Equal(t, "admin", decided.DecidedBy)

	_, err = s.DecideAgentApproval(a.ID, false, "admin", testNow.Add(2*time.Minute))
	require.ErrorIs(t, err, ErrAgentApprovalDecided, "a decision is final")
	got, err := s.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.Equal(t, ApprovalApproved, got.Status)

	b, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow)
	require.NoError(t, err)
	rejected, err := s.DecideAgentApproval(b.ID, false, "admin", testNow.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, ApprovalRejected, rejected.Status)

	_, err = s.DecideAgentApproval("missing", true, "admin", testNow)
	require.ErrorIs(t, err, ErrAgentApprovalNotFound)
}

func TestDecideExpiredAgentApproval(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	a, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow)
	require.NoError(t, err)
	_, err = s.DecideAgentApproval(a.ID, true, "admin", a.ExpiresAt)
	require.ErrorIs(t, err, ErrAgentApprovalExpired, "approving at the expiry instant is too late")
	got, err := s.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.Equal(t, ApprovalExpired, got.Status)
}

func TestExpireAgentApprovals(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	stale, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow)
	require.NoError(t, err)
	fresh, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow.Add(10*time.Minute))
	require.NoError(t, err)
	decided, err := s.CreateAgentApproval(newToolCallApproval("tok"), testNow)
	require.NoError(t, err)
	_, err = s.DecideAgentApproval(decided.ID, true, "admin", testNow.Add(time.Minute))
	require.NoError(t, err)

	at := testNow.Add(ToolCallApprovalLifetime)
	assert.Equal(t, ApprovalExpired, stale.EffectiveStatus(at))
	assert.Equal(t, ApprovalPending, fresh.EffectiveStatus(at))
	n, err := s.CountPendingAgentApprovals(at)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the badge never counts expired approvals")

	expired, err := s.ExpireAgentApprovals(at)
	require.NoError(t, err)
	assert.Equal(t, 1, expired)
	got, err := s.GetAgentApproval(stale.ID)
	require.NoError(t, err)
	assert.Equal(t, ApprovalExpired, got.Status)
	got, err = s.GetAgentApproval(fresh.ID)
	require.NoError(t, err)
	assert.Equal(t, ApprovalPending, got.Status)
	got, err = s.GetAgentApproval(decided.ID)
	require.NoError(t, err)
	assert.Equal(t, ApprovalApproved, got.Status)

	// Decided approvals are pruned once past the retention window.
	_, err = s.ExpireAgentApprovals(testNow.Add(AgentApprovalRetention + time.Hour))
	require.NoError(t, err)
	_, err = s.GetAgentApproval(decided.ID)
	require.ErrorIs(t, err, ErrAgentApprovalNotFound)
}
