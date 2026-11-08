package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingExecutor struct {
	mu    sync.Mutex
	calls []storage.AgentApproval
	err   error
}

func (e *recordingExecutor) ExecuteApproval(_ context.Context, a storage.AgentApproval) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, a)
	return e.err
}

type settableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *settableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *settableClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

var approvalsT0 = time.Now().Truncate(time.Second)

// addApproval queues a tool call approval from a new, active token.
func addApproval(t *testing.T, s *storage.SQLiteStore, at time.Time, expires time.Duration) storage.AgentApproval {
	t.Helper()
	tok, _, err := s.CreateAPIToken(storage.NewAPIToken{Name: "agent", Scopes: []string{storage.ScopeCall}}, at)
	require.NoError(t, err)
	return addApprovalFor(t, s, tok.ID, at, expires)
}

func addApprovalFor(t *testing.T, s *storage.SQLiteStore, tokenID string, at time.Time, expires time.Duration) storage.AgentApproval {
	t.Helper()
	a, err := s.CreateAgentApproval(storage.AgentApproval{
		Kind: storage.ApprovalKindToolCall, TokenID: tokenID, FlowID: "flow", Ref: "trigger",
		Args: json.RawMessage(`{"valve":"open"}`), Summary: "Call valves.set", ExpiresAt: at.Add(expires),
	}, at)
	require.NoError(t, err)
	return *a
}

type approvalItem struct {
	storage.AgentApproval
	TokenStatus string `json:"tokenStatus"`
}

type approvalList struct {
	Approvals []approvalItem `json:"approvals"`
	Pending   int            `json:"pending"`
}

type approvalDecision struct {
	Approval       approvalItem `json:"approval"`
	Executed       bool         `json:"executed"`
	ExecutionError string       `json:"executionError"`
}

