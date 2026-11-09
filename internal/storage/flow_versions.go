package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
)

// Flow versions (docs/design/mcp.md §10). Every definition change of a flow
// (an editor save, an approved or agent-applied draft, a rollback) bumps
// flows.version and stores the new definition in flow_versions with who made
// it and whether the admin approved it. State changes (deploy, stop, toggle,
// a failed restore) do not. The newest MaxFlowVersions versions of each flow
// are kept, plus always its newest admin-approved one, which the low
// agent-deploy policy and the one-click rollback compare against.

// Version actors.
const (
	VersionActorEditor = "editor"
	VersionActorAgent  = "agent"
)

const (
	// MaxFlowVersions is how many of the newest versions of a flow are kept
	// (the newest admin-approved one is kept besides them).
	MaxFlowVersions = 50
	// MaxVersionNoteBytes bounds VersionMeta.Note.
	MaxVersionNoteBytes = 8 << 10
	// maxFlowVersionList bounds ListFlowVersions.
	maxFlowVersionList = 1000
)

var (
	// ErrVersionConflict is returned (as a *VersionConflictError) when a
	// versioned update expected another version than the stored one.
	ErrVersionConflict = errors.New("the flow was changed since it was loaded")
	// ErrFlowVersionNotFound is returned for an unknown (or pruned) version.
	ErrFlowVersionNotFound = errors.New("flow version not found")
)

// VersionConflictError reports the version a stale update did not match.
// errors.Is(err, ErrVersionConflict) holds for it.
type VersionConflictError struct {
	Current int // the stored version
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("%s (current version %d)", ErrVersionConflict, e.Current)
}

// Is makes errors.Is(err, ErrVersionConflict) true.
func (e *VersionConflictError) Is(target error) bool { return target == ErrVersionConflict }

// VersionMeta describes who made a version.
type VersionMeta struct {
	ActorKind     string // VersionActorEditor or VersionActorAgent
	TokenID       string // the agent's token (agent versions)
	TokenName     string
	AdminApproved bool // the admin saved or approved this definition; never for agents
	Note          string
	// DraftID is the draft this version applies, if any: the save closes
	// it as approved with the new version in its own transaction, and
	// fails without saving when the draft cannot be applied (applyDraftTx).
	DraftID string
}

func (m VersionMeta) validate() error {
	if err := validActor(m.ActorKind); err != nil {
		return err
	}
	if m.ActorKind == VersionActorAgent && m.AdminApproved {
		return fmt.Errorf("%w: an agent version cannot be admin-approved", ErrInvalidFlowData)
	}
	if len(m.Note) > MaxVersionNoteBytes {
		return fmt.Errorf("%w: version note exceeds %d bytes", ErrInvalidFlowData, MaxVersionNoteBytes)
	}
	return nil
}

func validActor(kind string) error {
	switch kind {
	case VersionActorEditor, VersionActorAgent:
		return nil
	}
	return fmt.Errorf("%w: unknown actor kind %q", ErrInvalidFlowData, kind)
}

// FlowVersion is one stored version of a flow. Flow is set by GetFlowVersion
// and LastAdminApprovedVersion, not by listings; it has no State.
type FlowVersion struct {
	FlowID        string     `json:"flowId"`
	Version       int        `json:"version"`
	ActorKind     string     `json:"actorKind"`
	TokenID       string     `json:"tokenId,omitempty"`
	TokenName     string     `json:"tokenName,omitempty"`
	AdminApproved bool       `json:"adminApproved"`
	Note          string     `json:"note,omitempty"`
	DraftID       string     `json:"draftId,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	Flow          *flow.Flow `json:"flow,omitempty"`
}

// Deployment marks which version of a flow was last deployed, and by whom.
type Deployment struct {
	Version    int       `json:"version"`
	ActorKind  string    `json:"actorKind"`
	TokenID    string    `json:"tokenId,omitempty"`
	TokenName  string    `json:"tokenName,omitempty"`
	DeployedAt time.Time `json:"deployedAt"`
}

const flowVersionsSchema = `
	CREATE TABLE IF NOT EXISTS flow_versions (
		flow_id        TEXT NOT NULL,
		version        INTEGER NOT NULL,
		data           TEXT NOT NULL,
		actor_kind     TEXT NOT NULL,
		token_id       TEXT NOT NULL DEFAULT '',
		token_name     TEXT NOT NULL DEFAULT '',
		admin_approved INTEGER NOT NULL DEFAULT 0,
		note           TEXT NOT NULL DEFAULT '',
		draft_id       TEXT NOT NULL DEFAULT '',
		created_at     INTEGER NOT NULL,
		PRIMARY KEY (flow_id, version)
	);
	CREATE TABLE IF NOT EXISTS flow_deployments (
		flow_id     TEXT PRIMARY KEY,
		version     INTEGER NOT NULL,
		actor_kind  TEXT NOT NULL,
		token_id    TEXT NOT NULL DEFAULT '',
		token_name  TEXT NOT NULL DEFAULT '',
		deployed_at INTEGER NOT NULL
	);
`

// migrateFlowVersions adds flows.version, the version and deployment tables,
// and seeds every flow without a stored version with its current definition
// (editor, admin-approved; without its run state). Every step is idempotent.
func migrateFlowVersions(db *sql.DB) error {
	has, err := historyHasColumn(db, "flows", "version")
	if err != nil {
		return err
	}
	if !has {
		if _, err := db.Exec(`ALTER TABLE flows ADD COLUMN version INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("failed to add flows.version: %w", err)
		}
	}
	if _, err := db.Exec(flowVersionsSchema); err != nil {
		return fmt.Errorf("failed to create flow version tables: %w", err)
	}
	// A row whose blob is not valid JSON (corruption) is copied as is:
	// listings skip it and it must not stop the store from opening.
	_, err = db.Exec(`
		INSERT INTO flow_versions (flow_id, version, data, actor_kind, admin_approved, created_at)
		SELECT f.id, f.version,
			CASE WHEN json_valid(f.data) THEN json_remove(json_set(f.data, '$.version', f.version), '$.state') ELSE f.data END,
			?, 1, ?
		FROM flows f
		WHERE NOT EXISTS (SELECT 1 FROM flow_versions v WHERE v.flow_id = f.id)
	`, VersionActorEditor, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to seed flow versions: %w", err)
	}
	return nil
}

