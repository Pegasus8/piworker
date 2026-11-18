package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func draftFor(tokenID, flowID string, base int) FlowDraft {
	return FlowDraft{
		FlowID:          flowID,
		BaseVersion:     base,
		Data:            json.RawMessage(`{"id":"` + flowID + `","name":"Proposed","nodes":[],"connections":[]}`),
		Note:            "rename the flow",
		AuthorTokenID:   tokenID,
		AuthorTokenName: "laptop",
		Risk:            "safe",
		IssuesJSON:      json.RawMessage(`[{"path":"/name","code":"x","severity":"warning","message":"m"}]`),
		AlertsJSON:      json.RawMessage(`[]`),
	}
}

func newDraftFixture(t *testing.T) (*SQLiteStore, *APIToken) {
	t.Helper()
	s := createTestStore(t)
	t.Cleanup(func() { s.Close() })
	tok, _ := newTestToken(t, s, NewAPIToken{Scopes: []string{ScopeAuthor}})
	return s, tok
}

func TestCreateAndGetFlowDraft(t *testing.T) {
	s, tok := newDraftFixture(t)
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))

	d, err := s.CreateFlowDraft(draftFor(tok.ID, f.ID, 1))
	require.NoError(t, err)
	require.NotEmpty(t, d.ID)
	assert.Equal(t, DraftPending, d.Status)
	assert.False(t, d.CreatedAt.IsZero())

	got, err := s.GetFlowDraft(d.ID)
	require.NoError(t, err)
	assert.Equal(t, f.ID, got.FlowID)
	assert.Equal(t, 1, got.BaseVersion)
	assert.JSONEq(t, string(draftFor(tok.ID, f.ID, 1).Data), string(got.Data))
	assert.Equal(t, "Proposed", got.Name, "the proposed flow's name is read from its data")
	assert.Equal(t, "rename the flow", got.Note)
	assert.Equal(t, tok.ID, got.AuthorTokenID)
	assert.Equal(t, "laptop", got.AuthorTokenName)
	assert.Equal(t, DraftPending, got.Status)
	assert.Equal(t, "safe", got.Risk)
	assert.JSONEq(t, `[{"path":"/name","code":"x","severity":"warning","message":"m"}]`, string(got.IssuesJSON))
	assert.JSONEq(t, `[]`, string(got.AlertsJSON))
	assert.Nil(t, got.DecidedAt)
	assert.Zero(t, got.AppliedVersion)

	// A new flow: no flow ID, base version 0; empty issue and alert lists.
	nd := draftFor(tok.ID, "", 0)
	nd.IssuesJSON, nd.AlertsJSON = nil, nil
	created, err := s.CreateFlowDraft(nd)
	require.NoError(t, err)
	got, err = s.GetFlowDraft(created.ID)
	require.NoError(t, err)
	assert.Empty(t, got.FlowID)
	assert.JSONEq(t, `[]`, string(got.IssuesJSON))
	assert.JSONEq(t, `[]`, string(got.AlertsJSON))

	_, err = s.GetFlowDraft("missing")
	require.ErrorIs(t, err, ErrFlowDraftNotFound)
}

// A draft keeps the list of the base values its author did not write (the
// service's: which hidden values to keep hidden from it).
func TestFlowDraftKeepsItsKeptList(t *testing.T) {
	s, tok := newDraftFixture(t)
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))
	d := draftFor(tok.ID, f.ID, 1)
	d.KeptJSON = json.RawMessage(`[["calc","template"]]`)
	created, err := s.CreateFlowDraft(d)
	require.NoError(t, err)
	got, err := s.GetFlowDraft(created.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `[["calc","template"]]`, string(got.KeptJSON))

	created, err = s.CreateFlowDraft(draftFor(tok.ID, f.ID, 1))
	require.NoError(t, err)
	got, err = s.GetFlowDraft(created.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(got.KeptJSON), "none")

	bad := draftFor(tok.ID, f.ID, 1)
	bad.KeptJSON = json.RawMessage(`nope`)
	_, err = s.CreateFlowDraft(bad)
	require.ErrorIs(t, err, ErrInvalidFlowDraft)
}