func TestListApprovals(t *testing.T) {
	clock := &settableClock{now: approvalsT0}
	env := newAgentTestEnv(t, AgentHandlerOptions{Now: clock.Now})
	short := addApproval(t, env.store, approvalsT0, time.Minute)
	open := addApproval(t, env.store, approvalsT0.Add(time.Second), 15*time.Minute)
	closed := addApproval(t, env.store, approvalsT0.Add(2*time.Second), 15*time.Minute)
	_, err := env.store.DecideAgentApproval(closed.ID, false, "admin", approvalsT0.Add(3*time.Second))
	require.NoError(t, err)
	clock.Set(approvalsT0.Add(2 * time.Minute))

	w := env.do("GET", "/api/agent/approvals", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	all := data[approvalList](t, w)
	require.Len(t, all.Approvals, 3)
	assert.Equal(t, 1, all.Pending, "the badge counts only approvals still waiting")
	status := map[string]string{}
	for _, a := range all.Approvals {
		status[a.ID] = a.Status
	}
	assert.Equal(t, map[string]string{short.ID: "expired", open.ID: "pending", closed.ID: "rejected"}, status)
	assert.JSONEq(t, `{"valve":"open"}`, string(all.Approvals[0].Args))

	w = env.do("GET", "/api/agent/approvals?status=pending", nil)
	require.Equal(t, http.StatusOK, w.Code)
	pending := data[approvalList](t, w)
	require.Len(t, pending.Approvals, 1)
	assert.Equal(t, open.ID, pending.Approvals[0].ID)

	w = env.do("GET", "/api/agent/approvals?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, data[approvalList](t, w).Approvals, 1)

	for _, q := range []string{"?status=bogus", "?limit=abc", "?limit=-1"} {
		require.Equal(t, http.StatusBadRequest, env.do("GET", "/api/agent/approvals"+q, nil).Code, q)
	}
}

func TestApproveRecordsDecisionThenExecutes(t *testing.T) {
	clock := &settableClock{now: approvalsT0}
	exec := &recordingExecutor{}
	env := newAgentTestEnv(t, AgentHandlerOptions{Now: clock.Now, Executor: exec})
	a := addApproval(t, env.store, approvalsT0, 15*time.Minute)
	clock.Set(approvalsT0.Add(time.Minute))

	w := env.do("POST", "/api/agent/approvals/"+a.ID+"/approve", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	d := data[approvalDecision](t, w)
	assert.True(t, d.Executed)
	assert.Empty(t, d.ExecutionError)
	assert.Equal(t, storage.ApprovalApproved, d.Approval.Status)
	assert.Equal(t, "admin", d.Approval.DecidedBy)

	require.Len(t, exec.calls, 1)
	assert.Equal(t, a.ID, exec.calls[0].ID)
	assert.Equal(t, storage.ApprovalApproved, exec.calls[0].Status, "the executor only ever sees recorded approvals")

	w = env.do("POST", "/api/agent/approvals/"+a.ID+"/approve", nil)
	require.Equal(t, http.StatusConflict, w.Code, "an approval runs at most once")
	require.Len(t, exec.calls, 1)
}

func TestApproveWithoutExecutorReportsUnavailable(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	a := addApproval(t, env.store, time.Now(), 15*time.Minute)
	w := env.do("POST", "/api/agent/approvals/"+a.ID+"/approve", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	d := data[approvalDecision](t, w)
	assert.False(t, d.Executed)
	assert.Equal(t, "not available", d.ExecutionError)
	stored, err := env.store.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalApproved, stored.Status, "the decision is recorded even when nothing can run it yet")
}

func TestExecutorErrorIsReported(t *testing.T) {
	exec := &recordingExecutor{err: errors.New("flow stopped")}
	env := newAgentTestEnv(t, AgentHandlerOptions{Executor: exec})
	a := addApproval(t, env.store, time.Now(), 15*time.Minute)
	w := env.do("POST", "/api/agent/approvals/"+a.ID+"/approve", nil)
	require.Equal(t, http.StatusOK, w.Code)
	d := data[approvalDecision](t, w)
	assert.False(t, d.Executed)
	assert.Equal(t, "flow stopped", d.ExecutionError)
}

func TestRejectDoesNotExecute(t *testing.T) {
	exec := &recordingExecutor{}
	env := newAgentTestEnv(t, AgentHandlerOptions{Executor: exec})
	a := addApproval(t, env.store, time.Now(), 15*time.Minute)
	w := env.do("POST", "/api/agent/approvals/"+a.ID+"/reject", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	d := data[approvalDecision](t, w)
	assert.Equal(t, storage.ApprovalRejected, d.Approval.Status)
	assert.False(t, d.Executed)
	assert.Empty(t, exec.calls)
	require.Equal(t, http.StatusConflict, env.do("POST", "/api/agent/approvals/"+a.ID+"/approve", nil).Code)
}

func TestDecisionErrors(t *testing.T) {
	clock := &settableClock{now: approvalsT0}
	exec := &recordingExecutor{}
	env := newAgentTestEnv(t, AgentHandlerOptions{Now: clock.Now, Executor: exec})
	require.Equal(t, http.StatusNotFound, env.do("POST", "/api/agent/approvals/unknown/approve", nil).Code)

	a := addApproval(t, env.store, approvalsT0, time.Minute)
	clock.Set(approvalsT0.Add(time.Minute))
	w := env.do("POST", "/api/agent/approvals/"+a.ID+"/approve", nil)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, errorMessage(t, w), "expired")
	assert.Empty(t, exec.calls)
	stored, err := env.store.GetAgentApproval(a.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalExpired, stored.Status)
}

func TestRevokedTokenApprovalCannotBeApproved(t *testing.T) {
	exec := &recordingExecutor{}
	env := newAgentTestEnv(t, AgentHandlerOptions{Executor: exec})
	created := env.createToken(t, map[string]any{"name": "compromised", "scopes": []string{"call"}})
	queued := addApprovalFor(t, env.store, created.Info.ID, time.Now(), 15*time.Minute)

	require.Equal(t, http.StatusOK, env.do("DELETE", "/api/agent/tokens/"+created.Info.ID, nil).Code)
	w := env.do("POST", "/api/agent/approvals/"+queued.ID+"/approve", nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Empty(t, exec.calls, "nothing a revoked token queued may run")

	w = env.do("GET", "/api/agent/approvals", nil)
	require.Equal(t, http.StatusOK, w.Code)
	list := data[approvalList](t, w)
	require.Len(t, list.Approvals, 1)
	assert.Equal(t, storage.ApprovalRejected, list.Approvals[0].Status)
	assert.Equal(t, storage.ApprovalDecidedByTokenRevoked, list.Approvals[0].DecidedBy)
	assert.Equal(t, AgentTokenRevoked, list.Approvals[0].TokenStatus)
	assert.Zero(t, list.Pending)

	// A request queued in the instant after the revocation is refused too.
	late := addApprovalFor(t, env.store, created.Info.ID, time.Now(), 15*time.Minute)
	w = env.do("POST", "/api/agent/approvals/"+late.ID+"/approve", nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, errorMessage(t, w), "revoked")
	assert.Empty(t, exec.calls)
	stored, err := env.store.GetAgentApproval(late.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRejected, stored.Status)
}

func TestRecoveryVoidsQueuedApprovals(t *testing.T) {
	exec := &recordingExecutor{}
	env := newAgentTestEnv(t, AgentHandlerOptions{Executor: exec})
	created := env.createToken(t, map[string]any{"name": "compromised", "scopes": []string{"call"}})
	call := addApprovalFor(t, env.store, created.Info.ID, time.Now(), 15*time.Minute)
	deploy, err := env.store.CreateAgentApproval(storage.AgentApproval{Kind: storage.ApprovalKindDraftDeploy, TokenID: created.Info.ID, Ref: "draft-1"}, time.Now())
	require.NoError(t, err)

	code, err := env.users.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	w := authRequest(env.router, "POST", "/api/auth/recover", map[string]string{"username": "admin", "password": "replacement-password", "code": code}, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = authRequest(env.router, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "replacement-password"}, nil)
	require.Equal(t, http.StatusOK, w.Code)
	env.cookie = w.Result().Cookies()[0]

	for _, id := range []string{call.ID, deploy.ID} {
		w = env.do("POST", "/api/agent/approvals/"+id+"/approve", nil)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	}
	assert.Empty(t, exec.calls, "recovery treats everything the old tokens queued as hostile")
}

func TestListApprovalsReportsTokenStatus(t *testing.T) {
	clock := &settableClock{now: approvalsT0}
	env := newAgentTestEnv(t, AgentHandlerOptions{Now: clock.Now})
	active := addApproval(t, env.store, approvalsT0, 15*time.Minute)
	short, _, err := env.store.CreateAPIToken(storage.NewAPIToken{Name: "short", Scopes: []string{storage.ScopeCall}, ExpiresAt: approvalsT0.Add(time.Minute)}, approvalsT0)
	require.NoError(t, err)
	expired := addApprovalFor(t, env.store, short.ID, approvalsT0, 15*time.Minute)
	orphan := addApprovalFor(t, env.store, "no-such-token", approvalsT0, 15*time.Minute)
	clock.Set(approvalsT0.Add(2 * time.Minute))

	w := env.do("GET", "/api/agent/approvals", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := map[string]string{}
	for _, a := range data[approvalList](t, w).Approvals {
		got[a.ID] = a.TokenStatus
	}
	assert.Equal(t, map[string]string{active.ID: AgentTokenActive, expired.ID: AgentTokenExpired, orphan.ID: AgentTokenUnknown}, got)

	w = env.do("POST", "/api/agent/approvals/"+expired.ID+"/approve", nil)
	require.Equal(t, http.StatusConflict, w.Code, "an expired token cannot have its request approved")
}
