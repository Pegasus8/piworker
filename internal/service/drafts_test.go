package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor's side of drafts (docs/design/mcp.md §10, contract C2).

func (e *authEnv) draft(agent Actor, in DraftInput) DraftResult {
	e.t.Helper()
	res, err := e.a.SaveDraft(e.t.Context(), agent, in)
	require.NoError(e.t, err)
	return res
}

func TestListDraftsForTheEditor(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()
	older := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Note: strings.Repeat("n", 300), Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"url": "https://other.example.org/x"}},
	}})
	newer := e.draft(agent, DraftInput{Flow: safeFlow(), Name: "Proposed name", Note: "new flow"})
	decided := e.draft(agent, DraftInput{Flow: safeFlow(), Name: "rejected one"})
	require.NoError(t, e.a.RejectDraft(t.Context(), EditorActor(), decided.DraftID, "no"))

	list, err := e.a.ListDrafts(t.Context(), EditorActor(), DraftQuery{})
	require.NoError(t, err)
	require.Len(t, list.Drafts, 2, "pending by default")
	assert.Empty(t, list.NextBefore)
	assert.Equal(t, newer.DraftID, list.Drafts[0].ID, "newest first")
	n := list.Drafts[0]
	assert.True(t, n.IsNew)
	assert.Equal(t, "Proposed name", n.FlowName)
	assert.Equal(t, storage.DraftPending, n.Status)
	assert.False(t, n.Stale)
	assert.Equal(t, agent.TokenID, n.TokenID)
	assert.Equal(t, agent.TokenName, n.TokenName)
	assert.Nil(t, n.DecidedAt)

	o := list.Drafts[1]
	assert.False(t, o.IsNew)
	assert.Equal(t, base.ID, o.FlowID)
	assert.Equal(t, "lights", o.FlowName)
	assert.Equal(t, 1, o.BaseVersion)
	assert.Equal(t, 1, o.CurrentVersion)
	assert.Equal(t, node.RiskExternal, o.Risk)
	assert.Equal(t, len(older.Alerts), o.AlertCount)
	assert.Positive(t, o.AlertCount)
	assert.Len(t, []rune(o.Note), DraftNotePreviewChars, "a preview of the note")

	// A change of the flow makes its pending drafts stale.
	require.NoError(t, e.store.UpdateFlow(base))
	list, err = e.a.ListDrafts(t.Context(), EditorActor(), DraftQuery{Status: "pending"})
	require.NoError(t, err)
	assert.True(t, list.Drafts[1].Stale)
	assert.Equal(t, 2, list.Drafts[1].CurrentVersion)

	list, err = e.a.ListDrafts(t.Context(), EditorActor(), DraftQuery{Status: "decided"})
	require.NoError(t, err)
	require.Len(t, list.Drafts, 1)
	assert.Equal(t, storage.DraftRejected, list.Drafts[0].Status)
	assert.Equal(t, "no", list.Drafts[0].Reason)
	assert.NotNil(t, list.Drafts[0].DecidedAt)

	// Paging.
	page, err := e.a.ListDrafts(t.Context(), EditorActor(), DraftQuery{Status: "all", Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Drafts, 2)
	require.Equal(t, page.Drafts[1].ID, page.NextBefore)
	rest, err := e.a.ListDrafts(t.Context(), EditorActor(), DraftQuery{Status: "all", Limit: 2, BeforeID: page.NextBefore})
	require.NoError(t, err)
	require.Len(t, rest.Drafts, 1)
	assert.Equal(t, older.DraftID, rest.Drafts[0].ID)
	assert.Empty(t, rest.NextBefore)

	_, err = e.a.ListDrafts(t.Context(), EditorActor(), DraftQuery{Status: "weird"})
	requireKind(t, err, ErrInvalidArguments)
	_, err = e.a.ListDrafts(t.Context(), agent, DraftQuery{})
	requireKind(t, err, ErrPermissionDenied)
}

