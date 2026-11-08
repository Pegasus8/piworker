package storage

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MCP audit (docs/design/mcp.md §9). Calls are written asynchronously in
// batches; rejections that can be frequent (busy, cooldown, invalid args) are
// kept as per-token per-minute counters instead of rows. Everything is kept
// for MCPAuditRetention; a row cap may evict older rows early, but never
// privileged calls.

const (
	// MCPAuditRetention is how long audit rows and counters are kept.
	MCPAuditRetention = 30 * 24 * time.Hour
	// MaxMCPAuditPreview bounds the stored argument preview (bytes).
	MaxMCPAuditPreview = 512
	// DefaultMCPAuditLimit and MaxMCPAuditLimit bound listings.
	DefaultMCPAuditLimit = 100
	MaxMCPAuditLimit     = 1000
)

// MCPAuditEntry is one audited agent call.
type MCPAuditEntry struct {
	ID          int64     `json:"id"`
	TS          time.Time `json:"ts"`
	TokenID     string    `json:"tokenId"`
	TokenName   string    `json:"tokenName"`
	Tool        string    `json:"tool"`
	FlowID      string    `json:"flowId,omitempty"`
	RunID       string    `json:"runId,omitempty"`
	ArgsSHA256  string    `json:"argsSha256,omitempty"`
	ArgsPreview string    `json:"argsPreview,omitempty"`
	Outcome     string    `json:"outcome"`
	DurMs       int64     `json:"durMs"`
	Privileged  bool      `json:"privileged"`
}

// MCPRejection counts rejections of one reason for one token. At is
// truncated to its minute when stored.
type MCPRejection struct {
	TokenID string    `json:"tokenId"`
	At      time.Time `json:"at"`
	Reason  string    `json:"reason"`
	Count   int       `json:"count"`
}

// MCPAuditFilter selects audit rows (and, by token and time, counters).
// Empty fields match everything; Since is inclusive and Until exclusive.
type MCPAuditFilter struct {
	TokenID  string
	Tool     string
	FlowID   string
	Outcome  string
	Since    time.Time
	Until    time.Time
	BeforeID int64 // only rows with a smaller id (paging backwards)
	Limit    int   // 0 for DefaultMCPAuditLimit
}

func (f MCPAuditFilter) limit() int {
	if f.Limit <= 0 {
		return DefaultMCPAuditLimit
	}
	return min(f.Limit, MaxMCPAuditLimit)
}

// truncatePreview bounds s to MaxMCPAuditPreview bytes of valid UTF-8,
// marking a cut with an ellipsis.
func truncatePreview(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= MaxMCPAuditPreview {
		return s
	}
	const mark = "…"
	cut := MaxMCPAuditPreview - len(mark)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + mark
}

// InsertMCPAudit writes a batch in one transaction and drops rows older than
// MCPAuditRetention relative to now, so retention needs no scheduler.
func (s *SQLiteStore) InsertMCPAudit(batch []MCPAuditEntry, now time.Time) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin audit insert: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	stmt, err := tx.Prepare(`
		INSERT INTO mcp_audit (ts, token_id, token_name, tool, flow_id, run_id, args_sha256, args_preview, outcome, dur_ms, privileged)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare audit insert: %w", err)
	}
	defer stmt.Close()
	for _, e := range batch {
		if _, err := stmt.Exec(e.TS.UnixMilli(), e.TokenID, e.TokenName, e.Tool, e.FlowID, e.RunID, e.ArgsSHA256, truncatePreview(e.ArgsPreview), e.Outcome, e.DurMs, e.Privileged); err != nil {
			return fmt.Errorf("failed to insert audit entry: %w", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM mcp_audit WHERE ts < ?`, now.Add(-MCPAuditRetention).UnixMilli()); err != nil {
		return fmt.Errorf("failed to apply audit retention: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit audit entries: %w", err)
	}
	return nil
}

