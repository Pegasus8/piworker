package service

import (
	"encoding/json"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No equality oracles (docs/design/mcp.md §10): an agent may not confirm a
// guess of a value it may not read by comparing how a correct and a wrong
// guess are answered. Only what the agent did not write (a "<redacted>"
// sentinel, or a property its ops left alone) counts as the base's value.

const adminLiteral = "Bearer hunter2"

// literalFlow is lightsFlow with a credential the admin typed in.
func literalFlow() *flow.Flow {
	f := lightsFlow()
	nodeOf(f, "call").Config["headers"] = map[string]any{"Authorization": adminLiteral}
	return f
}

func TestSensitiveLiteralGuessesAreAllRefused(t *testing.T) {
	e := newAuthEnv(t)
	stored := e.create(literalFlow())
	agent := e.token()
	for _, guess := range []string{"Bearer password", adminLiteral} {
		t.Run(guess, func(t *testing.T) {
			ops := []flow.Op{{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"headers": map[string]any{"Authorization": guess}}}}
			res, err := e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: stored.ID, Ops: ops})
			require.NoError(t, err)
			assert.Contains(t, issueCodes(res.Issues), IssueSensitiveLiteral, "a written literal, equal to the base's or not")
			assert.False(t, res.Valid)

			whole := literalFlow()
			whole.ID = stored.ID
			nodeOf(whole, "call").Config["headers"] = map[string]any{"Authorization": guess}
			nodeOf(whole, "call").Config["url"] = RedactedValue
			_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Flow: whole})
			te := requireKind(t, err, ErrInvalidArguments)
			assert.Contains(t, issueCodes(te.FlowIssues), IssueSensitiveLiteral)
		})
	}

	// What the agent did not write stays: the sentinel, or ops that leave
	// the property alone.
	whole := literalFlow()
	whole.ID = stored.ID
	nodeOf(whole, "call").Config["headers"] = RedactedValue
	nodeOf(whole, "call").Config["url"] = RedactedValue
	_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Flow: whole})
	require.NoError(t, err)
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"method": "PUT"}},
	}})
	require.NoError(t, err)
}

func TestWebhookSettingGuessesAreAllForbidden(t *testing.T) {
	e := newAuthEnv(t)
	hooked := safeFlow()
	addNode(hooked, "hook", "trigger-webhook", map[string]any{"path": "zz-hidden-path-4f2a9c", "token": "tok-literal"})
	connect(hooked, "hook", "calc")
	stored := e.create(hooked)
	agent := e.token()
	for name, cfg := range map[string]map[string]any{
		"wrong path": {"path": "guess"}, "right path": {"path": "zz-hidden-path-4f2a9c"},
		"wrong token": {"token": "guess"}, "right token": {"token": "tok-literal"},
	} {
		t.Run(name, func(t *testing.T) {
			ops := []flow.Op{{Op: flow.OpUpdateNode, NodeID: "hook", Config: cfg}}
			_, err := e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: stored.ID, Ops: ops})
			requireKind(t, err, ErrPermissionDenied)
			_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: ops})
			requireKind(t, err, ErrPermissionDenied)
		})
	}

	// Kept through the sentinel or left alone, they may stay.
	whole, err := cloneFlow(stored)
	require.NoError(t, err)
	nodeOf(whole, "hook").Config["path"] = RedactedValue
	nodeOf(whole, "hook").Config["token"] = RedactedValue
	nodeOf(whole, "calc").Config["expression"] = "payload + 1"
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Flow: whole})
	require.NoError(t, err)
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "hook", Config: map[string]any{"method": "POST"}},
	}})
	require.NoError(t, err)
}

// staticFlow has two templates whose texts are hidden from agents.
func staticFlow() *flow.Flow {
	f := flow.NewFlow("static")
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "t1", "process-template", map[string]any{"template": "HIDDENSTATIC"})
	addNode(f, "t2", "process-template", map[string]any{"template": "second"})
	connect(f, "start", "t1")
	connect(f, "start", "t2")
	return f
}

// In a dry-run of a draft, what the agent kept of the base is hidden from the
// run; what it wrote runs as written, whether or not it equals a hidden base
// value.
func TestDraftDryRunShowsGuessesAsWritten(t *testing.T) {
	e := newAuthEnv(t)
	stored := e.create(staticFlow())
	agent := e.token()
	for _, guess := range []string{"third", "second", "HIDDENSTATIC"} {
		t.Run(guess, func(t *testing.T) {
			saved := e.draft(agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
				{Op: flow.OpUpdateNode, NodeID: "t2", Config: map[string]any{"template": guess}},
			}})
			res, err := e.a.DryRun(t.Context(), agent, DryRunRequest{DraftID: saved.DraftID, Payload: "x"})
			require.NoError(t, err)
			assert.Equal(t, guess, outputOf(t, res, "t2"), "the agent's own text")
			assert.Equal(t, RedactedValue, outputOf(t, res, "t1"), "kept: hidden from the run")
		})
	}
}

