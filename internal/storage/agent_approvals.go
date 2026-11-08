package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Agent approvals (docs/design/mcp.md §9): a tool call with confirm "admin" or
// an agent's draft deploy waits here until the admin approves or rejects it in
// the editor, or it expires.

// Approval kinds.
const (
	ApprovalKindToolCall    = "tool_call"
	ApprovalKindDraftDeploy = "draft_deploy"
)

// Approval statuses.
const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
	ApprovalExpired  = "expired"
)

// DecidedBy values recorded when the system, not the admin, closes a pending
// approval because the token that requested it can no longer act.
const (
	ApprovalDecidedByTokenRevoked = "token revoked"
	ApprovalDecidedByTokenExpired = "token expired"
)

const (
	// ToolCallApprovalLifetime is how long a tool call waits for the admin.
	ToolCallApprovalLifetime = 15 * time.Minute
	// DraftDeployApprovalLifetime is how long a draft deploy request waits.
	DraftDeployApprovalLifetime = 7 * 24 * time.Hour
	// AgentApprovalRetention is how long decided approvals are kept.
	AgentApprovalRetention = 30 * 24 * time.Hour
	// MaxApprovalArgsBytes bounds the stored arguments.
	MaxApprovalArgsBytes = 64 << 10
	// MaxApprovalResultBytes bounds the stored CallResult of an approved
	// tool call (the service already caps results far below it).
	MaxApprovalResultBytes = 2 << 20
	// MaxApprovalSummaryLength bounds the summary (bytes).
	MaxApprovalSummaryLength = 2048
	// MaxApprovalToolRefLength bounds ToolName and ToolFingerprint (bytes).
	MaxApprovalToolRefLength = 256
	// MaxPendingApprovalsPerToken bounds the approvals of one kind that one
	// token may have waiting at a time.
	MaxPendingApprovalsPerToken = 20
	// DefaultApprovalListLimit and MaxApprovalListLimit bound listings.
	DefaultApprovalListLimit = 100
	MaxApprovalListLimit     = 500
)

var (
	ErrAgentApprovalNotFound = errors.New("approval not found")
	ErrAgentApprovalExists   = errors.New("approval already exists")
	ErrAgentApprovalDecided  = errors.New("approval already decided")
	ErrAgentApprovalExpired  = errors.New("approval expired")
	ErrInvalidAgentApproval  = errors.New("invalid approval")
	// ErrTooManyPendingApprovals refuses a new approval while the token
	// already has MaxPendingApprovalsPerToken of that kind waiting.
	ErrTooManyPendingApprovals = errors.New("too many requests are already waiting for the admin")
	// ErrAgentApprovalTokenRevoked refuses to approve a request whose token
	// is revoked, expired or unknown; the approval is closed as rejected.
	ErrAgentApprovalTokenRevoked = errors.New("the token that requested this approval is revoked or expired")
)

// AgentApproval is one request waiting for (or decided by) the admin.
// TokenName is read from api_tokens and is not stored with the approval.
type AgentApproval struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	TokenID   string `json:"tokenId"`
	TokenName string `json:"tokenName,omitempty"`
	FlowID    string `json:"flowId,omitempty"`
	Ref       string `json:"ref"`
	// ToolName and ToolFingerprint identify the tool a tool call asks for,
	// as published when the call was made: an approval runs only that
	// version of the tool (empty for other kinds).
	ToolName        string          `json:"toolName,omitempty"`
	ToolFingerprint string          `json:"toolFingerprint,omitempty"`
	Args            json.RawMessage `json:"args,omitempty"`
	Summary         string          `json:"summary"`
	Status          string          `json:"status"`
	CreatedAt       time.Time       `json:"createdAt"`
	DecidedAt       *time.Time      `json:"decidedAt,omitempty"`
	DecidedBy       string          `json:"decidedBy,omitempty"`
	ExpiresAt       time.Time       `json:"expiresAt"`
	// Result is the JSON CallResult of an approved tool call once it ran
	// (SetAgentApprovalResult); nil before.
	Result json.RawMessage `json:"result,omitempty"`

	// TokenRevokedAt and TokenExpiresAt describe the requesting token as read
	// with the approval (TokenExpiresAt is nil when no such token exists).
	// They are not stored with the approval; see TokenActive.
	TokenRevokedAt *time.Time `json:"-"`
	TokenExpiresAt *time.Time `json:"-"`
}

