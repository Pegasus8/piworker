package api

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type auditPage struct {
	Entries    []storage.MCPAuditEntry `json:"entries"`
	Rejections []storage.MCPRejection  `json:"rejections"`
}

func TestListAudit(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	var batch []storage.MCPAuditEntry
	for i := range 4 {
		e := storage.MCPAuditEntry{
			TS: base.Add(time.Duration(i) * time.Minute), TokenID: "tok-a", TokenName: "laptop",
			Tool: "lights.on", FlowID: "f1", RunID: fmt.Sprintf("run-%d", i), Outcome: "completed", DurMs: 5,
		}
		if i%2 == 1 {
			e.TokenID, e.Tool, e.FlowID, e.Outcome, e.Privileged = "tok-b", "door.open", "f2", "error", true
		}
		batch = append(batch, e)
	}
	require.NoError(t, env.store.InsertMCPAudit(batch, time.Now()))
	require.NoError(t, env.store.AddMCPRejections([]storage.MCPRejection{
		{TokenID: "tok-a", At: base, Reason: "busy", Count: 3},
		{TokenID: "tok-b", At: base, Reason: "cooldown", Count: 1},
	}, time.Now()))

	runs := func(query string) []string {
		t.Helper()
		w := env.do("GET", "/api/agent/audit"+query, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		page := data[auditPage](t, w)
		ids := []string{}
		for _, e := range page.Entries {
			ids = append(ids, e.RunID)
		}
		return ids
	}

	w := env.do("GET", "/api/agent/audit", nil)
	require.Equal(t, http.StatusOK, w.Code)
	page := data[auditPage](t, w)
	require.Len(t, page.Entries, 4)
	assert.Equal(t, "run-3", page.Entries[0].RunID)
	assert.True(t, page.Entries[0].Privileged)
	assert.Len(t, page.Rejections, 2)

	assert.Equal(t, []string{"run-3", "run-1"}, runs("?tokenId=tok-b"))
	assert.Equal(t, []string{"run-2", "run-0"}, runs("?tool=lights.on"))
	assert.Equal(t, []string{"run-3", "run-1"}, runs("?flowId=f2"))
	assert.Equal(t, []string{"run-2", "run-0"}, runs("?outcome=completed"))
	assert.Equal(t, []string{"run-3"}, runs("?limit=1"))
	q := url.Values{"since": {base.Add(time.Minute).Format(time.RFC3339)}, "until": {base.Add(3 * time.Minute).Format(time.RFC3339)}}
	assert.Equal(t, []string{"run-2", "run-1"}, runs("?"+q.Encode()))
	assert.Equal(t, []string{"run-1", "run-0"}, runs(fmt.Sprintf("?beforeId=%d", page.Entries[1].ID)))

	w = env.do("GET", "/api/agent/audit?tokenId=tok-a", nil)
	require.Equal(t, http.StatusOK, w.Code)
	filtered := data[auditPage](t, w)
	require.Len(t, filtered.Rejections, 1)
	assert.Equal(t, "busy", filtered.Rejections[0].Reason)
	assert.Equal(t, 3, filtered.Rejections[0].Count)

	for _, bad := range []string{"?since=yesterday", "?until=1", "?limit=x", "?limit=0", "?beforeId=-2", "?beforeId=x"} {
		require.Equal(t, http.StatusBadRequest, env.do("GET", "/api/agent/audit"+bad, nil).Code, bad)
	}
}
