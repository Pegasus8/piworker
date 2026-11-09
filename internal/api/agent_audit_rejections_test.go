package api

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rejectionTotals struct {
	Totals    []storage.MCPRejectionTotal `json:"totals"`
	Truncated bool                        `json:"truncated"`
}

// The audit page reads rejection totals per token and reason from the
// server instead of summing a page of per-minute counters.
func TestRejectionTotals(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	tok, _, err := env.store.CreateAPIToken(storage.NewAPIToken{Name: "laptop", Scopes: []string{storage.ScopeCall}}, time.Now())
	require.NoError(t, err)
	base := time.Now().Add(-time.Hour).Truncate(time.Minute).UTC()
	require.NoError(t, env.store.AddMCPRejections([]storage.MCPRejection{
		{TokenID: tok.ID, At: base, Reason: "busy", Count: 3},
		{TokenID: tok.ID, At: base.Add(time.Minute), Reason: "busy", Count: 2},
		{TokenID: "tok-b", At: base.Add(2 * time.Minute), Reason: "cooldown", Count: 1},
	}, time.Now()))

	w := env.do("GET", "/api/agent/audit/rejections", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := data[rejectionTotals](t, w)
	assert.False(t, got.Truncated)
	assert.Equal(t, []storage.MCPRejectionTotal{
		{TokenID: tok.ID, TokenName: "laptop", Reason: "busy", Count: 5, Last: base.Add(time.Minute)},
		{TokenID: "tok-b", Reason: "cooldown", Count: 1, Last: base.Add(2 * time.Minute)},
	}, got.Totals)

	q := url.Values{"tokenId": {tok.ID}, "since": {base.Add(time.Minute).Format(time.RFC3339)}, "until": {base.Add(time.Hour).Format(time.RFC3339)}}
	w = env.do("GET", "/api/agent/audit/rejections?"+q.Encode(), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got = data[rejectionTotals](t, w)
	require.Len(t, got.Totals, 1)
	assert.Equal(t, int64(2), got.Totals[0].Count)

	for _, bad := range []string{"since=yesterday", "until=2026-13-01T00:00:00Z"} {
		require.Equal(t, http.StatusBadRequest, env.do("GET", "/api/agent/audit/rejections?"+bad, nil).Code, bad)
	}
	require.Equal(t, http.StatusUnauthorized, authRequest(env.router, "GET", "/api/agent/audit/rejections", nil, nil).Code)
}
