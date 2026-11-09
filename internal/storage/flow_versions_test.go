package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentMeta is the meta of an agent version that applies no draft.
func agentMeta(note string) VersionMeta {
	return VersionMeta{ActorKind: VersionActorAgent, TokenID: "tok-1", TokenName: "laptop", Note: note}
}

func TestCreateFlowStoresVersionOne(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("v1")
	f.Version = 42 // client-supplied: ignored
	require.NoError(t, s.CreateFlow(f))
	assert.Equal(t, 1, f.Version, "the caller's flow carries the stored version")

	got, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Version)

	versions, err := s.ListFlowVersions(f.ID, 10)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	v := versions[0]
	assert.Equal(t, f.ID, v.FlowID)
	assert.Equal(t, 1, v.Version)
	assert.Equal(t, VersionActorEditor, v.ActorKind)
	assert.True(t, v.AdminApproved)
	assert.Nil(t, v.Flow, "listings carry metadata only")
	assert.False(t, v.CreatedAt.IsZero())

	full, err := s.GetFlowVersion(f.ID, 1)
	require.NoError(t, err)
	require.NotNil(t, full.Flow)
	assert.Equal(t, "v1", full.Flow.Name)
	assert.Equal(t, 1, full.Flow.Version)
	assert.Len(t, full.Flow.Nodes, 1)
}

func TestCreateFlowVersionedRecordsItsMeta(t *testing.T) {
	s, tok := newDraftFixture(t)
	d, err := s.CreateFlowDraft(draftFor(tok.ID, "", 0))
	require.NoError(t, err)
	f := createTestFlow("from a draft")
	meta := VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true, DraftID: d.ID, Note: "approved draft from laptop"}
	require.NoError(t, s.CreateFlowVersioned(f, meta))
	assert.Equal(t, 1, f.Version)
	v, err := s.GetFlowVersion(f.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, d.ID, v.DraftID)
	assert.Equal(t, meta.Note, v.Note)
	meta.DraftID = ""

	require.ErrorIs(t, s.CreateFlowVersioned(createTestFlow("x"), VersionMeta{ActorKind: "robot"}), ErrInvalidFlowData)
	require.ErrorIs(t, s.CreateFlowVersioned(f, meta), ErrFlowAlreadyExists)
	versions, err := s.ListFlowVersions(f.ID, 10)
	require.NoError(t, err)
	assert.Len(t, versions, 1, "a refused creation records nothing")
}

// A flow re-created under the ID of a deleted one starts a fresh history.
func TestRecreatedFlowStartsAtVersionOne(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("again")
	require.NoError(t, s.CreateFlow(f))
	require.NoError(t, s.UpdateFlow(f))
	require.NoError(t, s.DeleteFlow(f.ID))
	require.NoError(t, s.CreateFlow(f))
	versions, err := s.ListFlowVersions(f.ID, 10)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, 1, versions[0].Version)
}

func TestUpdateFlowBumpsVersionAsAnEditorSave(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("before")
	require.NoError(t, s.CreateFlow(f))

	f.Name = "after"
	require.NoError(t, s.UpdateFlow(f))
	assert.Equal(t, 2, f.Version)

	got, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Version)
	assert.Equal(t, "after", got.Name)

	v, err := s.GetFlowVersion(f.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, VersionActorEditor, v.ActorKind)
	assert.True(t, v.AdminApproved)
	assert.Equal(t, "after", v.Flow.Name)
	old, err := s.GetFlowVersion(f.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "before", old.Flow.Name, "earlier versions keep their definition")

	listed, err := s.ListFlows(nil)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, 2, listed[0].Version)
}

func TestUpdateFlowVersionedChecksTheExpectedVersion(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))

	f.Name = "agent edit"
	v, err := s.UpdateFlowVersioned(f, 1, agentMeta("rename"))
	require.NoError(t, err)
	assert.Equal(t, 2, v)
	assert.Equal(t, 2, f.Version)

	meta, err := s.GetFlowVersion(f.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, VersionActorAgent, meta.ActorKind)
	assert.Equal(t, "tok-1", meta.TokenID)
	assert.Equal(t, "laptop", meta.TokenName)
	assert.False(t, meta.AdminApproved)
	assert.Equal(t, "rename", meta.Note)
	assert.Empty(t, meta.DraftID)

	f.Name = "stale"
	_, err = s.UpdateFlowVersioned(f, 1, VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true})
	require.ErrorIs(t, err, ErrVersionConflict)
	var conflict *VersionConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, 2, conflict.Current)
	got, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, "agent edit", got.Name, "a stale save changes nothing")
	assert.Equal(t, 2, got.Version)

	// expected 0: no check.
	v, err = s.UpdateFlowVersioned(f, 0, VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true})
	require.NoError(t, err)
	assert.Equal(t, 3, v)
}

