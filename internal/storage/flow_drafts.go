package storage

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Flow drafts (docs/design/mcp.md §10): agent-authored proposals of a whole
// flow definition (new, or based on a version of an existing flow) that wait
// for the admin. Approving one saves it as a new version; a draft never
// changes what runs by itself. Revoking the author's token, or deleting the
// flow, voids its pending drafts.

// Draft statuses.
const (
	DraftPending  = "pending"
	DraftApproved = "approved"
	DraftRejected = "rejected"
	DraftVoid     = "void"
	// DraftFilterDecided lists every draft that is no longer pending.
	DraftFilterDecided = "decided"
	// DraftFilterAll lists every draft (as does "").
	DraftFilterAll = "all"
)

// Reasons recorded when the system voids a pending draft.
const (
	DraftReasonTokenRevoked = "token revoked"
	DraftReasonFlowDeleted  = "flow deleted"
)

const (
	// MaxPendingDraftsPerToken bounds the drafts one token may have waiting.
	MaxPendingDraftsPerToken = 20
	// MaxDraftDataBytes bounds the stored proposed flow (the service limits
	// what an agent sends to 512 KiB; resolved values may add to it).
	MaxDraftDataBytes = 1 << 20
	// MaxDraftReportBytes bounds the stored issues and alerts (each).
	MaxDraftReportBytes = 256 << 10
	// MaxDraftNoteBytes bounds the agent's note (2000 characters).
	MaxDraftNoteBytes = 8 << 10
	// MaxDraftReasonBytes bounds the admin's reason (500 characters).
	MaxDraftReasonBytes = 2 << 10
	// DraftRetention is how long decided drafts are kept.
	DraftRetention = 30 * 24 * time.Hour
	// DefaultDraftListLimit and MaxDraftListLimit bound listings.
	DefaultDraftListLimit = 100
	MaxDraftListLimit     = 500
)

var (
	ErrFlowDraftNotFound = errors.New("draft not found")
	ErrInvalidFlowDraft  = errors.New("invalid draft")
	// ErrDraftLimit refuses a new draft while its token already has
	// MaxPendingDraftsPerToken drafts waiting.
	ErrDraftLimit = errors.New("too many drafts are already waiting for the admin")
	// ErrDraftNotPending refuses to decide a draft that is not pending.
	ErrDraftNotPending = errors.New("the draft is no longer pending")
	// ErrDraftTokenRevoked refuses a draft whose author token is unknown or
	// revoked.
	ErrDraftTokenRevoked = errors.New("the token is revoked")
)

