package service

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetDraft shows an agent its own drafts, the proposed flow through the read
// views' default-deny redaction.
func TestGetDraftShowsTheAuthorItsDraftRedacted(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()
	saved, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Note: "switch to PUT", Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"method": "PUT"}},
	}})
	require.NoError(t, err)

	view, err := e.a.GetDraft(t.Context(), agent, saved.DraftID)
	require.NoError(t, err)
	assert.Equal(t, saved.DraftID, view.DraftID)
	assert.Equal(t, storage.DraftPending, view.Status)
	assert.Equal(t, base.ID, view.FlowID)
	assert.Equal(t, 1, view.BaseVersion)
	assert.Equal(t, "switch to PUT", view.Note)
	assert.Equal(t, saved.Risk, view.Risk)
	assert.Equal(t, saved.Alerts, view.Alerts)
	assert.Equal(t, saved.Issues, view.Issues)
	assert.False(t, view.CreatedAt.IsZero())
	assert.Nil(t, view.DecidedAt)
	assert.Zero(t, view.AppliedVersion)

	assert.Equal(t, base.ID, view.Flow.ID)
	assert.Equal(t, "lights", view.Flow.Name)
	assert.Equal(t, 3, view.Flow.TotalNodes)
	assert.Equal(t, 2, view.Flow.TotalConnections)
	require.Len(t, view.Flow.Nodes, 3)
	byID := map[string]NodeView{}
	for _, n := range view.Flow.Nodes {
		byID[n.ID] = n
	}
	call := byID["call"]
	assert.Equal(t, "PUT", call.Config["method"], "AgentVisible values are shown")
	assert.Equal(t, RedactedValue, call.Config["url"], "hidden values are not")
	assert.Equal(t, RedactedValue, call.Config["headers"])
	assert.Equal(t, RedactedValue, byID["calc"].Config["expression"])
	assert.NotContains(t, mustJSON(t, view), "very-private-path")
	assert.NotContains(t, mustJSON(t, view), apiKeyCanary)

	rows := e.auditRows()
	require.Len(t, rows, 2)
	assert.Equal(t, AuthorToolGetDraft, rows[0].Tool, "newest first")
	assert.Equal(t, base.ID, rows[0].FlowID)
}

func TestGetDraftOfOthersIsUnknown(t *testing.T) {
	e := newAuthEnv(t)
	author := e.token()
	saved, err := e.a.SaveDraft(t.Context(), author, DraftInput{Flow: safeFlow(), Name: "mine"})
	require.NoError(t, err)

	other := e.token()
	_, err = e.a.GetDraft(t.Context(), other, saved.DraftID)
	unknown := requireKind(t, err, ErrToolNotFound)
	_, err = e.a.GetDraft(t.Context(), other, "no-such-draft")
	missing := requireKind(t, err, ErrToolNotFound)
	assert.Equal(t, missing.Code(), unknown.Code(), "another token's draft reads as an unknown one")
	assert.NotContains(t, unknown.Message, "mine")

	_, err = e.a.GetDraft(t.Context(), EditorActor(), saved.DraftID)
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.GetDraft(t.Context(), e.token(storage.ScopeRead), saved.DraftID)
	requireKind(t, err, ErrPermissionDenied)
}

// The admin's reason for a rejection reaches the author as plain, redacted
// text.
func TestGetDraftShowsTheDecision(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	saved, err := e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: safeFlow(), Name: "new one"})
	require.NoError(t, err)
	require.NoError(t, e.store.DecideFlowDraft(saved.DraftID, storage.DraftRejected,
		"no: it would post "+apiKeyCanary+" to https://hooks.example.com/services/T0/B0/secretpath", 0))

	view, err := e.a.GetDraft(t.Context(), agent, saved.DraftID)
	require.NoError(t, err)
	assert.Equal(t, storage.DraftRejected, view.Status)
	assert.Contains(t, view.Reason, "no: it would post")
	assert.NotContains(t, view.Reason, apiKeyCanary)
	assert.NotContains(t, view.Reason, "secretpath")
	assert.NotNil(t, view.DecidedAt)
	assert.Empty(t, view.Flow.ID, "a new flow")
	assert.Equal(t, "new one", view.Flow.Name)
}