func TestUpdateFlowVersionedRejectsBadInput(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("base")
	require.NoError(t, s.CreateFlow(f))

	_, err := s.UpdateFlowVersioned(f, 0, VersionMeta{ActorKind: "robot"})
	require.ErrorIs(t, err, ErrInvalidFlowData)
	_, err = s.UpdateFlowVersioned(f, 0, VersionMeta{ActorKind: VersionActorAgent, AdminApproved: true})
	require.ErrorIs(t, err, ErrInvalidFlowData, "an agent never approves its own version")

	missing := createTestFlow("missing")
	_, err = s.UpdateFlowVersioned(missing, 0, VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true})
	require.ErrorIs(t, err, ErrFlowNotFound)
	require.ErrorIs(t, s.UpdateFlow(missing), ErrFlowNotFound)

	versions, err := s.ListFlowVersions(f.ID, 10)
	require.NoError(t, err)
	assert.Len(t, versions, 1)
}

func TestStateChangesDoNotBumpTheVersion(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("state")
	require.NoError(t, s.CreateFlow(f))

	require.NoError(t, s.UpdateFlowState(f.ID, flow.FlowStateRunning))
	require.NoError(t, s.UpdateFlowState(f.ID, flow.FlowStateInactive))

	got, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Version)
	versions, err := s.ListFlowVersions(f.ID, 10)
	require.NoError(t, err)
	assert.Len(t, versions, 1)
}

func TestConcurrentVersionedUpdatesExactlyOneWins(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	base := createTestFlow("race")
	require.NoError(t, s.CreateFlow(base))

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := createTestFlow("writer")
			f.ID = base.ID
			_, errs[i] = s.UpdateFlowVersioned(f, 1, agentMeta("race"))
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		assert.ErrorIs(t, err, ErrVersionConflict)
	}
	assert.Equal(t, 1, wins)
	got, err := s.GetFlow(base.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Version)
	versions, err := s.ListFlowVersions(base.ID, 100)
	require.NoError(t, err)
	assert.Len(t, versions, 2)
}

func TestFlowVersionRetentionKeepsTheNewestAdminApproved(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("busy")
	require.NoError(t, s.CreateFlow(f)) // v1, admin-approved

	for range MaxFlowVersions + 10 { // v2..v61, agent
		_, err := s.UpdateFlowVersioned(f, 0, agentMeta("tweak"))
		require.NoError(t, err)
	}
	versions, err := s.ListFlowVersions(f.ID, 1000)
	require.NoError(t, err)
	require.Len(t, versions, MaxFlowVersions+1, "the newest 50 plus the newest admin-approved one")
	assert.Equal(t, MaxFlowVersions+11, versions[0].Version, "newest first")
	assert.Equal(t, 1, versions[len(versions)-1].Version)
	approved, err := s.LastAdminApprovedVersion(f.ID)
	require.NoError(t, err)
	require.NotNil(t, approved)
	assert.Equal(t, 1, approved.Version)
	require.NotNil(t, approved.Flow)
	assert.Equal(t, "busy", approved.Flow.Name)
	_, err = s.GetFlowVersion(f.ID, 2)
	require.ErrorIs(t, err, ErrFlowVersionNotFound, "old agent versions are pruned")

	// An editor save becomes the newest admin-approved version: v1 goes.
	require.NoError(t, s.UpdateFlow(f))
	versions, err = s.ListFlowVersions(f.ID, 1000)
	require.NoError(t, err)
	assert.Len(t, versions, MaxFlowVersions)
	approved, err = s.LastAdminApprovedVersion(f.ID)
	require.NoError(t, err)
	assert.Equal(t, MaxFlowVersions+12, approved.Version)

	limited, err := s.ListFlowVersions(f.ID, 3)
	require.NoError(t, err)
	assert.Len(t, limited, 3)
}