// FlowDraft is one proposal. Data is the proposed flow JSON (resolved: no
// redaction sentinels); IssuesJSON and AlertsJSON are the lint issues and
// diff alerts computed when it was saved. Name is the proposed flow's name,
// read from Data. Listings leave Data nil.
type FlowDraft struct {
	ID              string          `json:"id"`
	FlowID          string          `json:"flowId"`      // "" for a new flow
	BaseVersion     int             `json:"baseVersion"` // 0 for a new flow
	Name            string          `json:"name"`
	Data            json.RawMessage `json:"data,omitempty"`
	Note            string          `json:"note"`
	AuthorTokenID   string          `json:"authorTokenId"`
	AuthorTokenName string          `json:"authorTokenName"`
	Status          string          `json:"status"`
	Risk            string          `json:"risk"`
	IssuesJSON      json.RawMessage `json:"issues"`
	AlertsJSON      json.RawMessage `json:"alerts"`
	Reason          string          `json:"reason,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	DecidedAt       *time.Time      `json:"decidedAt,omitempty"`
	AppliedVersion  int             `json:"appliedVersion,omitempty"` // the version an approval saved
}

// FlowDraftFilter selects drafts for ListFlowDrafts. Status is "" or
// DraftFilterAll (every draft), DraftFilterDecided, or one status.
type FlowDraftFilter struct {
	Status  string
	TokenID string
	FlowID  string
}

const flowDraftsSchema = `
	CREATE TABLE IF NOT EXISTS flow_drafts (
		id                TEXT PRIMARY KEY,
		seq               INTEGER NOT NULL,
		flow_id           TEXT NOT NULL DEFAULT '',
		base_version      INTEGER NOT NULL DEFAULT 0,
		data              TEXT NOT NULL,
		note              TEXT NOT NULL DEFAULT '',
		author_token_id   TEXT NOT NULL,
		author_token_name TEXT NOT NULL DEFAULT '',
		status            TEXT NOT NULL,
		risk              TEXT NOT NULL DEFAULT '',
		issues_json       TEXT NOT NULL DEFAULT '[]',
		alerts_json       TEXT NOT NULL DEFAULT '[]',
		reason            TEXT NOT NULL DEFAULT '',
		created_at        INTEGER NOT NULL,
		decided_at        INTEGER,
		applied_version   INTEGER
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_flow_drafts_seq ON flow_drafts(seq);
	CREATE INDEX IF NOT EXISTS idx_flow_drafts_token ON flow_drafts(author_token_id, status);
	CREATE INDEX IF NOT EXISTS idx_flow_drafts_flow ON flow_drafts(flow_id, status);
	CREATE INDEX IF NOT EXISTS idx_flow_drafts_status ON flow_drafts(status, decided_at);
`

func invalidDraft(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidFlowDraft, fmt.Sprintf(format, args...))
}

// draftReport returns a stored issues/alerts list: "[]" when empty.
func draftReport(name string, raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "[]", nil
	}
	if len(raw) > MaxDraftReportBytes {
		return "", invalidDraft("%s exceed %d bytes", name, MaxDraftReportBytes)
	}
	if !json.Valid(raw) {
		return "", invalidDraft("%s are not valid JSON", name)
	}
	return string(raw), nil
}

// draftServerFields are the flow fields CreateFlowDraft drops from a draft.
var draftServerFields = []string{"state", "version", "createdAt", "updatedAt"}

// validateNewDraft checks a new draft and drops draftServerFields from its
// data.
func validateNewDraft(d *FlowDraft) error {
	if d.AuthorTokenID == "" {
		return invalidDraft("the author token is required")
	}
	if d.Status != "" && d.Status != DraftPending {
		return invalidDraft("a new draft is pending")
	}
	if d.AppliedVersion != 0 || d.DecidedAt != nil || d.Reason != "" {
		return invalidDraft("a new draft is not decided")
	}
	if (d.FlowID == "") != (d.BaseVersion == 0) || d.BaseVersion < 0 {
		return invalidDraft("a draft of an existing flow names its base version; a new flow has none")
	}
	if len(d.Data) == 0 {
		return invalidDraft("the proposed flow is required")
	}
	if len(d.Data) > MaxDraftDataBytes {
		return invalidDraft("the proposed flow exceeds %d bytes", MaxDraftDataBytes)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(d.Data, &obj); err != nil || obj == nil {
		return invalidDraft("the proposed flow is not a JSON object")
	}
	// A draft is a definition: run state, version and timestamps are the
	// server's (a save keeps the stored ones) and are not stored with it.
	stripped := false
	for _, key := range draftServerFields {
		if _, ok := obj[key]; ok {
			delete(obj, key)
			stripped = true
		}
	}
	if stripped {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(obj); err != nil {
			return invalidDraft("the proposed flow cannot be stored")
		}
		d.Data = json.RawMessage(bytes.TrimSpace(buf.Bytes()))
	}
	if len(d.Note) > MaxDraftNoteBytes {
		return invalidDraft("note exceeds %d bytes", MaxDraftNoteBytes)
	}
	switch d.Risk {
	case "", ApprovalToolRiskSafe, ApprovalToolRiskExternal, ApprovalToolRiskPrivileged:
	default:
		return invalidDraft("unknown risk %q", d.Risk)
	}
	return nil
}

// CreateFlowDraft stores a new pending draft (an empty ID is generated; a
// zero CreatedAt is now; draftServerFields are dropped from the data) and
// returns it. In one transaction it checks that
// the author token exists and is not revoked (ErrDraftTokenRevoked), that
// the token has fewer than MaxPendingDraftsPerToken drafts waiting
// (ErrDraftLimit) and, for an existing flow, that the flow exists
// (ErrFlowNotFound) at BaseVersion (*VersionConflictError); it also prunes
// decided drafts older than DraftRetention.
func (s *SQLiteStore) CreateFlowDraft(d FlowDraft) (*FlowDraft, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	if err := validateNewDraft(&d); err != nil {
		return nil, err
	}
	issues, err := draftReport("issues", d.IssuesJSON)
	if err != nil {
		return nil, err
	}
	alerts, err := draftReport("alerts", d.AlertsJSON)
	if err != nil {
		return nil, err
	}
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.CreatedAt = time.Unix(d.CreatedAt.Unix(), 0).UTC()
	d.Status = DraftPending
	d.IssuesJSON, d.AlertsJSON = json.RawMessage(issues), json.RawMessage(alerts)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin draft creation: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	var active int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE id = ? AND revoked_at IS NULL`, d.AuthorTokenID).Scan(&active); err != nil {
		return nil, fmt.Errorf("failed to check the draft's token: %w", err)
	}
	if active == 0 {
		return nil, ErrDraftTokenRevoked
	}
	var pending int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM flow_drafts WHERE status = ? AND author_token_id = ?`, DraftPending, d.AuthorTokenID).Scan(&pending); err != nil {
		return nil, fmt.Errorf("failed to count pending drafts: %w", err)
	}
	if pending >= MaxPendingDraftsPerToken {
		return nil, ErrDraftLimit
	}
	if d.FlowID != "" {
		var current int
		err := tx.QueryRow(`SELECT version FROM flows WHERE id = ?`, d.FlowID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFlowNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read the flow's version: %w", err)
		}
		if current != d.BaseVersion {
			return nil, &VersionConflictError{Current: current}
		}
	}
	if _, err := tx.Exec(`DELETE FROM flow_drafts WHERE status != ? AND decided_at < ?`, DraftPending, now.Add(-DraftRetention).Unix()); err != nil {
		return nil, fmt.Errorf("failed to prune drafts: %w", err)
	}
	_, err = tx.Exec(`
		INSERT INTO flow_drafts (id, seq, flow_id, base_version, data, note, author_token_id, author_token_name,
			status, risk, issues_json, alerts_json, created_at)
		VALUES (?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM flow_drafts), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, d.ID, d.FlowID, d.BaseVersion, string(d.Data), d.Note, d.AuthorTokenID, d.AuthorTokenName,
		d.Status, d.Risk, issues, alerts, d.CreatedAt.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return nil, invalidDraft("draft %s already exists", d.ID)
		}
		return nil, fmt.Errorf("failed to create draft: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit draft: %w", err)
	}
	d.Name = draftName(d.Data)
	return &d, nil
}

