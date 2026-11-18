package service

import (
	"runtime"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateReportsLintAuthoringIssuesAndRisk(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()

	res, err := e.a.Validate(t.Context(), agent, ValidateRequest{Flow: lightsFlow()})
	require.NoError(t, err)
	assert.True(t, res.Valid, "%v", res.Issues)
	assert.Equal(t, node.RiskExternal, res.Risk)
	assert.NotNil(t, res.Issues)

	bad := safeFlow()
	addNode(bad, "wait", "process-delay", map[string]any{"delay": apiKeyCanary})
	connect(bad, "calc", "wait")
	addNode(bad, "call", "action-http", map[string]any{"url": "https://api.example.com", "headers": map[string]any{"X": "literal"}})
	connect(bad, "wait", "call")
	res, err = e.a.Validate(t.Context(), agent, ValidateRequest{Flow: bad})
	require.NoError(t, err, "an invalid flow is a verdict, not an error")
	assert.False(t, res.Valid)
	assert.Contains(t, issueCodes(res.Issues), flow.IssueConfigType)
	assert.Contains(t, issueCodes(res.Issues), IssueSensitiveLiteral)
	assert.Contains(t, mustJSON(t, res), apiKeyCanary, "what the agent wrote comes back as it wrote it, a secret's value or not")

	// An undefined secret a strict setting needs is an error deploy would
	// refuse, but not a blocking one: the admin may define it.
	strict := safeFlow()
	addNode(strict, "tg", "action-telegram", map[string]any{"botToken": "{{secret.bot}}", "chatId": "1"})
	connect(strict, "calc", "tg")
	res, err = e.a.Validate(t.Context(), agent, ValidateRequest{Flow: strict})
	require.NoError(t, err)
	assert.True(t, res.Valid)
	assert.Contains(t, issueCodes(res.Issues), flow.IssueUndefinedSecret)
	assert.NotContains(t, issueCodes(res.Issues), IssueUnknownSecret, "Lint's error is enough")

	rows := e.auditRows()
	require.Len(t, rows, 3)
	assert.Equal(t, AuthorToolValidateFlow, rows[0].Tool)
}

// With flowId the proposal is resolved as SaveDraft resolves it: ops or the
// sentinel keep the current version's values, and the author restrictions
// apply; flowId alone validates the current version.
func TestValidateAgainstAStoredFlow(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()

	res, err := e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: base.ID, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"url": RedactedValue, "method": "GET"}},
	}})
	require.NoError(t, err)
	assert.True(t, res.Valid, "%v", res.Issues)

	res, err = e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: base.ID})
	require.NoError(t, err)
	assert.True(t, res.Valid)
	assert.Equal(t, node.RiskExternal, res.Risk)

	_, err = e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: base.ID, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"allowPrivate": true}},
	}})
	requireKind(t, err, ErrPermissionDenied)

	_, err = e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: "nope"})
	requireKind(t, err, ErrFlowNotFound)

	_, err = e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: base.ID, Ops: []flow.Op{{Op: "explode"}}})
	te := requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, issueCodes(te.FlowIssues), flow.IssueUnknownOp)
}

// The editor validates its own flows: no author restrictions, no authoring
// issues.
func TestValidateForTheEditor(t *testing.T) {
	e := newAuthEnv(t)
	f := safeFlow()
	addNode(f, "call", "action-http", map[string]any{
		"url": "http://192.168.1.10/x", "allowPrivate": true, "headers": map[string]any{"X": "admin literal"},
	})
	connect(f, "calc", "call")
	res, err := e.a.Validate(t.Context(), EditorActor(), ValidateRequest{Flow: f})
	require.NoError(t, err)
	assert.True(t, res.Valid, "%v", res.Issues)
	assert.NotContains(t, issueCodes(res.Issues), IssueSensitiveLiteral)
	assert.Equal(t, node.RiskPrivileged, res.Risk, "allowPrivate escalates the instance")
	assert.Empty(t, e.auditRows(), "the editor is not audited as an agent")

	_, err = e.a.Validate(t.Context(), e.token(storage.ScopeRead), ValidateRequest{Flow: f})
	requireKind(t, err, ErrPermissionDenied)
}

// An author token's pw_validate_flow of one 480 KB flow (ten mistyped
// settings on a node with 240,000 hidden leaves) used to allocate 3.7 GB:
// Lint hid the node's settings from each issue again.
func TestValidateCostIsBounded(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token(storage.ScopeAuthor)
	leaves := make([]any, 240_000)
	for i := range leaves {
		leaves[i] = 1.0
	}
	cfg := map[string]any{"zz": leaves, "url": 1.0, "method": 1.0, "headers": 1.0, "body": 1.0, "bodyType": 1.0,
		"outputMode": 1.0, "timeout": "x", "maxResponseSize": "x", "allowPrivate": "x", "retryable": "x"}
	f := safeFlow()
	addNode(f, "big", "action-http", cfg)
	connect(f, "calc", "big")
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	res, err := e.a.Validate(t.Context(), agent, ValidateRequest{Flow: f})
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	assert.False(t, res.Valid)
	alloc := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, alloc, uint64(400<<20), "Validate allocated %d MiB", alloc>>20)
}