func TestCreateFlowDraftRejectsBadInput(t *testing.T) {
	s, tok := newDraftFixture(t)
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))

	cases := map[string]func(*FlowDraft){
		"no token":                      func(d *FlowDraft) { d.AuthorTokenID = "" },
		"data not JSON":                 func(d *FlowDraft) { d.Data = json.RawMessage(`{"nodes":`) },
		"data not an object":            func(d *FlowDraft) { d.Data = json.RawMessage(`[1]`) },
		"no data":                       func(d *FlowDraft) { d.Data = nil },
		"issues not JSON":               func(d *FlowDraft) { d.IssuesJSON = json.RawMessage(`nope`) },
		"alerts not JSON":               func(d *FlowDraft) { d.AlertsJSON = json.RawMessage(`{`) },
		"unknown risk":                  func(d *FlowDraft) { d.Risk = "spicy" },
		"existing flow without a base":  func(d *FlowDraft) { d.BaseVersion = 0 },
		"new flow with a base version":  func(d *FlowDraft) { d.FlowID = ""; d.BaseVersion = 3 },
		"note too long":                 func(d *FlowDraft) { d.Note = strings.Repeat("n", MaxDraftNoteBytes+1) },
		"data too large":                func(d *FlowDraft) { d.Data = json.RawMessage(`{"x":"` + strings.Repeat("a", MaxDraftDataBytes) + `"}`) },
		"a status other than pending":   func(d *FlowDraft) { d.Status = DraftApproved },
		"a decided draft is not stored": func(d *FlowDraft) { d.AppliedVersion = 2 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := draftFor(tok.ID, f.ID, 1)
			mutate(&d)
			_, err := s.CreateFlowDraft(d)
			require.ErrorIs(t, err, ErrInvalidFlowDraft)
		})
	}

	_, err := s.CreateFlowDraft(draftFor(tok.ID, "unknown-flow", 1))
	require.ErrorIs(t, err, ErrFlowNotFound)

	require.NoError(t, s.UpdateFlow(f)) // now version 2
	_, err = s.CreateFlowDraft(draftFor(tok.ID, f.ID, 1))
	require.ErrorIs(t, err, ErrVersionConflict, "a draft is based on the current version")

	n, err := s.CountPendingFlowDrafts()
	require.NoError(t, err)
	assert.Zero(t, n)
}

// A token revoked between authentication and the insert cannot leave a
// pending draft behind: the check runs in the insert's transaction, which
// revocations (that void pending drafts) serialize with. Expiry is left to
// authentication.
func TestCreateFlowDraftNeedsAnUnrevokedToken(t *testing.T) {
	s, tok := newDraftFixture(t)
	_, err := s.CreateFlowDraft(draftFor("no-such-token", "", 0))
	require.ErrorIs(t, err, ErrDraftTokenRevoked)

	require.NoError(t, s.RevokeAPIToken(tok.ID, time.Now()))
	_, err = s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.ErrorIs(t, err, ErrDraftTokenRevoked)
}

func TestPendingDraftsPerTokenAreCapped(t *testing.T) {
	s, tok := newDraftFixture(t)
	other, _ := newTestToken(t, s, NewAPIToken{Name: "desktop", Scopes: []string{ScopeAuthor}})

	var first *FlowDraft
	for i := range MaxPendingDraftsPerToken {
		d, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
		require.NoError(t, err, "draft %d", i)
		if first == nil {
			first = d
		}
	}
	_, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.ErrorIs(t, err, ErrDraftLimit)

	_, err = s.CreateFlowDraft(draftFor(other.ID, "", 0))
	require.NoError(t, err, "the cap is per token")

	require.NoError(t, s.DecideFlowDraft(first.ID, DraftRejected, "no", 0))
	_, err = s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err, "a decided draft frees its slot")
}