// draftName reads the name of a proposed flow.
func draftName(data json.RawMessage) string {
	var v struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(data, &v)
	return v.Name
}

func draftColumns(withData bool) string {
	cols := `id, flow_id, base_version, COALESCE(json_extract(data, '$.name'), ''), note, author_token_id, author_token_name,
		status, risk, issues_json, alerts_json, reason, created_at, decided_at, applied_version`
	if withData {
		cols += `, data`
	}
	return cols
}

func scanFlowDraft(row rowScanner, withData bool) (*FlowDraft, error) {
	var (
		d              FlowDraft
		name           any
		issues, alerts string
		created        int64
		decided        sql.NullInt64
		applied        sql.NullInt64
		data           string
	)
	dest := []any{&d.ID, &d.FlowID, &d.BaseVersion, &name, &d.Note, &d.AuthorTokenID, &d.AuthorTokenName,
		&d.Status, &d.Risk, &issues, &alerts, &d.Reason, &created, &decided, &applied}
	if withData {
		dest = append(dest, &data)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if s, ok := name.(string); ok {
		d.Name = s
	}
	d.IssuesJSON, d.AlertsJSON = json.RawMessage(issues), json.RawMessage(alerts)
	d.CreatedAt = time.Unix(created, 0).UTC()
	d.DecidedAt = unixPtr(decided)
	if applied.Valid {
		d.AppliedVersion = int(applied.Int64)
	}
	if withData {
		d.Data = json.RawMessage(data)
	}
	return &d, nil
}

// GetFlowDraft returns a draft with its data, or ErrFlowDraftNotFound.
func (s *SQLiteStore) GetFlowDraft(id string) (*FlowDraft, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	d, err := scanFlowDraft(s.db.QueryRow(`SELECT `+draftColumns(true)+` FROM flow_drafts WHERE id = ?`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFlowDraftNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get draft: %w", err)
	}
	return d, nil
}

// ListFlowDrafts returns the drafts matching filter, newest first, without
// their data: at most limit (0 for DefaultDraftListLimit, capped at
// MaxDraftListLimit), and only those created before the draft beforeID when
// it is set (an unknown beforeID lists nothing).
func (s *SQLiteStore) ListFlowDrafts(filter FlowDraftFilter, limit int, beforeID string) ([]FlowDraft, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	if limit <= 0 {
		limit = DefaultDraftListLimit
	}
	if limit > MaxDraftListLimit {
		limit = MaxDraftListLimit
	}
	var (
		where []string
		args  []any
	)
	switch filter.Status {
	case "", DraftFilterAll:
	case DraftFilterDecided:
		where, args = append(where, `status != ?`), append(args, DraftPending)
	case DraftPending, DraftApproved, DraftRejected, DraftVoid:
		where, args = append(where, `status = ?`), append(args, filter.Status)
	default:
		return nil, invalidDraft("unknown status filter %q", filter.Status)
	}
	if filter.TokenID != "" {
		where, args = append(where, `author_token_id = ?`), append(args, filter.TokenID)
	}
	if filter.FlowID != "" {
		where, args = append(where, `flow_id = ?`), append(args, filter.FlowID)
	}
	if beforeID != "" {
		where, args = append(where, `seq < (SELECT seq FROM flow_drafts WHERE id = ?)`), append(args, beforeID)
	}
	query := `SELECT ` + draftColumns(false) + ` FROM flow_drafts`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY seq DESC LIMIT ?`
	rows, err := s.db.Query(query, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("failed to list drafts: %w", err)
	}
	defer rows.Close()
	out := []FlowDraft{}
	for rows.Next() {
		d, err := scanFlowDraft(rows, false)
		if err != nil {
			return nil, fmt.Errorf("failed to scan draft: %w", err)
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating drafts: %w", err)
	}
	return out, nil
}

// DecideFlowDraft closes a pending draft as approved (recording the version
// the approval saved, ≥ 1), rejected or void, with an optional reason. A
// draft that is not pending is ErrDraftNotPending, except that approving a
// draft already approved with the same version succeeds without change (a
// save that applies a draft closes it itself: VersionMeta.DraftID); an
// unknown one is ErrFlowDraftNotFound.
func (s *SQLiteStore) DecideFlowDraft(id, status, reason string, appliedVersion int) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	switch status {
	case DraftApproved:
		if appliedVersion < 1 {
			return invalidDraft("an approved draft records the version it saved")
		}
	case DraftRejected, DraftVoid:
		if appliedVersion != 0 {
			return invalidDraft("only an approved draft applies a version")
		}
	default:
		return invalidDraft("cannot decide a draft as %q", status)
	}
	if len(reason) > MaxDraftReasonBytes {
		return invalidDraft("reason exceeds %d bytes", MaxDraftReasonBytes)
	}
	var applied any
	if appliedVersion > 0 {
		applied = appliedVersion
	}
	res, err := s.db.Exec(`
		UPDATE flow_drafts SET status = ?, reason = ?, decided_at = ?, applied_version = ?
		WHERE id = ? AND status = ?
	`, status, reason, time.Now().Unix(), applied, id, DraftPending)
	if err != nil {
		return fmt.Errorf("failed to decide draft: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 1 {
		return nil
	}
	d, err := s.GetFlowDraft(id)
	if err != nil {
		return err
	}
	if status == DraftApproved && d.Status == DraftApproved && d.AppliedVersion == appliedVersion {
		return nil
	}
	return ErrDraftNotPending
}

// CountPendingFlowDrafts returns how many drafts wait for the admin. It is a
// read-only query (the editor's badge polls it passively, ADR-0017).
func (s *SQLiteStore) CountPendingFlowDrafts() (int, error) {
	if s.isClosed() {
		return 0, ErrDatabaseClosed
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM flow_drafts WHERE status = ?`, DraftPending).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to count pending drafts: %w", err)
	}
	return n, nil
}