// ListMCPAudit returns matching audit rows, newest first.
func (s *SQLiteStore) ListMCPAudit(f MCPAuditFilter) ([]MCPAuditEntry, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	where, args := []string{"1 = 1"}, []any{}
	add := func(cond string, v any) {
		where = append(where, cond)
		args = append(args, v)
	}
	if f.TokenID != "" {
		add("token_id = ?", f.TokenID)
	}
	if f.Tool != "" {
		add("tool = ?", f.Tool)
	}
	if f.FlowID != "" {
		add("flow_id = ?", f.FlowID)
	}
	if f.Outcome != "" {
		add("outcome = ?", f.Outcome)
	}
	if !f.Since.IsZero() {
		add("ts >= ?", f.Since.UnixMilli())
	}
	if !f.Until.IsZero() {
		add("ts < ?", f.Until.UnixMilli())
	}
	if f.BeforeID > 0 {
		add("id < ?", f.BeforeID)
	}
	args = append(args, f.limit())
	rows, err := s.db.Query(`
		SELECT id, ts, token_id, token_name, tool, flow_id, run_id, args_sha256, args_preview, outcome, dur_ms, privileged
		FROM mcp_audit WHERE `+strings.Join(where, " AND ")+`
		ORDER BY ts DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list audit entries: %w", err)
	}
	defer rows.Close()
	out := []MCPAuditEntry{}
	for rows.Next() {
		var e MCPAuditEntry
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.TokenID, &e.TokenName, &e.Tool, &e.FlowID, &e.RunID, &e.ArgsSHA256, &e.ArgsPreview, &e.Outcome, &e.DurMs, &e.Privileged); err != nil {
			return nil, fmt.Errorf("failed to scan audit entry: %w", err)
		}
		e.TS = time.UnixMilli(ts).UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating audit entries: %w", err)
	}
	return out, nil
}

// PruneMCPAudit deletes rows and counters older than MCPAuditRetention and,
// when maxRows > 0, the oldest non-privileged rows beyond the newest maxRows.
// It returns the number of audit rows deleted.
func (s *SQLiteStore) PruneMCPAudit(now time.Time, maxRows int) (int64, error) {
	if s.isClosed() {
		return 0, ErrDatabaseClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin audit prune: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	cutoff := now.Add(-MCPAuditRetention)
	res, err := tx.Exec(`DELETE FROM mcp_audit WHERE ts < ?`, cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("failed to prune audit entries: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if maxRows > 0 {
		res, err := tx.Exec(`
			DELETE FROM mcp_audit
			WHERE privileged = 0 AND id NOT IN (SELECT id FROM mcp_audit ORDER BY ts DESC, id DESC LIMIT ?)
		`, maxRows)
		if err != nil {
			return 0, fmt.Errorf("failed to cap audit entries: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		deleted += n
	}
	if _, err := tx.Exec(`DELETE FROM mcp_rejections WHERE minute < ?`, cutoff.Unix()); err != nil {
		return 0, fmt.Errorf("failed to prune rejection counters: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit audit prune: %w", err)
	}
	return deleted, nil
}

// AddMCPRejections adds counts to the per-token per-minute counters in one
// transaction (non-positive counts are ignored) and drops counters older than
// MCPAuditRetention relative to now.
func (s *SQLiteStore) AddMCPRejections(batch []MCPRejection, now time.Time) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin rejection update: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	stmt, err := tx.Prepare(`
		INSERT INTO mcp_rejections (token_id, minute, reason, count) VALUES (?, ?, ?, ?)
		ON CONFLICT(token_id, minute, reason) DO UPDATE SET count = count + excluded.count
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare rejection update: %w", err)
	}
	defer stmt.Close()
	for _, r := range batch {
		if r.Count <= 0 {
			continue
		}
		if _, err := stmt.Exec(r.TokenID, r.At.Truncate(time.Minute).Unix(), r.Reason, r.Count); err != nil {
			return fmt.Errorf("failed to add rejection count: %w", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM mcp_rejections WHERE minute < ?`, now.Add(-MCPAuditRetention).Unix()); err != nil {
		return fmt.Errorf("failed to apply rejection retention: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit rejection counts: %w", err)
	}
	return nil
}

// ListMCPRejections returns counters, newest minute first. Only TokenID,
// Since, Until and Limit of the filter apply.
func (s *SQLiteStore) ListMCPRejections(f MCPAuditFilter) ([]MCPRejection, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	where, args := []string{"1 = 1"}, []any{}
	if f.TokenID != "" {
		where, args = append(where, "token_id = ?"), append(args, f.TokenID)
	}
	if !f.Since.IsZero() {
		where, args = append(where, "minute >= ?"), append(args, f.Since.Truncate(time.Minute).Unix())
	}
	if !f.Until.IsZero() {
		where, args = append(where, "minute < ?"), append(args, f.Until.Unix())
	}
	args = append(args, f.limit())
	rows, err := s.db.Query(`
		SELECT token_id, minute, reason, count FROM mcp_rejections
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY minute DESC, token_id, reason LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list rejection counters: %w", err)
	}
	defer rows.Close()
	out := []MCPRejection{}
	for rows.Next() {
		var r MCPRejection
		var minute int64
		if err := rows.Scan(&r.TokenID, &minute, &r.Reason, &r.Count); err != nil {
			return nil, fmt.Errorf("failed to scan rejection counter: %w", err)
		}
		r.At = time.Unix(minute, 0).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rejection counters: %w", err)
	}
	return out, nil
}

const mcpAuditSchema = `
	CREATE TABLE IF NOT EXISTS mcp_audit (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		ts           INTEGER NOT NULL,
		token_id     TEXT NOT NULL DEFAULT '',
		token_name   TEXT NOT NULL DEFAULT '',
		tool         TEXT NOT NULL DEFAULT '',
		flow_id      TEXT NOT NULL DEFAULT '',
		run_id       TEXT NOT NULL DEFAULT '',
		args_sha256  TEXT NOT NULL DEFAULT '',
		args_preview TEXT NOT NULL DEFAULT '',
		outcome      TEXT NOT NULL DEFAULT '',
		dur_ms       INTEGER NOT NULL DEFAULT 0,
		privileged   INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_mcp_audit_ts ON mcp_audit(ts);
	CREATE INDEX IF NOT EXISTS idx_mcp_audit_token ON mcp_audit(token_id, ts);

	CREATE TABLE IF NOT EXISTS mcp_rejections (
		token_id TEXT NOT NULL,
		minute   INTEGER NOT NULL,
		reason   TEXT NOT NULL,
		count    INTEGER NOT NULL,
		PRIMARY KEY (token_id, minute, reason)
	);
	CREATE INDEX IF NOT EXISTS idx_mcp_rejections_minute ON mcp_rejections(minute);
`