func TestListFlowDrafts(t *testing.T) {
	s, tok := newDraftFixture(t)
	other, _ := newTestToken(t, s, NewAPIToken{Name: "desktop", Scopes: []string{ScopeAuthor}})
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))

	var ids []string // creation order
	for i := range 5 {
		d := draftFor(tok.ID, "", 0)
		if i%2 == 1 {
			d = draftFor(other.ID, f.ID, 1)
		}
		created, err := s.CreateFlowDraft(d)
		require.NoError(t, err)
		ids = append(ids, created.ID)
	}
	require.NoError(t, s.DecideFlowDraft(ids[0], DraftRejected, "", 0))
	require.NoError(t, s.DecideFlowDraft(ids[1], DraftApproved, "", 2))

	idsOf := func(ds []FlowDraft) []string {
		out := []string{}
		for _, d := range ds {
			out = append(out, d.ID)
		}
		return out
	}
	all, err := s.ListFlowDrafts(FlowDraftFilter{}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []string{ids[4], ids[3], ids[2], ids[1], ids[0]}, idsOf(all), "newest first")
	explicit, err := s.ListFlowDrafts(FlowDraftFilter{Status: DraftFilterAll}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, idsOf(all), idsOf(explicit))
	for _, d := range all {
		assert.Nil(t, d.Data, "listings carry no flow data")
		assert.Equal(t, "Proposed", d.Name)
	}

	pending, err := s.ListFlowDrafts(FlowDraftFilter{Status: DraftPending}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []string{ids[4], ids[3], ids[2]}, idsOf(pending))
	decided, err := s.ListFlowDrafts(FlowDraftFilter{Status: DraftFilterDecided}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []string{ids[1], ids[0]}, idsOf(decided))
	assert.Equal(t, DraftApproved, decided[0].Status)
	assert.Equal(t, 2, decided[0].AppliedVersion)
	require.NotNil(t, decided[0].DecidedAt)

	mine, err := s.ListFlowDrafts(FlowDraftFilter{TokenID: other.ID}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []string{ids[3], ids[1]}, idsOf(mine))
	ofFlow, err := s.ListFlowDrafts(FlowDraftFilter{FlowID: f.ID, Status: DraftPending}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []string{ids[3]}, idsOf(ofFlow))

	page, err := s.ListFlowDrafts(FlowDraftFilter{}, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []string{ids[4], ids[3]}, idsOf(page))
	page, err = s.ListFlowDrafts(FlowDraftFilter{}, 2, ids[3])
	require.NoError(t, err)
	assert.Equal(t, []string{ids[2], ids[1]}, idsOf(page))
	page, err = s.ListFlowDrafts(FlowDraftFilter{}, 2, ids[0])
	require.NoError(t, err)
	assert.Empty(t, page)

	_, err = s.ListFlowDrafts(FlowDraftFilter{Status: "maybe"}, 0, "")
	require.ErrorIs(t, err, ErrInvalidFlowDraft)
}

