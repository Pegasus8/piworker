package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pendingCount struct {
	Pending int `json:"pending"`
}

func sessionLastSeen(t *testing.T, env agentTestEnv) int64 {
	t.Helper()
	var lastSeen int64
	require.NoError(t, env.store.DB().QueryRow(`SELECT last_seen FROM auth_sessions`).Scan(&lastSeen))
	return lastSeen
}

// ageSession moves the signed-in session's last activity d into the past.
func ageSession(t *testing.T, env agentTestEnv, d time.Duration) {
	t.Helper()
	_, err := env.store.DB().Exec(`UPDATE auth_sessions SET last_seen = last_seen - ?`, int64(d/time.Second))
	require.NoError(t, err)
}

// The editor badge polls the pending count in every open tab; that alone
// must not keep a session alive (the 7-day idle timeout) nor write to the
// database.
func TestPendingCountDoesNotExtendTheSession(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	ageSession(t, env, time.Hour)
	before := sessionLastSeen(t, env)

	w := env.do("GET", "/api/agent/approvals/pending-count", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, before, sessionLastSeen(t, env), "polling the count leaves the idle expiry alone")
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	// Any other request is activity and extends it.
	require.Equal(t, http.StatusOK, env.do("GET", "/api/private", nil).Code)
	assert.Greater(t, sessionLastSeen(t, env), before)
}

func TestPendingCountRequiresALiveSession(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	w := authRequest(env.router, "GET", "/api/agent/approvals/pending-count", nil, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// The idle timeout is still enforced: polling cannot revive a session.
	ageSession(t, env, storage.SessionIdleTimeout+time.Minute)
	w = env.do("GET", "/api/agent/approvals/pending-count", nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// The count is read-only: approvals past their expiry that nobody closed yet
// are not counted, and are left as they are.
func TestPendingCountIgnoresExpiredApprovals(t *testing.T) {
	clock := &settableClock{now: approvalsT0}
	env := newAgentTestEnv(t, AgentHandlerOptions{Now: clock.Now})
	short := addApproval(t, env.store, approvalsT0, time.Minute)
	addApproval(t, env.store, approvalsT0, 15*time.Minute)
	decided := addApproval(t, env.store, approvalsT0, 15*time.Minute)
	_, err := env.store.DecideAgentApproval(decided.ID, false, "admin", approvalsT0)
	require.NoError(t, err)

	w := env.do("GET", "/api/agent/approvals/pending-count", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 2, data[pendingCount](t, w).Pending)

	clock.Set(approvalsT0.Add(2 * time.Minute))
	w = env.do("GET", "/api/agent/approvals/pending-count", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, data[pendingCount](t, w).Pending, "an approval past its expiry no longer waits")

	got, err := env.store.GetAgentApproval(short.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalPending, got.Status, "counting writes nothing; the expiry pass closes it later")
}