func TestGetDraftDetailRecomputesTheDiff(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()
	saved := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Note: "the whole note", Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{
			"url": "https://api.example.com/hook/other", "headers": map[string]any{"Authorization": "{{secret.api_key}}", "X-Other": "{{secret.later}}"},
		}},
	}})

	detail, err := e.a.GetDraftDetail(t.Context(), EditorActor(), saved.DraftID)
	require.NoError(t, err)
	assert.Equal(t, saved.DraftID, detail.ID)
	assert.Equal(t, "the whole note", detail.Note)
	assert.False(t, detail.Running)
	assert.Equal(t, 1, detail.LastApprovedVersion)
	assert.Nil(t, detail.DiffApproved, "the current version is the one the admin approved")
	require.Len(t, detail.Diff.Nodes, 1)
	nd := detail.Diff.Nodes[0]
	assert.Equal(t, "call", nd.NodeID)
	var urlField *flow.FieldDiff
	for i := range nd.Fields {
		if nd.Fields[i].Path == "config.url" {
			urlField = &nd.Fields[i]
		}
	}
	require.NotNil(t, urlField)
	assert.Equal(t, "https://api.example.com/hook/very-private-path", urlField.Before, "the admin sees full values")
	assert.Equal(t, "https://api.example.com/hook/other", urlField.After)
	assert.True(t, urlField.Destination)
	require.NotNil(t, detail.Flow)
	assert.Equal(t, base.ID, detail.Flow.ID)
	assert.Contains(t, issueCodes(detail.Issues), IssueUnknownSecret, "unknown secrets are recomputed too")

	// A draft based on an agent's version compares with the version the
	// admin last approved too.
	agentVersion := lightsFlow()
	agentVersion.ID = base.ID
	nodeOf(agentVersion, "calc").Config["expression"] = "payload * 3"
	_, err = e.store.UpdateFlowVersioned(agentVersion, 0, storage.VersionMeta{ActorKind: storage.VersionActorAgent, TokenID: agent.TokenID, TokenName: agent.TokenName})
	require.NoError(t, err)
	onAgent := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 2, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"method": "GET"}},
	}})
	detail, err = e.a.GetDraftDetail(t.Context(), EditorActor(), onAgent.DraftID)
	require.NoError(t, err)
	assert.Equal(t, 1, detail.LastApprovedVersion)
	require.NotNil(t, detail.DiffApproved)
	assert.Len(t, detail.DiffApproved.Nodes, 2, "calc and call differ from v1")

	// Sensitive literals in the proposed flow are hidden from the page.
	literal := lightsFlow()
	nodeOf(literal, "call").Config["headers"] = map[string]any{"Authorization": "Bearer typed-by-admin"}
	stored := e.create(literal)
	kept := e.draft(agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "1"}},
	}})
	detail, err = e.a.GetDraftDetail(t.Context(), EditorActor(), kept.DraftID)
	require.NoError(t, err)
	assert.Equal(t, flow.HiddenValue, nodeOf(detail.Flow, "call").Config["headers"])
	assert.NotContains(t, mustJSON(t, detail), "typed-by-admin")

	// Hiding them never changes what is compared: against the version the
	// admin last approved too, the untouched node is unchanged.
	e.agentVersion(stored.ID, agent, func(f *flow.Flow) { nodeOf(f, "calc").Config["expression"] = "2" })
	again := e.draft(agent, DraftInput{FlowID: stored.ID, BaseVersion: 2, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "3"}},
	}})
	detail, err = e.a.GetDraftDetail(t.Context(), EditorActor(), again.DraftID)
	require.NoError(t, err)
	require.NotNil(t, detail.DiffApproved)
	require.Len(t, detail.DiffApproved.Nodes, 1, "%+v", detail.DiffApproved.Nodes)
	assert.Equal(t, "calc", detail.DiffApproved.Nodes[0].NodeID)

	_, err = e.a.GetDraftDetail(t.Context(), EditorActor(), "nope")
	requireKind(t, err, ErrToolNotFound)
	_, err = e.a.GetDraftDetail(t.Context(), agent, kept.DraftID)
	requireKind(t, err, ErrPermissionDenied)
}