func TestDecideFlowDraft(t *testing.T) {
	s, tok := newDraftFixture(t)
	d, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)

	require.ErrorIs(t, s.DecideFlowDraft(d.ID, "maybe", "", 0), ErrInvalidFlowDraft)
	require.ErrorIs(t, s.DecideFlowDraft(d.ID, DraftPending, "", 0), ErrInvalidFlowDraft)
	require.ErrorIs(t, s.DecideFlowDraft(d.ID, DraftApproved, "", 0), ErrInvalidFlowDraft, "an approval records the applied version")
	require.ErrorIs(t, s.DecideFlowDraft(d.ID, DraftRejected, "", 3), ErrInvalidFlowDraft, "only approvals apply a version")
	require.ErrorIs(t, s.DecideFlowDraft(d.ID, DraftRejected, strings.Repeat("r", MaxDraftReasonBytes+1), 0), ErrInvalidFlowDraft)
	require.ErrorIs(t, s.DecideFlowDraft("missing", DraftRejected, "", 0), ErrFlowDraftNotFound)

	n, err := s.CountPendingFlowDrafts()
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	require.NoError(t, s.DecideFlowDraft(d.ID, DraftApproved, "looks good", 1))
	got, err := s.GetFlowDraft(d.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftApproved, got.Status)
	assert.Equal(t, "looks good", got.Reason)
	assert.Equal(t, 1, got.AppliedVersion)
	require.NotNil(t, got.DecidedAt)

	require.ErrorIs(t, s.DecideFlowDraft(d.ID, DraftRejected, "", 0), ErrDraftNotPending)
	n, err = s.CountPendingFlowDrafts()
	require.NoError(t, err)
	assert.Zero(t, n)
}

func pendingDraftsOf(t *testing.T, s *SQLiteStore, tokenID string) []FlowDraft {
	t.Helper()
	ds, err := s.ListFlowDrafts(FlowDraftFilter{Status: DraftPending, TokenID: tokenID}, 0, "")
	require.NoError(t, err)
	return ds
}

// Every path that revokes tokens voids their pending drafts in the same
// transaction (single revoke, revoke-all, recovery, password change).
func TestRevokingATokenVoidsItsPendingDrafts(t *testing.T) {
	s, tok := newDraftFixture(t)
	other, _ := newTestToken(t, s, NewAPIToken{Name: "desktop", Scopes: []string{ScopeAuthor}})
	decided, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	require.NoError(t, s.DecideFlowDraft(decided.ID, DraftRejected, "no", 0))
	pending, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	kept, err := s.CreateFlowDraft(draftFor(other.ID, "", 0))
	require.NoError(t, err)

	require.NoError(t, s.RevokeAPIToken(tok.ID, testNow))

	got, err := s.GetFlowDraft(pending.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftVoid, got.Status)
	assert.Equal(t, DraftReasonTokenRevoked, got.Reason)
	require.NotNil(t, got.DecidedAt)
	got, err = s.GetFlowDraft(decided.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftRejected, got.Status, "decided drafts keep their decision")
	got, err = s.GetFlowDraft(kept.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftPending, got.Status, "other tokens' drafts are untouched")
}

func TestRevokingAllTokensVoidsEveryPendingDraft(t *testing.T) {
	s, tok := newDraftFixture(t)
	other, _ := newTestToken(t, s, NewAPIToken{Name: "desktop", Scopes: []string{ScopeAuthor}})
	for _, id := range []string{tok.ID, other.ID} {
		_, err := s.CreateFlowDraft(draftFor(id, "", 0))
		require.NoError(t, err)
	}
	_, err := s.RevokeAllAPITokens(testNow)
	require.NoError(t, err)
	n, err := s.CountPendingFlowDrafts()
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestCredentialResetVoidsDraftsAtomically(t *testing.T) {
	s, users := credentialStores(t)
	tok, _ := newTestToken(t, s, NewAPIToken{Scopes: []string{ScopeAuthor}})
	d, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)

	boom := errors.New("disk full")
	err = users.ChangePassword("admin", "old-hash", "new-hash", revokeAll(testNow), func(*sql.Tx) error { return boom })
	require.ErrorIs(t, err, boom)
	assert.Len(t, pendingDraftsOf(t, s, tok.ID), 1, "a failed password change voids nothing")

	require.NoError(t, users.ChangePassword("admin", "old-hash", "new-hash", revokeAll(testNow)))
	got, err := s.GetFlowDraft(d.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftVoid, got.Status)

	// Recovery-code redemption goes through the same hook.
	tok2, _ := newTestToken(t, s, NewAPIToken{Name: "second", Scopes: []string{ScopeAuthor}})
	d2, err := s.CreateFlowDraft(draftFor(tok2.ID, "", 0))
	require.NoError(t, err)
	code, err := users.NewAuthCode("recovery", "admin", testNow)
	require.NoError(t, err)
	require.NoError(t, users.RedeemAuthCode("recovery", code, "admin", "third-hash", testNow, revokeAll(testNow)))
	got, err = s.GetFlowDraft(d2.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftVoid, got.Status)
}

