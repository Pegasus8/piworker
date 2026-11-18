package api

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
)

// AuditPage is the GET /api/agent/audit payload.
type AuditPage struct {
	Entries    []storage.MCPAuditEntry `json:"entries"`
	Rejections []storage.MCPRejection  `json:"rejections"`
}

// ListAudit returns audited agent calls (most recently recorded first) and
// rejection counters. Filters: tokenId, tool, flowId, outcome, since and until
// (RFC 3339 on the call time; since inclusive, until exclusive), beforeId
// (paging: pass the id of the last entry of the previous page) and limit.
// GET /api/agent/audit
func (h *AgentHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := storage.MCPAuditFilter{
		TokenID: q.Get("tokenId"),
		Tool:    q.Get("tool"),
		FlowID:  q.Get("flowId"),
		Outcome: q.Get("outcome"),
	}
	if !parseAuditWindow(w, q, &f.Since, &f.Until) {
		return
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "Invalid limit")
			return
		}
		f.Limit = n
	}
	if v := q.Get("beforeId"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "Invalid beforeId")
			return
		}
		f.BeforeID = n
	}
	entries, err := h.store.ListMCPAudit(f)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list MCP audit")
		writeError(w, http.StatusInternalServerError, "Cannot read the audit log")
		return
	}
	rejections, err := h.store.ListMCPRejections(f)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list MCP rejections")
		writeError(w, http.StatusInternalServerError, "Cannot read the audit log")
		return
	}
	writeSuccess(w, http.StatusOK, AuditPage{Entries: entries, Rejections: rejections}, "")
}

// parseAuditWindow reads the since and until query parameters (RFC 3339),
// answering 400 when one is malformed.
func parseAuditWindow(w http.ResponseWriter, q url.Values, since, until *time.Time) bool {
	for name, dst := range map[string]*time.Time{"since": since, "until": until} {
		if v := q.Get(name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				writeError(w, http.StatusBadRequest, "Invalid "+name+": use RFC 3339, e.g. 2026-11-08T12:00:00Z")
				return false
			}
			*dst = t
		}
	}
	return true
}

// RejectionTotals is the GET /api/agent/audit/rejections payload. Truncated
// reports that only the storage.MaxMCPRejectionTotals largest totals are
// listed.
type RejectionTotals struct {
	Totals    []storage.MCPRejectionTotal `json:"totals"`
	Truncated bool                        `json:"truncated"`
}

// ListRejectionTotals sums the rejected calls (busy, cooldown, invalid
// arguments…) per token and reason over a window. Filters: tokenId, since
// (inclusive, of its whole minute) and until (exclusive), RFC 3339.
// GET /api/agent/audit/rejections
func (h *AgentHandler) ListRejectionTotals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := storage.MCPRejectionFilter{TokenID: q.Get("tokenId")}
	if !parseAuditWindow(w, q, &f.Since, &f.Until) {
		return
	}
	totals, truncated, err := h.store.MCPRejectionTotals(f)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to sum MCP rejections")
		writeError(w, http.StatusInternalServerError, "Cannot read the audit log")
		return
	}
	writeSuccess(w, http.StatusOK, RejectionTotals{Totals: totals, Truncated: truncated}, "")
}