// recordFlowVersion copies the flows row of flowID (just written) into
// flow_versions with meta, without its run state (a version is a
// definition), then prunes old versions, inside tx.
func recordFlowVersion(tx *sql.Tx, flowID string, meta VersionMeta, now time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO flow_versions (flow_id, version, data, actor_kind, token_id, token_name, admin_approved, note, draft_id, created_at)
		SELECT id, version, json_remove(data, '$.state'), ?, ?, ?, ?, ?, ?, ? FROM flows WHERE id = ?
	`, meta.ActorKind, meta.TokenID, meta.TokenName, boolInt(meta.AdminApproved), meta.Note, meta.DraftID, now.Unix(), flowID)
	if err != nil {
		return fmt.Errorf("failed to record flow version: %w", err)
	}
	_, err = tx.Exec(`
		DELETE FROM flow_versions
		WHERE flow_id = ?
		  AND version NOT IN (SELECT version FROM flow_versions WHERE flow_id = ? ORDER BY version DESC LIMIT ?)
		  AND version <> COALESCE((SELECT MAX(version) FROM flow_versions WHERE flow_id = ? AND admin_approved = 1), 0)
	`, flowID, flowID, MaxFlowVersions, flowID)
	if err != nil {
		return fmt.Errorf("failed to prune flow versions: %w", err)
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpdateFlowVersioned saves a new definition of an existing flow as the next
// version, recorded with meta, in one transaction. expected 0 skips the
// check; otherwise a stored version other than expected fails with a
// *VersionConflictError (errors.Is ErrVersionConflict) and changes nothing,
// as does a meta.DraftID that cannot be applied (applyDraftTx: a draft is
// applied and closed in this transaction or not at all). Only the
// definition changes: the stored state (restart intent) and
// creation time are kept and the update time is the save's, whatever f
// says (f may come from a draft or an old version). On success f holds the
// new version, the stored state and both times; the new version is also
// returned. Old versions are pruned in the same transaction.
func (s *SQLiteStore) UpdateFlowVersioned(f *flow.Flow, expected int, meta VersionMeta) (int, error) {
	if s.isClosed() {
		return 0, ErrDatabaseClosed
	}
	if f == nil {
		return 0, ErrInvalidFlowData
	}
	if f.ID == "" {
		return 0, fmt.Errorf("%w: empty flow ID", ErrInvalidFlowData)
	}
	if err := meta.validate(); err != nil {
		return 0, err
	}
	data, err := json.Marshal(f)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal flow: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin flow update: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	var (
		current int
		state   string
		created sql.NullString // the stored definition's createdAt (JSON text)
	)
	err = tx.QueryRow(`
		SELECT version, state, CASE WHEN json_valid(data) THEN json_extract(data, '$.createdAt') END
		FROM flows WHERE id = ?`, f.ID).Scan(&current, &state, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrFlowNotFound
		}
		return 0, fmt.Errorf("failed to read flow version: %w", err)
	}
	if expected != 0 && current != expected {
		return 0, &VersionConflictError{Current: current}
	}
	next := current + 1
	now := time.Now()
	if err := applyDraftTx(tx, meta, f.ID, current, next, "", now); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`
		UPDATE flows
		SET name = ?, description = ?, version = ?, updated_at = ?,
			data = json_set(?, '$.version', ?, '$.state', ?, '$.createdAt', ?, '$.updatedAt', ?)
		WHERE id = ?
	`, f.Name, f.Description, next, now, string(data), next, state, created, jsonTime(now), f.ID); err != nil {
		return 0, fmt.Errorf("failed to update flow: %w", err)
	}
	if err := recordFlowVersion(tx, f.ID, meta, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit flow update: %w", err)
	}
	f.Version, f.State, f.UpdatedAt = next, flow.FlowState(state), now
	if t, err := time.Parse(time.RFC3339Nano, created.String); err == nil && created.Valid {
		f.CreatedAt = t
	}
	return next, nil
}

// jsonTime renders t as encoding/json does.
func jsonTime(t time.Time) string { return t.Format(time.RFC3339Nano) }

const flowVersionColumns = `flow_id, version, actor_kind, token_id, token_name, admin_approved, note, draft_id, created_at`

func scanFlowVersion(row rowScanner, withData bool) (*FlowVersion, error) {
	var (
		v        FlowVersion
		approved int
		created  int64
		data     string
	)
	dest := []any{&v.FlowID, &v.Version, &v.ActorKind, &v.TokenID, &v.TokenName, &approved, &v.Note, &v.DraftID, &created}
	if withData {
		dest = append(dest, &data)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	v.AdminApproved = approved != 0
	v.CreatedAt = time.Unix(created, 0).UTC()
	if withData {
		f, err := flow.FromJSON([]byte(data))
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal flow version %s/%d: %w", v.FlowID, v.Version, err)
		}
		// A version is a definition: no run state (rows stored before
		// versions dropped it may still hold one).
		f.Version, f.State = v.Version, ""
		v.Flow = f
	}
	return &v, nil
}

// GetFlowVersion returns one version of a flow with its definition, or
// ErrFlowVersionNotFound.
func (s *SQLiteStore) GetFlowVersion(flowID string, version int) (*FlowVersion, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	v, err := scanFlowVersion(s.db.QueryRow(`SELECT `+flowVersionColumns+`, data FROM flow_versions WHERE flow_id = ? AND version = ?`, flowID, version), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFlowVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get flow version: %w", err)
	}
	return v, nil
}

// ListFlowVersions returns the stored versions of a flow, newest first,
// without their definitions (at most limit; limit <= 0 lists them all).
func (s *SQLiteStore) ListFlowVersions(flowID string, limit int) ([]FlowVersion, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	if limit <= 0 || limit > maxFlowVersionList {
		limit = maxFlowVersionList
	}
	rows, err := s.db.Query(`SELECT `+flowVersionColumns+` FROM flow_versions WHERE flow_id = ? ORDER BY version DESC LIMIT ?`, flowID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list flow versions: %w", err)
	}
	defer rows.Close()
	out := []FlowVersion{}
	for rows.Next() {
		v, err := scanFlowVersion(rows, false)
		if err != nil {
			return nil, fmt.Errorf("failed to scan flow version: %w", err)
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating flow versions: %w", err)
	}
	return out, nil
}

// LastAdminApprovedVersion returns the newest admin-approved version of a
// flow with its definition, or nil when there is none (unknown flow).
func (s *SQLiteStore) LastAdminApprovedVersion(flowID string) (*FlowVersion, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	v, err := scanFlowVersion(s.db.QueryRow(`
		SELECT `+flowVersionColumns+`, data FROM flow_versions
		WHERE flow_id = ? AND admin_approved = 1 ORDER BY version DESC LIMIT 1`, flowID), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the last admin-approved version: %w", err)
	}
	return v, nil
}

// RecordDeployment replaces the deployment marker of an existing flow
// (ErrFlowNotFound otherwise). A zero DeployedAt means now.
func (s *SQLiteStore) RecordDeployment(flowID string, d Deployment) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if err := validActor(d.ActorKind); err != nil {
		return err
	}
	if d.DeployedAt.IsZero() {
		d.DeployedAt = time.Now()
	}
	res, err := s.db.Exec(`
		INSERT INTO flow_deployments (flow_id, version, actor_kind, token_id, token_name, deployed_at)
		SELECT ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM flows WHERE id = ?)
		ON CONFLICT(flow_id) DO UPDATE SET
			version = excluded.version, actor_kind = excluded.actor_kind, token_id = excluded.token_id,
			token_name = excluded.token_name, deployed_at = excluded.deployed_at
	`, flowID, d.Version, d.ActorKind, d.TokenID, d.TokenName, d.DeployedAt.Unix(), flowID)
	if err != nil {
		return fmt.Errorf("failed to record deployment: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrFlowNotFound
	}
	return nil
}

// GetDeployment returns the deployment marker of a flow, or nil when it was
// never recorded.
func (s *SQLiteStore) GetDeployment(flowID string) (*Deployment, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	var (
		d  Deployment
		at int64
	)
	err := s.db.QueryRow(`SELECT version, actor_kind, token_id, token_name, deployed_at FROM flow_deployments WHERE flow_id = ?`, flowID).
		Scan(&d.Version, &d.ActorKind, &d.TokenID, &d.TokenName, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}
	d.DeployedAt = time.Unix(at, 0).UTC()
	return &d, nil
}