func TestDeletingAFlowVoidsItsPendingDrafts(t *testing.T) {
	s, tok := newDraftFixture(t)
	gone := createTestFlow("gone")
	kept := createTestFlow("kept")
	require.NoError(t, s.CreateFlow(gone))
	require.NoError(t, s.CreateFlow(kept))
	dg, err := s.CreateFlowDraft(draftFor(tok.ID, gone.ID, 1))
	require.NoError(t, err)
	dk, err := s.CreateFlowDraft(draftFor(tok.ID, kept.ID, 1))
	require.NoError(t, err)

	require.NoError(t, s.DeleteFlow(gone.ID))

	got, err := s.GetFlowDraft(dg.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftVoid, got.Status)
	assert.Equal(t, DraftReasonFlowDeleted, got.Reason)
	got, err = s.GetFlowDraft(dk.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftPending, got.Status)
}

// Decided drafts are kept for DraftRetention; creating a draft prunes older
// ones, so agent traffic alone keeps the table bounded.
func TestDecidedDraftsArePruned(t *testing.T) {
	s, tok := newDraftFixture(t)
	old, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	require.NoError(t, s.DecideFlowDraft(old.ID, DraftRejected, "", 0))
	_, err = s.DB().Exec(`UPDATE flow_drafts SET decided_at = ? WHERE id = ?`, time.Now().Add(-DraftRetention-time.Hour).Unix(), old.ID)
	require.NoError(t, err)
	stillPending, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	_, err = s.DB().Exec(`UPDATE flow_drafts SET created_at = ? WHERE id = ?`, time.Now().Add(-DraftRetention-time.Hour).Unix(), stillPending.ID)
	require.NoError(t, err)

	_, err = s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	_, err = s.GetFlowDraft(old.ID)
	require.ErrorIs(t, err, ErrFlowDraftNotFound)
	_, err = s.GetFlowDraft(stillPending.ID)
	require.NoError(t, err, "pending drafts wait for the admin however old they are")
}

func TestFlowDraftJSONIsCamelCase(t *testing.T) {
	s, tok := newDraftFixture(t)
	d, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	got, err := s.GetFlowDraft(d.ID)
	require.NoError(t, err)
	data, err := json.Marshal(got)
	require.NoError(t, err)
	for _, key := range []string{"id", "flowId", "baseVersion", "data", "note", "authorTokenId", "authorTokenName", "status", "risk", "issues", "alerts", "createdAt"} {
		assert.Contains(t, string(data), fmt.Sprintf("%q:", key))
	}
}

// A draft is a definition: run state, version and timestamps are the
// server's and are not stored with it.
func TestCreateFlowDraftDropsServerOwnedFields(t *testing.T) {
	s, tok := newDraftFixture(t)
	d := draftFor(tok.ID, "", 0)
	d.Data = json.RawMessage(`{"name":"P","state":"running","version":7,"createdAt":"2020-01-01T00:00:00Z","updatedAt":"2020-01-01T00:00:00Z","nodes":[],"connections":[]}`)
	created, err := s.CreateFlowDraft(d)
	require.NoError(t, err)
	want := `{"name":"P","nodes":[],"connections":[]}`
	assert.JSONEq(t, want, string(created.Data))
	got, err := s.GetFlowDraft(created.ID)
	require.NoError(t, err)
	assert.JSONEq(t, want, string(got.Data))
}