func TestApproveANewFlowDraft(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	saved := e.draft(agent, DraftInput{Flow: safeFlow(), Name: "Morning", Note: "please"})

	res, err := e.a.ApproveDraft(t.Context(), EditorActor(), saved.DraftID, false)
	require.NoError(t, err)
	require.NotEmpty(t, res.FlowID)
	assert.Equal(t, 1, res.Version)
	assert.False(t, res.Deployed)
	assert.Nil(t, res.DeployError)

	created, err := e.store.GetFlow(res.FlowID)
	require.NoError(t, err)
	assert.Equal(t, "Morning", created.Name)
	assert.Equal(t, flow.FlowStateInactive, created.State)
	v, err := e.store.GetFlowVersion(res.FlowID, 1)
	require.NoError(t, err)
	assert.True(t, v.AdminApproved)
	assert.Equal(t, storage.VersionActorEditor, v.ActorKind)
	assert.Equal(t, saved.DraftID, v.DraftID)
	assert.Equal(t, fmt.Sprintf("approved draft %s from %s", saved.DraftID, agent.TokenName), v.Note)
	d, err := e.store.GetFlowDraft(saved.DraftID)
	require.NoError(t, err)
	assert.Equal(t, storage.DraftApproved, d.Status, "the save closed the draft (H5)")
	assert.Equal(t, 1, d.AppliedVersion)
	assert.Equal(t, res.FlowID, d.FlowID)

	// Approving it again: no longer pending.
	_, err = e.a.ApproveDraft(t.Context(), EditorActor(), saved.DraftID, false)
	requireKind(t, err, ErrConflict)

	// With deploy, the new flow runs, deployed by the editor.
	second := e.draft(agent, DraftInput{Flow: safeFlow(), Name: "Evening"})
	res, err = e.a.ApproveDraft(t.Context(), EditorActor(), second.DraftID, true)
	require.NoError(t, err)
	assert.True(t, res.Deployed, "%+v", res.DeployError)
	dep, err := e.store.GetDeployment(res.FlowID)
	require.NoError(t, err)
	require.NotNil(t, dep)
	assert.Equal(t, storage.VersionActorEditor, dep.ActorKind)
}

func TestApproveAnExistingFlowDraft(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()
	saved := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 1"}},
	}})
	res, err := e.a.ApproveDraft(t.Context(), EditorActor(), saved.DraftID, false)
	require.NoError(t, err)
	assert.Equal(t, base.ID, res.FlowID)
	assert.Equal(t, 2, res.Version)
	assert.False(t, res.Deployed)
	stored, err := e.store.GetFlow(base.ID)
	require.NoError(t, err)
	assert.Equal(t, "payload + 1", nodeOf(stored, "calc").Config["expression"])
	assert.Equal(t, "https://api.example.com/hook/very-private-path", nodeOf(stored, "call").Config["url"])
	v, err := e.store.GetFlowVersion(base.ID, 2)
	require.NoError(t, err)
	assert.True(t, v.AdminApproved)
	assert.Equal(t, saved.DraftID, v.DraftID)

	// A draft based on an older version is stale: it must be redone.
	stale := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 2, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 2"}},
	}})
	require.NoError(t, e.store.UpdateFlow(stored)) // v3
	_, err = e.a.ApproveDraft(t.Context(), EditorActor(), stale.DraftID, false)
	te := requireKind(t, err, ErrConflict)
	assert.Equal(t, 3, te.CurrentVersion)
	d, err := e.store.GetFlowDraft(stale.DraftID)
	require.NoError(t, err)
	assert.Equal(t, storage.DraftPending, d.Status)

	_, err = e.a.ApproveDraft(t.Context(), EditorActor(), "nope", false)
	requireKind(t, err, ErrToolNotFound)
	_, err = e.a.ApproveDraft(t.Context(), agent, stale.DraftID, false)
	requireKind(t, err, ErrPermissionDenied)
}

// A running flow is always redeployed by an approval; a failed deploy is
// reported while the version stands.
func TestApproveRedeploysARunningFlow(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(safeFlow())
	require.NoError(t, e.life.WithLock(t.Context(), func(l LockedLifecycle) error {
		_, err := l.Deploy(t.Context(), base.ID, EditorActor().DeployActor(), false)
		return err
	}))
	agent := e.token()
	saved := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 1"}},
	}})
	res, err := e.a.ApproveDraft(t.Context(), EditorActor(), saved.DraftID, false)
	require.NoError(t, err)
	assert.True(t, res.Deployed, "a running flow is redeployed whatever deploy says")
	rt, ok := e.manager.GetRuntime(base.ID)
	require.True(t, ok)
	assert.True(t, rt.IsRunning())
	dep, err := e.store.GetDeployment(base.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, dep.Version)

	next := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 2, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 2"}},
	}})
	e.life.failDeploy = &LifecycleError{Status: http.StatusConflict, Message: "an exclusive resource is taken"}
	res, err = e.a.ApproveDraft(t.Context(), EditorActor(), next.DraftID, true)
	require.NoError(t, err, "the version is saved all the same")
	assert.Equal(t, 3, res.Version)
	assert.False(t, res.Deployed)
	require.NotNil(t, res.DeployError)
	assert.Equal(t, http.StatusConflict, res.DeployError.Status)
	assert.Equal(t, "an exclusive resource is taken", res.DeployError.Message)
	d, err := e.store.GetFlowDraft(next.DraftID)
	require.NoError(t, err)
	assert.Equal(t, storage.DraftApproved, d.Status)
}

