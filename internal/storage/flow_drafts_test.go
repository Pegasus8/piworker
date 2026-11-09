package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