// draftState reads a draft's status and applied version.
func draftState(t *testing.T, s *SQLiteStore, id string) (string, int) {
	t.Helper()
	d, err := s.GetFlowDraft(id)
	require.NoError(t, err)
	return d.Status, d.AppliedVersion
}

// flowVersion reads a flow's stored version.
func flowVersion(t *testing.T, s *SQLiteStore, id string) int {
	t.Helper()
	f, err := s.GetFlow(id)
	require.NoError(t, err)
	return f.Version
}

// A save that applies a draft closes it as approved in its own transaction:
// a draft voided before (a revoked token) can never become a version, and a
// draft that became a version is never left pending.
func TestApplyingADraftClosesItInTheSaveTransaction(t *testing.T) {
	s, tok := newDraftFixture(t)
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))
	d, err := s.CreateFlowDraft(draftFor(tok.ID, f.ID, 1))
	require.NoError(t, err)

	meta := VersionMeta{ActorKind: VersionActorAgent, TokenID: tok.ID, TokenName: "laptop", DraftID: d.ID}
	v, err := s.UpdateFlowVersioned(f, 1, meta)
	require.NoError(t, err)
	assert.Equal(t, 2, v)
	status, applied := draftState(t, s, d.ID)
	assert.Equal(t, DraftApproved, status)
	assert.Equal(t, 2, applied)
	got, err := s.GetFlowDraft(d.ID)
	require.NoError(t, err)
	require.NotNil(t, got.DecidedAt)
	version, err := s.GetFlowVersion(f.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, d.ID, version.DraftID)
	assert.Equal(t, tok.ID, version.TokenID)

	// Recording the same approval again is a no-op (callers that close the
	// draft themselves keep working); anything else is not.
	require.NoError(t, s.DecideFlowDraft(d.ID, DraftApproved, "", 2))
	require.ErrorIs(t, s.DecideFlowDraft(d.ID, DraftApproved, "", 3), ErrDraftNotPending)
	require.ErrorIs(t, s.DecideFlowDraft(d.ID, DraftRejected, "", 0), ErrDraftNotPending)

	// Applying it twice is refused, and changes nothing.
	_, err = s.UpdateFlowVersioned(f, 0, VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true, DraftID: d.ID})
	require.ErrorIs(t, err, ErrDraftNotPending)
	assert.Equal(t, 2, flowVersion(t, s, f.ID))
}