// applyDraftTx closes the draft meta.DraftID (if any) as approved with
// appliedVersion, inside tx: the transaction that saves its definition as
// flowID ("" for a draft creating a new flow) on top of version base (0 for
// a new flow). It fails, and the caller rolls back, when the draft is
// unknown or, for an agent, another token's (ErrFlowDraftNotFound), not
// pending (ErrDraftNotPending: decided, or voided by a revocation that
// committed first), of another flow (ErrInvalidFlowDraft), based on another
// version (*VersionConflictError) or, for an agent, its token is revoked
// (ErrDraftTokenRevoked). Revocations void drafts in their own
// transactions, so a voided draft can never become a version and an
// applied one is never voided.
func applyDraftTx(tx *sql.Tx, meta VersionMeta, flowID string, base, appliedVersion int, now time.Time) error {
	if meta.DraftID == "" {
		return nil
	}
	var (
		draftFlow, status, author string
		draftBase, tokenActive    int
	)
	err := tx.QueryRow(`SELECT d.flow_id, d.base_version, d.status, d.author_token_id, `+draftTokenUnrevoked+`
		FROM flow_drafts d WHERE d.id = ?`, meta.DraftID).Scan(&draftFlow, &draftBase, &status, &author, &tokenActive)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrFlowDraftNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to read the draft: %w", err)
	}
	agent := meta.ActorKind == VersionActorAgent
	switch {
	case agent && author != meta.TokenID:
		return ErrFlowDraftNotFound
	case status != DraftPending:
		return ErrDraftNotPending
	case draftFlow != flowID:
		if flowID == "" {
			return invalidDraft("draft %s changes an existing flow; it cannot create one", meta.DraftID)
		}
		return invalidDraft("draft %s is not a draft of flow %s", meta.DraftID, flowID)
	case draftBase != base:
		return &VersionConflictError{Current: base}
	case agent && tokenActive == 0:
		return ErrDraftTokenRevoked
	}
	if _, err := tx.Exec(`
		UPDATE flow_drafts SET status = ?, reason = '', decided_at = ?, applied_version = ?
		WHERE id = ? AND status = ?
	`, DraftApproved, now.Unix(), appliedVersion, meta.DraftID, DraftPending); err != nil {
		return fmt.Errorf("failed to close the applied draft: %w", err)
	}
	return nil
}

// voidPendingDrafts closes, as void with reason, the pending drafts that
// match cond (on flow_drafts aliased d), inside tx.
func voidPendingDrafts(tx *sql.Tx, cond string, condArgs []any, reason string, now time.Time) error {
	args := append([]any{DraftVoid, reason, now.Unix(), DraftPending}, condArgs...)
	if _, err := tx.Exec(`
		UPDATE flow_drafts AS d SET status = ?, reason = ?, decided_at = ?
		WHERE d.status = ? AND `+cond, args...); err != nil {
		return fmt.Errorf("failed to void pending drafts: %w", err)
	}
	return nil
}

// draftTokenUnrevoked matches drafts (aliased d) whose author token exists
// and is not revoked.
const draftTokenUnrevoked = `EXISTS (SELECT 1 FROM api_tokens t WHERE t.id = d.author_token_id AND t.revoked_at IS NULL)`