func TestLastAdminApprovedVersionIsNilWhenNone(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	got, err := s.LastAdminApprovedVersion("unknown")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetFlowVersionNotFound(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("one")
	require.NoError(t, s.CreateFlow(f))
	_, err := s.GetFlowVersion(f.ID, 7)
	require.ErrorIs(t, err, ErrFlowVersionNotFound)
	_, err = s.GetFlowVersion("unknown", 1)
	require.ErrorIs(t, err, ErrFlowVersionNotFound)
}

// A database created before flow versions existed gains the version column,
// and every existing flow gets its current definition as version 1
// (editor, admin-approved). Reopening does not seed again.
func TestFlowVersionMigrationSeedsVersionOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wave3.db")
	old, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = old.Exec(`
		CREATE TABLE flows (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			data TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'inactive',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		INSERT INTO flows (id, name, description, data, state, created_at, updated_at)
		VALUES ('legacy', 'Legacy', '', '{"id":"legacy","name":"Legacy","nodes":[],"connections":[],"state":"running"}', 'running', '2026-01-01 00:00:00', '2026-01-01 00:00:00');
	`)
	require.NoError(t, err)
	require.NoError(t, old.Close())

	for i := range 2 { // the second open runs the migration again
		store, err := NewSQLiteStore(path)
		require.NoError(t, err, "open %d", i)
		got, err := store.GetFlow("legacy")
		require.NoError(t, err)
		assert.Equal(t, 1, got.Version)
		assert.Equal(t, flow.FlowStateRunning, got.State)
		versions, err := store.ListFlowVersions("legacy", 10)
		require.NoError(t, err)
		require.Len(t, versions, 1, "open %d", i)
		assert.Equal(t, VersionActorEditor, versions[0].ActorKind)
		assert.True(t, versions[0].AdminApproved)
		approved, err := store.LastAdminApprovedVersion("legacy")
		require.NoError(t, err)
		require.NotNil(t, approved)
		assert.Equal(t, "Legacy", approved.Flow.Name)
		require.NoError(t, store.Close())
	}
}

func TestRecordDeployment(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("deployed")
	require.NoError(t, s.CreateFlow(f))

	got, err := s.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "never deployed")

	at := time.Unix(1_800_000_000, 0).UTC()
	require.NoError(t, s.RecordDeployment(f.ID, Deployment{Version: 1, ActorKind: VersionActorAgent, TokenID: "tok-1", TokenName: "laptop", DeployedAt: at}))
	got, err = s.GetDeployment(f.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, Deployment{Version: 1, ActorKind: VersionActorAgent, TokenID: "tok-1", TokenName: "laptop", DeployedAt: at}, *got)

	require.NoError(t, s.RecordDeployment(f.ID, Deployment{Version: 1, ActorKind: VersionActorEditor, DeployedAt: at.Add(time.Minute)}))
	got, err = s.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Equal(t, VersionActorEditor, got.ActorKind, "the latest deployment replaces the marker")
	assert.Empty(t, got.TokenID)

	require.ErrorIs(t, s.RecordDeployment("unknown", Deployment{Version: 1, ActorKind: VersionActorEditor}), ErrFlowNotFound)
	err = s.RecordDeployment(f.ID, Deployment{Version: 1, ActorKind: "robot"})
	require.True(t, errors.Is(err, ErrInvalidFlowData), "unknown actor kinds are refused: %v", err)
}