// By flowId the run never holds a hidden value, so a payload equal to one is
// shown like any other.
func TestStoredFlowDryRunShowsPayloadGuessesAsSent(t *testing.T) {
	e := newAuthEnv(t)
	f := flow.NewFlow("echo")
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "key", sensitiveEchoType, map[string]any{"apiKey": adminLiteral})
	addNode(f, "wait", "process-delay", map[string]any{"delay": float64(1)})
	connect(f, "start", "key")
	connect(f, "key", "wait")
	stored := e.create(f)
	agent := e.token()
	for _, guess := range []string{"Bearer password", adminLiteral} {
		res, err := e.a.DryRun(t.Context(), agent, DryRunRequest{FlowID: stored.ID, TriggerNodeID: "start", Payload: guess})
		require.NoError(t, err)
		assert.Equal(t, guess, outputOf(t, res, "wait"), "%+v", res)
	}
}

// A new flow's draft is all the agent's, before and after the admin approved
// it (which records the created flow's ID on the draft).
func TestApprovedNewFlowDraftDryRunIsTheAgents(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	fresh := e.draft(agent, DraftInput{Flow: templateFlow(), Name: "fresh"})
	_, err := e.a.ApproveDraft(t.Context(), EditorActor(), fresh.DraftID, false)
	require.NoError(t, err)
	res, err := e.a.DryRun(t.Context(), agent, DryRunRequest{DraftID: fresh.DraftID, Payload: "x"})
	require.NoError(t, err)
	assert.Equal(t, "BASEHIDDENWORDS x", outputOf(t, res, "t1"))
}

// An object patch merged into the base's object keeps the base's members:
// only the patch is the agent's (it may hold only references), and the
// merged setting counts as kept, so a dry-run of the draft hides it.
func TestMergedPatchIsKept(t *testing.T) {
	e := newAuthEnv(t)
	stored := e.create(literalFlow())
	agent := e.token()
	saved := e.draft(agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"headers": map[string]any{"X-Extra": "{{secret.api_key}}"}}},
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 1"}},
	}})
	d, proposed := storedDraft(t, e, saved.DraftID)
	assert.Equal(t, map[string]any{"Authorization": adminLiteral, "X-Extra": "{{secret.api_key}}"}, nodeOf(proposed, "call").Config["headers"])
	var kept keptSettings
	require.NoError(t, json.Unmarshal(d.KeptJSON, &kept))
	assert.Contains(t, kept.Props, [2]string{"call", "headers"}, "merged into the admin's headers")
	assert.Contains(t, kept.Props, [2]string{"call", "url"}, "left alone")
	assert.NotContains(t, kept.Props, [2]string{"calc", "expression"}, "written")

	_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"headers": map[string]any{"X-Extra": "typed"}}},
	}})
	te := requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, issueCodes(te.FlowIssues), IssueSensitiveLiteral, "what the agent merged in is checked")
}

// A draft stored without a readable kept list hides every hidden setting.
func TestKeptRedactionWithoutAListHidesEverything(t *testing.T) {
	e := newAuthEnv(t)
	n := &templateFlow().Nodes[1]
	for _, raw := range []string{`[]`, ``, `{"props":null}`} {
		redact := e.a.keptRedaction(json.RawMessage(raw))
		assert.True(t, redact(n, "template"), "%q", raw)
	}
	redact := e.a.keptRedaction(json.RawMessage(`{"props":[]}`))
	assert.False(t, redact(n, "template"), "nothing kept")
}

// Reading a stored flow through authoring (validating, drafting or dry-running
// it, reading a draft of it) needs the read scope too: the author scope
// alone works on the agent's own flows only.
func TestAuthoringOnStoredFlowsNeedsTheReadScope(t *testing.T) {
	e := newAuthEnv(t)
	stored := e.create(safeFlow())
	authorOnly := e.token(storage.ScopeAuthor)
	ops := []flow.Op{{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 1"}}}

	_, err := e.a.Validate(t.Context(), authorOnly, ValidateRequest{FlowID: stored.ID})
	te := requireKind(t, err, ErrPermissionDenied)
	assert.Contains(t, te.Message, "read scope")
	_, err = e.a.SaveDraft(t.Context(), authorOnly, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: ops})
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.DryRun(t.Context(), authorOnly, DryRunRequest{FlowID: stored.ID})
	requireKind(t, err, ErrPermissionDenied)

	// A draft of a stored flow (stored for it directly here).
	d, err := e.store.CreateFlowDraft(storage.FlowDraft{FlowID: stored.ID, BaseVersion: 1, Data: json.RawMessage(mustJSON(t, stored)),
		AuthorTokenID: authorOnly.TokenID, AuthorTokenName: authorOnly.TokenName, Risk: "safe"})
	require.NoError(t, err)
	_, err = e.a.GetDraft(t.Context(), authorOnly, d.ID)
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.DryRun(t.Context(), authorOnly, DryRunRequest{DraftID: d.ID})
	requireKind(t, err, ErrPermissionDenied)

	// Its own flows need no more than the author scope.
	_, err = e.a.Validate(t.Context(), authorOnly, ValidateRequest{Flow: safeFlow()})
	require.NoError(t, err)
	fresh := e.draft(authorOnly, DraftInput{Flow: safeFlow(), Name: "mine"})
	_, err = e.a.GetDraft(t.Context(), authorOnly, fresh.DraftID)
	require.NoError(t, err)
	_, err = e.a.DryRun(t.Context(), authorOnly, DryRunRequest{DraftID: fresh.DraftID})
	require.NoError(t, err)
}
