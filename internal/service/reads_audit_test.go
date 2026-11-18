package service

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every read an agent makes is audited like a tool call (ADR-0010: agent
// tokens are audited): which token read what (the tool, the flow and run
// it named, its arguments), the outcome and how long it took. The editor's
// reads are not.
func TestAgentReadsAreAudited(t *testing.T) {
	e := newReadsEnv(t)
	audit := NewAuditWriter(e.store, AuditOptions{FlushInterval: time.Hour, Now: e.clock.now})
	e.reads.audit = audit // what ReadsOptions.Audit sets
	require.NoError(t, e.secrets.Set("KEY", "s3cret-in-an-id"))
	f := e.save(manualFlow("audited"))
	e.addRun(f.ID, "run-1", "", 0, time.Second, "success")

	ctx := context.Background()
	agent := readsAgent(storage.ScopeRead)
	_, err := e.reads.ListFlows(ctx, agent)
	require.NoError(t, err)
	_, err = e.reads.GetFlow(ctx, agent, f.ID)
	require.NoError(t, err)
	_, err = e.reads.ListRuns(ctx, agent, f.ID, 5, "run-1")
	require.NoError(t, err)
	_, err = e.reads.GetRun(ctx, agent, f.ID, "run-1")
	require.NoError(t, err)
	_, err = e.reads.GetRun(ctx, agent, f.ID, "no-such-run")
	require.Error(t, err)
	_, err = e.reads.GetFlow(ctx, agent, "flow s3cret-in-an-id")
	require.Error(t, err)
	_, err = e.reads.SystemStatus(ctx, agent)
	require.NoError(t, err)
	_, err = e.reads.DiagnoseFlow(ctx, agent, f.ID)
	require.NoError(t, err)
	_, err = e.reads.ListFlows(ctx, readsAgent(storage.ScopeCall))
	require.Error(t, err)
	// The editor's reads are not audited.
	_, err = e.reads.GetFlow(ctx, EditorActor(), f.ID)
	require.NoError(t, err)

	require.NoError(t, audit.Flush())
	rows, err := e.store.ListMCPAudit(storage.MCPAuditFilter{})
	require.NoError(t, err)
	type row struct{ tool, flowID, runID, preview, outcome string }
	var got []row
	for i := len(rows) - 1; i >= 0; i-- { // oldest first
		r := rows[i]
		assert.Equal(t, "tok-1", r.TokenID)
		assert.Equal(t, "reader", r.TokenName)
		assert.False(t, r.Privileged)
		assert.Equal(t, e.clock.now().UnixMilli(), r.TS.UnixMilli())
		assert.Len(t, r.ArgsSHA256, 64)
		got = append(got, row{r.Tool, r.FlowID, r.RunID, r.ArgsPreview, r.Outcome})
	}
	assert.Equal(t, []row{
		{"pw_list_flows", "", "", `{}`, "completed"},
		{"pw_get_flow", f.ID, "", `{"flowId":"` + f.ID + `"}`, "completed"},
		{"pw_list_runs", f.ID, "", `{"flowId":"` + f.ID + `","limit":5,"before":"run-1"}`, "completed"},
		{"pw_get_run", f.ID, "run-1", `{"flowId":"` + f.ID + `","runId":"run-1"}`, "completed"},
		{"pw_get_run", f.ID, "no-such-run", `{"flowId":"` + f.ID + `","runId":"no-such-run"}`, "run_not_found"},
		{"pw_get_flow", "flow <redacted>", "", `{"flowId":"flow <redacted>"}`, "flow_not_found"},
		{"pw_system_status", "", "", `{}`, "completed"},
		{"prompt:diagnose_flow", f.ID, "", `{"flowId":"` + f.ID + `"}`, "completed"},
		{"pw_list_flows", "", "", `{}`, "forbidden"},
	}, got)
}