func TestApplyingADraftChecksIt(t *testing.T) {
	s, tok := newDraftFixture(t)
	other, _ := newTestToken(t, s, NewAPIToken{Name: "desktop", Scopes: []string{ScopeAuthor}})
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))
	g := createTestFlow("other")
	require.NoError(t, s.CreateFlow(g))
	editor := func(id string) VersionMeta {
		return VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true, DraftID: id}
	}
	agent := func(tokenID, id string) VersionMeta {
		return VersionMeta{ActorKind: VersionActorAgent, TokenID: tokenID, DraftID: id}
	}
	newDraft := func(tokenID, flowID string, base int) string {
		d, err := s.CreateFlowDraft(draftFor(tokenID, flowID, base))
		require.NoError(t, err)
		return d.ID
	}

	forF := newDraft(tok.ID, f.ID, 1)
	forNew := newDraft(tok.ID, "", 0)
	cases := []struct {
		name string
		save func() error
		want error
	}{
		{"unknown draft", func() error { _, err := s.UpdateFlowVersioned(f, 0, editor("missing")); return err }, ErrFlowDraftNotFound},
		{"a draft of another flow", func() error { _, err := s.UpdateFlowVersioned(g, 0, editor(forF)); return err }, ErrInvalidFlowDraft},
		{"a new-flow draft saved over a flow", func() error { _, err := s.UpdateFlowVersioned(f, 0, editor(forNew)); return err }, ErrInvalidFlowDraft},
		{"another token's draft", func() error { _, err := s.UpdateFlowVersioned(f, 0, agent(other.ID, forF)); return err }, ErrFlowDraftNotFound},
		{"an existing flow's draft created as new", func() error {
			return s.CreateFlowVersioned(createTestFlow("new"), editor(forF))
		}, ErrInvalidFlowDraft},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.save(), tc.want)
			assert.Equal(t, 1, flowVersion(t, s, f.ID))
			assert.Equal(t, 1, flowVersion(t, s, g.ID))
			for _, id := range []string{forF, forNew} {
				status, _ := draftState(t, s, id)
				assert.Equal(t, DraftPending, status)
			}
		})
	}

	// A stale draft is a conflict even when the caller checks no version.
	require.NoError(t, s.UpdateFlow(f)) // v2
	_, err := s.UpdateFlowVersioned(f, 0, editor(forF))
	require.ErrorIs(t, err, ErrVersionConflict)
	var conflict *VersionConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, 2, conflict.Current)
	assert.Equal(t, 2, flowVersion(t, s, f.ID))

	// A new-flow draft creates its flow and is closed with version 1.
	n := createTestFlow("brand new")
	require.NoError(t, s.CreateFlowVersioned(n, agent(tok.ID, forNew)))
	status, applied := draftState(t, s, forNew)
	assert.Equal(t, DraftApproved, status)
	assert.Equal(t, 1, applied)
	// The applied draft names the flow it created, so its author and the
	// editor can find it.
	created, err := s.GetFlowDraft(forNew)
	require.NoError(t, err)
	assert.Equal(t, n.ID, created.FlowID)
	assert.Zero(t, created.BaseVersion, "still a draft of a new flow")

	// An agent applies a draft only while its token is not revoked (a
	// revocation voids the drafts too; this is the belt to those braces).
	late := newDraft(tok.ID, f.ID, 2)
	_, err = s.DB().Exec(`UPDATE api_tokens SET revoked_at = ? WHERE id = ?`, testNow.Unix(), tok.ID)
	require.NoError(t, err)
	_, err = s.UpdateFlowVersioned(f, 0, agent(tok.ID, late))
	require.ErrorIs(t, err, ErrDraftTokenRevoked)
	assert.Equal(t, 2, flowVersion(t, s, f.ID))
}

// Revoking the author's token and applying its draft race: whichever
// commits first wins, and the other sees its effect. A voided draft never
// becomes a version; an applied one is never voided.
func TestRevocationAndDraftApplicationAreAtomic(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	for i := range 20 {
		tok, _ := newTestToken(t, s, NewAPIToken{Name: fmt.Sprintf("t%d", i), Scopes: []string{ScopeAuthor}})
		f := createTestFlow(fmt.Sprintf("f%d", i))
		require.NoError(t, s.CreateFlow(f))
		d, err := s.CreateFlowDraft(draftFor(tok.ID, f.ID, 1))
		require.NoError(t, err)

		var wg sync.WaitGroup
		var saveErr, revokeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, saveErr = s.UpdateFlowVersioned(f, 1, VersionMeta{ActorKind: VersionActorAgent, TokenID: tok.ID, DraftID: d.ID})
		}()
		go func() {
			defer wg.Done()
			revokeErr = s.RevokeAPIToken(tok.ID, testNow)
		}()
		wg.Wait()
		require.NoError(t, revokeErr)

		status, applied := draftState(t, s, d.ID)
		if saveErr == nil {
			assert.Equal(t, DraftApproved, status)
			assert.Equal(t, 2, applied)
			assert.Equal(t, 2, flowVersion(t, s, f.ID))
		} else {
			require.ErrorIs(t, saveErr, ErrDraftNotPending)
			assert.Equal(t, DraftVoid, status)
			assert.Equal(t, 1, flowVersion(t, s, f.ID), "a voided draft never becomes a version")
		}
	}
}