// Approval re-lints the proposal against the current registry (H2): a
// blocking error refuses it and the draft stays pending; an undefined
// secret refuses it only when it would be deployed.
func TestApproveRelintsTheProposal(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	bad := safeFlow()
	addNode(bad, "mystery", "no-such-type", nil)
	data, err := json.Marshal(bad)
	require.NoError(t, err)
	d, err := e.store.CreateFlowDraft(storage.FlowDraft{Data: data, AuthorTokenID: agent.TokenID, AuthorTokenName: agent.TokenName})
	require.NoError(t, err)
	_, err = e.a.ApproveDraft(t.Context(), EditorActor(), d.ID, false)
	te := requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, issueCodes(te.FlowIssues), flow.IssueUnknownType)
	got, err := e.store.GetFlowDraft(d.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.DraftPending, got.Status)

	strict := safeFlow()
	addNode(strict, "tg", "action-telegram", map[string]any{"botToken": "{{secret.bot}}", "chatId": "1"})
	connect(strict, "calc", "tg")
	saved := e.draft(agent, DraftInput{Flow: strict, Name: "telegram"})
	_, err = e.a.ApproveDraft(t.Context(), EditorActor(), saved.DraftID, true)
	te = requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, issueCodes(te.FlowIssues), flow.IssueUndefinedSecret)
	res, err := e.a.ApproveDraft(t.Context(), EditorActor(), saved.DraftID, false)
	require.NoError(t, err, "saved without deploying: the admin may define the secret later")
	assert.False(t, res.Deployed)
}

func TestRejectDraft(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	saved := e.draft(agent, DraftInput{Flow: safeFlow(), Name: "x"})
	require.Error(t, e.a.RejectDraft(t.Context(), EditorActor(), saved.DraftID, strings.Repeat("r", MaxRejectReasonChars+1)))
	require.NoError(t, e.a.RejectDraft(t.Context(), EditorActor(), saved.DraftID, "not now"))
	view, err := e.a.GetDraft(t.Context(), agent, saved.DraftID)
	require.NoError(t, err)
	assert.Equal(t, storage.DraftRejected, view.Status)
	assert.Equal(t, "not now", view.Reason)
	requireKind(t, e.a.RejectDraft(t.Context(), EditorActor(), saved.DraftID, ""), ErrConflict)
	requireKind(t, e.a.RejectDraft(t.Context(), EditorActor(), "nope", ""), ErrToolNotFound)
	requireKind(t, e.a.RejectDraft(t.Context(), agent, saved.DraftID, ""), ErrPermissionDenied)
}

// Revoking a token voids its pending drafts: they can no longer be
// approved.
func TestRevokingATokenVoidsItsDrafts(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	saved := e.draft(agent, DraftInput{Flow: safeFlow(), Name: "x"})
	require.NoError(t, e.store.RevokeAPIToken(agent.TokenID, time.Now()))
	list, err := e.a.ListDrafts(t.Context(), EditorActor(), DraftQuery{Status: "decided"})
	require.NoError(t, err)
	require.Len(t, list.Drafts, 1)
	assert.Equal(t, storage.DraftVoid, list.Drafts[0].Status)
	assert.Equal(t, storage.DraftReasonTokenRevoked, list.Drafts[0].Reason)
	_, err = e.a.ApproveDraft(t.Context(), EditorActor(), saved.DraftID, false)
	requireKind(t, err, ErrConflict)
}

// Without the lifecycle path nothing can be applied.
func TestApproveWithoutALifecycleIsUnavailable(t *testing.T) {
	e := newAuthEnv(t, func(o *AuthoringOptions) { o.Lifecycle = nil })
	saved := e.draft(e.token(), DraftInput{Flow: safeFlow(), Name: "x"})
	_, err := e.a.ApproveDraft(t.Context(), EditorActor(), saved.DraftID, false)
	requireKind(t, err, ErrUnavailable)
}
