package storage

import (
	"fmt"
	"strings"
	"time"
)

// MaxMCPRejectionTotals bounds the (token, reason) groups MCPRejectionTotals
// returns; the largest totals are kept.
const MaxMCPRejectionTotals = 500

// MCPRejectionTotal is the number of rejected calls of one token for one
// reason over a time window. TokenName is empty when the token no longer
// exists; Last is the latest minute with a rejection.
type MCPRejectionTotal struct {
	TokenID   string    `json:"tokenId"`
	TokenName string    `json:"tokenName"`
	Reason    string    `json:"reason"`
	Count     int64     `json:"count"`
	Last      time.Time `json:"last"`
}

// MCPRejectionFilter selects the counters summed by MCPRejectionTotals.
// Empty fields match everything; Since is inclusive (of its whole minute)
// and Until exclusive, as for ListMCPRejections.
type MCPRejectionFilter struct {
	TokenID string
	Since   time.Time
	Until   time.Time
}

// MCPRejectionTotals sums the per-minute rejection counters per token and
// reason in SQL, largest first, and reports whether more than
// MaxMCPRejectionTotals groups matched (only the largest are returned).
func (s *SQLiteStore) MCPRejectionTotals(f MCPRejectionFilter) ([]MCPRejectionTotal, bool, error) {
	if s.isClosed() {
		return nil, false, ErrDatabaseClosed
	}
	where, args := []string{"1 = 1"}, []any{}
	if f.TokenID != "" {
		where, args = append(where, "r.token_id = ?"), append(args, f.TokenID)
	}
	if !f.Since.IsZero() {
		where, args = append(where, "r.minute >= ?"), append(args, f.Since.Truncate(time.Minute).Unix())
	}
	if !f.Until.IsZero() {
		where, args = append(where, "r.minute < ?"), append(args, f.Until.Unix())
	}
	args = append(args, MaxMCPRejectionTotals+1)
	rows, err := s.db.Query(`
		SELECT r.token_id, COALESCE(t.name, ''), r.reason, SUM(r.count) AS total, MAX(r.minute)
		FROM mcp_rejections r LEFT JOIN api_tokens t ON t.id = r.token_id
		WHERE `+strings.Join(where, " AND ")+`
		GROUP BY r.token_id, r.reason
		ORDER BY total DESC, r.token_id, r.reason
		LIMIT ?`, args...)
	if err != nil {
		return nil, false, fmt.Errorf("failed to sum rejection counters: %w", err)
	}
	defer rows.Close()
	out := []MCPRejectionTotal{}
	for rows.Next() {
		var r MCPRejectionTotal
		var last int64
		if err := rows.Scan(&r.TokenID, &r.TokenName, &r.Reason, &r.Count, &last); err != nil {
			return nil, false, fmt.Errorf("failed to scan rejection total: %w", err)
		}
		r.Last = time.Unix(last, 0).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("error iterating rejection totals: %w", err)
	}
	truncated := len(out) > MaxMCPRejectionTotals
	if truncated {
		out = out[:MaxMCPRejectionTotals]
	}
	return out, truncated, nil
}