// TokenActive reports whether the token that requested the approval existed,
// was not revoked and had not expired at now, as of the read.
func (a *AgentApproval) TokenActive(now time.Time) bool {
	return a.TokenExpiresAt != nil && a.TokenRevokedAt == nil && now.Before(*a.TokenExpiresAt)
}

// EffectiveStatus is Status, except that a pending approval at or past its
// expiry reports "expired" even before ExpireAgentApprovals marks it.
func (a *AgentApproval) EffectiveStatus(now time.Time) string {
	if a.Status == ApprovalPending && !now.Before(a.ExpiresAt) {
		return ApprovalExpired
	}
	return a.Status
}

// AgentApprovalFilter selects approvals for ListAgentApprovals.
type AgentApprovalFilter struct {
	Status string // "" for every status
	Limit  int    // 0 for DefaultApprovalListLimit
}

func invalidApproval(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidAgentApproval, fmt.Sprintf(format, args...))
}

// CreateAgentApproval stores a new pending approval. An empty ID is generated
// (callers may pass the run id); a zero ExpiresAt defaults per kind. A token
// may have at most MaxPendingApprovalsPerToken approvals of each kind waiting
// (ErrTooManyPendingApprovals). Each creation also runs the expiry and
// retention pass of ExpireAgentApprovals, so agent traffic alone keeps the
// table bounded even when nobody opens the queue.
func (s *SQLiteStore) CreateAgentApproval(a AgentApproval, now time.Time) (*AgentApproval, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	var lifetime time.Duration
	switch a.Kind {
	case ApprovalKindToolCall:
		lifetime = ToolCallApprovalLifetime
	case ApprovalKindDraftDeploy:
		lifetime = DraftDeployApprovalLifetime
	default:
		return nil, invalidApproval("unknown kind %q", a.Kind)
	}
	if a.TokenID == "" || a.Ref == "" {
		return nil, invalidApproval("token and ref are required")
	}
	if len(a.Args) > MaxApprovalArgsBytes {
		return nil, invalidApproval("arguments exceed %d bytes", MaxApprovalArgsBytes)
	}
	if len(a.Args) > 0 && !json.Valid(a.Args) {
		return nil, invalidApproval("arguments are not valid JSON")
	}
	if len(a.Summary) > MaxApprovalSummaryLength {
		return nil, invalidApproval("summary exceeds %d bytes", MaxApprovalSummaryLength)
	}
	if len(a.ToolName) > MaxApprovalToolRefLength || len(a.ToolFingerprint) > MaxApprovalToolRefLength {
		return nil, invalidApproval("tool name and fingerprint must not exceed %d bytes", MaxApprovalToolRefLength)
	}
	if a.ExpiresAt.IsZero() {
		a.ExpiresAt = now.Add(lifetime)
	}
	if !a.ExpiresAt.After(now) {
		return nil, invalidApproval("expiry must be in the future")
	}
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	a.Status = ApprovalPending
	a.TokenName = ""
	a.Result = nil
	a.DecidedAt, a.DecidedBy = nil, ""
	a.CreatedAt = time.Unix(now.Unix(), 0).UTC()
	a.ExpiresAt = time.Unix(a.ExpiresAt.Unix(), 0).UTC()
	var args any
	if len(a.Args) > 0 {
		args = string(a.Args)
	} else {
		a.Args = nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin approval creation: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var pending int
	err = tx.QueryRow(`
		SELECT COUNT(*) FROM agent_approvals
		WHERE status = ? AND expires_at > ? AND token_id = ? AND kind = ?
	`, ApprovalPending, now.Unix(), a.TokenID, a.Kind).Scan(&pending)
	if err != nil {
		return nil, fmt.Errorf("failed to count pending approvals: %w", err)
	}
	if pending >= MaxPendingApprovalsPerToken {
		return nil, ErrTooManyPendingApprovals
	}
	if _, err := expireAgentApprovals(tx, now); err != nil {
		return nil, err
	}
	_, err = tx.Exec(`
		INSERT INTO agent_approvals (id, kind, token_id, flow_id, ref, tool_name, tool_fingerprint, args_json, summary, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, a.ID, a.Kind, a.TokenID, a.FlowID, a.Ref, a.ToolName, a.ToolFingerprint, args, a.Summary, a.Status, a.CreatedAt.Unix(), a.ExpiresAt.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAgentApprovalExists
		}
		return nil, fmt.Errorf("failed to create approval: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit approval: %w", err)
	}
	return &a, nil
}

// expireApprovalSet marks an approval expired at its expiry instant. A tool
// call that never ran loses its arguments (up to MaxApprovalArgsBytes each),
// which are not worth keeping for the retention period; the audit log keeps
// their hash and a preview. Bind ApprovalExpired and ApprovalKindToolCall.
const expireApprovalSet = `status = ?, decided_at = expires_at, args_json = CASE WHEN kind = ? THEN NULL ELSE args_json END`

// expireAgentApprovals marks pending approvals past their expiry as expired,
// returns how many changed, and deletes decided approvals older than
// AgentApprovalRetention.
func expireAgentApprovals(tx *sql.Tx, now time.Time) (int64, error) {
	res, err := tx.Exec(`UPDATE agent_approvals SET `+expireApprovalSet+` WHERE status = ? AND expires_at <= ?`,
		ApprovalExpired, ApprovalKindToolCall, ApprovalPending, now.Unix())
	if err != nil {
		return 0, fmt.Errorf("failed to expire approvals: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM agent_approvals WHERE status != ? AND decided_at < ?`, ApprovalPending, now.Add(-AgentApprovalRetention).Unix()); err != nil {
		return 0, fmt.Errorf("failed to prune approvals: %w", err)
	}
	return n, nil
}

const agentApprovalSelect = `
	SELECT a.id, a.kind, a.token_id, COALESCE(t.name, ''), a.flow_id, a.ref, a.tool_name, a.tool_fingerprint, a.args_json, a.summary,
	       a.status, a.created_at, a.decided_at, a.decided_by, a.expires_at, t.revoked_at, t.expires_at, a.result_json
	FROM agent_approvals a LEFT JOIN api_tokens t ON t.id = a.token_id`

// approvalTokenActive matches approvals (aliased a) whose token exists, is
// not revoked and has not expired at the bound time.
const approvalTokenActive = `EXISTS (
	SELECT 1 FROM api_tokens t WHERE t.id = a.token_id AND t.revoked_at IS NULL AND t.expires_at > ?)`

func scanAgentApproval(row rowScanner) (*AgentApproval, error) {
	var (
		a                          AgentApproval
		args, result               sql.NullString
		created, expires           int64
		decided                    sql.NullInt64
		tokenRevoked, tokenExpires sql.NullInt64
	)
	if err := row.Scan(&a.ID, &a.Kind, &a.TokenID, &a.TokenName, &a.FlowID, &a.Ref, &a.ToolName, &a.ToolFingerprint, &args, &a.Summary, &a.Status, &created, &decided, &a.DecidedBy, &expires, &tokenRevoked, &tokenExpires, &result); err != nil {
		return nil, err
	}
	if args.Valid {
		a.Args = json.RawMessage(args.String)
	}
	if result.Valid {
		a.Result = json.RawMessage(result.String)
	}
	a.CreatedAt = time.Unix(created, 0).UTC()
	a.ExpiresAt = time.Unix(expires, 0).UTC()
	a.DecidedAt = unixPtr(decided)
	a.TokenRevokedAt = unixPtr(tokenRevoked)
	a.TokenExpiresAt = unixPtr(tokenExpires)
	return &a, nil
}

// GetAgentApproval returns an approval as stored (see EffectiveStatus).
func (s *SQLiteStore) GetAgentApproval(id string) (*AgentApproval, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	a, err := scanAgentApproval(s.db.QueryRow(agentApprovalSelect+` WHERE a.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentApprovalNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get approval: %w", err)
	}
	return a, nil
}

// ListAgentApprovals returns approvals newest first.
func (s *SQLiteStore) ListAgentApprovals(f AgentApprovalFilter) ([]AgentApproval, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	switch f.Status {
	case "", ApprovalPending, ApprovalApproved, ApprovalRejected, ApprovalExpired:
	default:
		return nil, invalidApproval("unknown status %q", f.Status)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultApprovalListLimit
	}
	limit = min(limit, MaxApprovalListLimit)
	rows, err := s.db.Query(agentApprovalSelect+`
		WHERE (? = '' OR a.status = ?)
		ORDER BY a.created_at DESC, a.rowid DESC
		LIMIT ?`, f.Status, f.Status, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list approvals: %w", err)
	}
	defer rows.Close()
	out := []AgentApproval{}
	for rows.Next() {
		a, err := scanAgentApproval(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan approval: %w", err)
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating approvals: %w", err)
	}
	return out, nil
}

// CountPendingAgentApprovals counts approvals still awaiting a decision at now.
func (s *SQLiteStore) CountPendingAgentApprovals(now time.Time) (int, error) {
	if s.isClosed() {
		return 0, ErrDatabaseClosed
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM agent_approvals WHERE status = ? AND expires_at > ?`, ApprovalPending, now.Unix()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("failed to count approvals: %w", err)
	}
	return n, nil
}

// DecideAgentApproval approves or rejects a pending, unexpired approval. A
// decision is final: deciding again returns ErrAgentApprovalDecided, and a
// pending approval past its expiry is marked expired and returns
// ErrAgentApprovalExpired. Only a request whose token is still active can be
// approved: otherwise the approval is closed as rejected (DecidedBy says why)
// and ErrAgentApprovalTokenRevoked is returned. Rejecting needs no token.
func (s *SQLiteStore) DecideAgentApproval(id string, approve bool, decidedBy string, now time.Time) (*AgentApproval, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	status := ApprovalRejected
	if approve {
		status = ApprovalApproved
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin approval decision: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	query := `
		UPDATE agent_approvals AS a SET status = ?, decided_at = ?, decided_by = ?
		WHERE a.id = ? AND a.status = ? AND a.expires_at > ?`
	args := []any{status, now.Unix(), decidedBy, id, ApprovalPending, now.Unix()}
	if approve {
		query += ` AND ` + approvalTokenActive
		args = append(args, now.Unix())
	}
	res, err := tx.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to decide approval: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	var decideErr error
	if n == 0 {
		cur, err := scanAgentApproval(tx.QueryRow(agentApprovalSelect+` WHERE a.id = ?`, id))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrAgentApprovalNotFound
		case err != nil:
			return nil, fmt.Errorf("failed to read approval: %w", err)
		case cur.Status == ApprovalPending && !now.Before(cur.ExpiresAt):
			if _, err := tx.Exec(`UPDATE agent_approvals SET `+expireApprovalSet+` WHERE id = ?`, ApprovalExpired, ApprovalKindToolCall, id); err != nil {
				return nil, fmt.Errorf("failed to expire approval: %w", err)
			}
			decideErr = ErrAgentApprovalExpired
		case cur.Status == ApprovalPending:
			// Only an approval can fail on a live pending request: its
			// token can no longer act, so the request is void.
			reason := ApprovalDecidedByTokenRevoked
			if cur.TokenRevokedAt == nil && cur.TokenExpiresAt != nil {
				reason = ApprovalDecidedByTokenExpired
			}
			if _, err := tx.Exec(`UPDATE agent_approvals SET status = ?, decided_at = ?, decided_by = ? WHERE id = ?`, ApprovalRejected, now.Unix(), reason, id); err != nil {
				return nil, fmt.Errorf("failed to void approval: %w", err)
			}
			decideErr = ErrAgentApprovalTokenRevoked
		case cur.Status == ApprovalExpired:
			return nil, ErrAgentApprovalExpired
		default:
			return nil, ErrAgentApprovalDecided
		}
	}
	a, err := scanAgentApproval(tx.QueryRow(agentApprovalSelect+` WHERE a.id = ?`, id))
	if err != nil {
		return nil, fmt.Errorf("failed to read approval: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit approval decision: %w", err)
	}
	if decideErr != nil {
		return nil, decideErr
	}
	return a, nil
}

// ExpireAgentApprovals marks pending approvals past their expiry as expired
// (dropping the arguments of tool calls, which never ran), returns how many
// changed, and deletes decided approvals older than AgentApprovalRetention.
// CreateAgentApproval runs the same pass.
func (s *SQLiteStore) ExpireAgentApprovals(now time.Time) (int, error) {
	if s.isClosed() {
		return 0, ErrDatabaseClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin approval expiry: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	n, err := expireAgentApprovals(tx, now)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit approval expiry: %w", err)
	}
	return int(n), nil
}

const agentApprovalsSchema = `
	CREATE TABLE IF NOT EXISTS agent_approvals (
		id         TEXT PRIMARY KEY,
		kind       TEXT NOT NULL,
		token_id   TEXT NOT NULL,
		flow_id    TEXT NOT NULL DEFAULT '',
		ref        TEXT NOT NULL,
		tool_name  TEXT NOT NULL DEFAULT '',
		tool_fingerprint TEXT NOT NULL DEFAULT '',
		args_json  TEXT,
		summary    TEXT NOT NULL DEFAULT '',
		status     TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		decided_at INTEGER,
		decided_by TEXT NOT NULL DEFAULT '',
		expires_at INTEGER NOT NULL,
		result_json TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_agent_approvals_status ON agent_approvals(status, expires_at);
	CREATE INDEX IF NOT EXISTS idx_agent_approvals_created ON agent_approvals(created_at);
	CREATE INDEX IF NOT EXISTS idx_agent_approvals_decided ON agent_approvals(decided_at);
`

// SetAgentApprovalResult stores the JSON CallResult of an approved tool call
// that ran (replacing an earlier one, e.g. "running" by the final result),
// so the agent can fetch it later. Only approved tool calls hold a result.
func (s *SQLiteStore) SetAgentApprovalResult(id string, result json.RawMessage) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if len(result) == 0 || !json.Valid(result) {
		return invalidApproval("the result is not valid JSON")
	}
	if len(result) > MaxApprovalResultBytes {
		return invalidApproval("the result exceeds %d bytes", MaxApprovalResultBytes)
	}
	res, err := s.db.Exec(`UPDATE agent_approvals SET result_json = ? WHERE id = ? AND kind = ? AND status = ?`,
		string(result), id, ApprovalKindToolCall, ApprovalApproved)
	if err != nil {
		return fmt.Errorf("failed to store approval result: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n > 0 {
		return nil
	}
	if _, err := s.GetAgentApproval(id); err != nil {
		return err
	}
	return invalidApproval("only an approved tool call holds a result")
}

// migrateAgentApprovals adds the columns newer versions need to an existing
// agent_approvals table. It is idempotent.
func migrateAgentApprovals(db *sql.DB) error {
	for _, col := range []struct{ name, def string }{
		{"result_json", "TEXT"},
		{"tool_name", "TEXT NOT NULL DEFAULT ''"},
		{"tool_fingerprint", "TEXT NOT NULL DEFAULT ''"},
	} {
		has, err := historyHasColumn(db, "agent_approvals", col.name)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE agent_approvals ADD COLUMN ` + col.name + ` ` + col.def); err != nil {
			return fmt.Errorf("failed to add agent_approvals.%s: %w", col.name, err)
		}
	}
	return nil
}
