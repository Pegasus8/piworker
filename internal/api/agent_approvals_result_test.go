package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storingExecutor stores a CallResult on the approval, as the tool service
// does.
type storingExecutor struct {
	store  *storage.SQLiteStore
	result json.RawMessage
}

func (e *storingExecutor) ExecuteApproval(_ context.Context, a storage.AgentApproval) error {
	return e.store.SetAgentApprovalResult(a.ID, e.result)
}

// The admin sees what the approved call returned in the decision itself.
func TestApproveAnswersTheStoredResult(t *testing.T) {
	exec := &storingExecutor{result: json.RawMessage(`{"status":"completed","runId":"run-1","result":{"moved":true},"durationMs":4}`)}
	env := newAgentTestEnv(t, AgentHandlerOptions{Executor: exec})
	exec.store = env.store
	a := addApproval(t, env.store, time.Now(), 15*time.Minute)

	w := env.do("POST", "/api/agent/approvals/"+a.ID+"/approve", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	d := data[approvalDecision](t, w)
	assert.True(t, d.Executed)
	assert.JSONEq(t, string(exec.result), string(d.Approval.Result))
}