func TestDeleteFlowRemovesVersionsAndDeployment(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("gone")
	require.NoError(t, s.CreateFlow(f))
	require.NoError(t, s.UpdateFlow(f))
	require.NoError(t, s.RecordDeployment(f.ID, Deployment{Version: 2, ActorKind: VersionActorEditor}))

	require.NoError(t, s.DeleteFlow(f.ID))

	for _, table := range []string{"flow_versions", "flow_deployments"} {
		var n int
		require.NoError(t, s.DB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE flow_id = ?", f.ID).Scan(&n))
		assert.Zerof(t, n, "%s rows of a deleted flow must not linger", table)
	}
}

func runningIDs(t *testing.T, s *SQLiteStore) []string {
	t.Helper()
	flows, err := s.GetRunningFlows()
	require.NoError(t, err)
	ids := []string{}
	for _, f := range flows {
		ids = append(ids, f.ID)
	}
	return ids
}

// A definition save is not a state change: it neither sets the restart
// intent (a draft or a stored version saying "running" would otherwise
// start the flow at the next boot without any deploy) nor drops it.
func TestVersionedSavesKeepTheStoredState(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("stopped")
	require.NoError(t, s.CreateFlow(f))

	g, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	g.State = flow.FlowStateRunning
	_, err = s.UpdateFlowVersioned(g, 0, VersionMeta{ActorKind: VersionActorAgent, TokenID: "tok-1"})
	require.NoError(t, err)
	assert.Empty(t, runningIDs(t, s), "saving a definition never schedules it for startup")
	got, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateInactive, got.State, "nor in the stored definition")
	assert.Equal(t, flow.FlowStateInactive, g.State, "the caller's flow carries the stored state")

	// The reverse: a running flow keeps its restart intent across a save.
	require.NoError(t, s.UpdateFlowState(f.ID, flow.FlowStateRunning))
	g.State = flow.FlowStateInactive
	require.NoError(t, s.UpdateFlow(g))
	assert.Equal(t, []string{f.ID}, runningIDs(t, s))
	got, err = s.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, got.State)
}

// Versions are definitions: they carry no run state, so nothing loaded from
// one (a rollback) can carry restart intent either.
func TestFlowVersionsCarryNoState(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("v")
	f.State = flow.FlowStateRunning // CreateFlow keeps the caller's state
	require.NoError(t, s.CreateFlow(f))
	require.NoError(t, s.UpdateFlow(f))
	for _, v := range []int{1, 2} {
		got, err := s.GetFlowVersion(f.ID, v)
		require.NoError(t, err)
		assert.Empty(t, got.Flow.State, "version %d", v)
		var raw sql.NullString
		require.NoError(t, s.DB().QueryRow(`SELECT json_extract(data, '$.state') FROM flow_versions WHERE flow_id = ? AND version = ?`, f.ID, v).Scan(&raw))
		assert.False(t, raw.Valid, "version %d stores no state", v)
	}
	approved, err := s.LastAdminApprovedVersion(f.ID)
	require.NoError(t, err)
	assert.Empty(t, approved.Flow.State)
}

// CreateFlowVersioned (a definition from a draft) always creates the flow
// stopped.
func TestCreateFlowVersionedStoresTheFlowInactive(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("from a draft")
	f.State = flow.FlowStateRunning
	require.NoError(t, s.CreateFlowVersioned(f, VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true}))
	assert.Equal(t, flow.FlowStateInactive, f.State)
	assert.Empty(t, runningIDs(t, s))
	got, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateInactive, got.State)
}

// The creation time is the stored one and the update time is the save's,
// whatever the caller's flow says (a draft or a version carries neither).
func TestVersionedSavesOwnTheTimestamps(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	f := createTestFlow("times")
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	f.CreatedAt, f.UpdatedAt = created, created
	require.NoError(t, s.CreateFlow(f))

	g, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	g.CreatedAt, g.UpdatedAt = time.Time{}, time.Time{}
	before := time.Now().Add(-time.Second)
	_, err = s.UpdateFlowVersioned(g, 0, VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true})
	require.NoError(t, err)
	got, err := s.GetFlow(f.ID)
	require.NoError(t, err)
	assert.True(t, got.CreatedAt.Equal(created), "created at %v", got.CreatedAt)
	assert.True(t, got.UpdatedAt.After(before), "updated at %v", got.UpdatedAt)
	assert.True(t, g.CreatedAt.Equal(created))

	h := createTestFlow("no times")
	h.CreatedAt, h.UpdatedAt = time.Time{}, time.Time{}
	require.NoError(t, s.CreateFlowVersioned(h, VersionMeta{ActorKind: VersionActorEditor, AdminApproved: true}))
	got, err = s.GetFlow(h.ID)
	require.NoError(t, err)
	assert.True(t, got.CreatedAt.After(before))
	assert.True(t, got.UpdatedAt.After(before))
}