// A draft records who applied it: the admin approving it in the editor, or
// the agent deploying its own draft (agent_deploy low or all), which nobody
// reviewed. Drafts not applied record nobody.
func TestAppliedDraftsRecordWhoAppliedThem(t *testing.T) {
	s, tok := newDraftFixture(t)
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))

	byAgent, err := s.CreateFlowDraft(draftFor(tok.ID, f.ID, 1))
	require.NoError(t, err)
	_, err = s.UpdateFlowVersioned(f, 1, VersionMeta{ActorKind: VersionActorAgent, TokenID: tok.ID, TokenName: "laptop", DraftID: byAgent.ID})
	require.NoError(t, err)
	got, err := s.GetFlowDraft(byAgent.ID)
	require.NoError(t, err)
	assert.Equal(t, DraftApplierAgent, got.AppliedBy)

	byEditor, err := s.CreateFlowDraft(draftFor(tok.ID, f.ID, 2))
	require.NoError(t, err)
	_, err = s.UpdateFlowVersioned(f, 2, VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true, DraftID: byEditor.ID})
	require.NoError(t, err)
	listed, err := s.ListFlowDrafts(FlowDraftFilter{Status: DraftApproved}, 0, "")
	require.NoError(t, err)
	by := map[string]string{}
	for _, d := range listed {
		by[d.ID] = d.AppliedBy
	}
	assert.Equal(t, map[string]string{byAgent.ID: DraftApplierAgent, byEditor.ID: DraftApplierEditor}, by)

	rejected, err := s.CreateFlowDraft(draftFor(tok.ID, f.ID, 3))
	require.NoError(t, err)
	require.NoError(t, s.DecideFlowDraft(rejected.ID, DraftRejected, "no", 0))
	got, err = s.GetFlowDraft(rejected.ID)
	require.NoError(t, err)
	assert.Empty(t, got.AppliedBy)
	data, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "appliedBy")
}

// Decided drafts are bounded by count too, so an agent looping on save and
// deploy (each a decided draft of up to about 2 MiB) cannot fill the disk
// within the retention: each token keeps its newest MaxDecidedDraftsPerToken
// decided drafts and the table its newest MaxDecidedDrafts.
func TestDecidedDraftsAreCappedByCount(t *testing.T) {
	s, tok := newDraftFixture(t)
	decide := func(tokenID string, n int) []string {
		var ids []string
		for range n {
			d, err := s.CreateFlowDraft(draftFor(tokenID, "", 0))
			require.NoError(t, err)
			require.NoError(t, s.DecideFlowDraft(d.ID, DraftRejected, "", 0))
			ids = append(ids, d.ID)
		}
		return ids
	}
	ids := decide(tok.ID, MaxDecidedDraftsPerToken+3)
	pending, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	decided, err := s.ListFlowDrafts(FlowDraftFilter{Status: DraftFilterDecided, TokenID: tok.ID}, MaxDraftListLimit, "")
	require.NoError(t, err)
	assert.Len(t, decided, MaxDecidedDraftsPerToken)
	for _, id := range ids[:3] {
		_, err := s.GetFlowDraft(id)
		require.ErrorIs(t, err, ErrFlowDraftNotFound, "the oldest go first")
	}
	_, err = s.GetFlowDraft(ids[len(ids)-1])
	require.NoError(t, err)
	_, err = s.GetFlowDraft(pending.ID)
	require.NoError(t, err, "pending drafts wait for the admin")

	// The whole table: tokens together keep at most MaxDecidedDrafts.
	tokens := MaxDecidedDrafts/MaxDecidedDraftsPerToken + 1
	for range tokens {
		other, _ := newTestToken(t, s, NewAPIToken{Scopes: []string{ScopeAuthor}})
		decide(other.ID, MaxDecidedDraftsPerToken)
	}
	_, err = s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	all, err := s.ListFlowDrafts(FlowDraftFilter{Status: DraftFilterDecided}, MaxDraftListLimit, "")
	require.NoError(t, err)
	assert.Len(t, all, MaxDecidedDrafts)
}
