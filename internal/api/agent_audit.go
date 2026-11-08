package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
)

// AuditPage is the GET /api/agent/audit payload.
type AuditPage struct {
	Entries    []storage.MCPAuditEntry `json:"entries"`
	Rejections []storage.MCPRejection  `json:"rejections"`
}

// ListAudit returns audited agent calls (newest first) and rejection
// counters. Filters: tokenId, tool, flowId, outcome, since and until
// (RFC 3339; since inclusive, until exclusive), beforeId (paging) and limit.
// GET /api/agent/audit
func (h *AgentHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := storage.MCPAuditFilter{
		TokenID: q.Get("tokenId"),
		Tool:    q.Get("tool"),
		FlowID:  q.Get("flowId"),
		Outcome: q.Get("outcome"),
	}
	for name, dst := range map[string]*time.Time{"since": &f.Since, "until": &f.Until} {
		if v := q.Get(name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				writeError(w, http.StatusBadRequest, "Invalid "+name+": use RFC 3339, e.g. 2026-11-08T12:00:00Z")
				return
			}
			*dst = t
		}
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
