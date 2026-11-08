package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
)

// ApprovalList is the GET /api/agent/approvals payload. Pending counts the
// approvals still awaiting a decision (the editor badge), whatever the filter.
type ApprovalList struct {
	Approvals []storage.AgentApproval `json:"approvals"`
	Pending   int                     `json:"pending"`
}

// ApprovalDecision is the payload of approve/reject. Executed reports whether
// the ApprovalExecutor ran the approved request; ExecutionError says why not.
type ApprovalDecision struct {
	Approval       storage.AgentApproval `json:"approval"`
	Executed       bool                  `json:"executed"`
	ExecutionError string                `json:"executionError,omitempty"`
}

// ListApprovals expires stale approvals, then lists them newest first.
// GET /api/agent/approvals?status=pending|approved|rejected|expired|all&limit=N
func (h *AgentHandler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	switch status {
	case "all":
		status = ""
	case "", storage.ApprovalPending, storage.ApprovalApproved, storage.ApprovalRejected, storage.ApprovalExpired:
	default:
		writeError(w, http.StatusBadRequest, "Invalid status filter")
		return
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "Invalid limit")
			return
		}
		limit = n
	}
	now := h.now()
	if _, err := h.store.ExpireAgentApprovals(now); err != nil {
		h.logger.Error().Err(err).Msg("Failed to expire agent approvals")
		writeError(w, http.StatusInternalServerError, "Cannot read approvals")
		return
	}
	list, err := h.store.ListAgentApprovals(storage.AgentApprovalFilter{Status: status, Limit: limit})
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list agent approvals")
		writeError(w, http.StatusInternalServerError, "Cannot read approvals")
		return
	}
	pending, err := h.store.CountPendingAgentApprovals(now)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to count agent approvals")
		writeError(w, http.StatusInternalServerError, "Cannot read approvals")
		return
	}
	writeSuccess(w, http.StatusOK, ApprovalList{Approvals: list, Pending: pending}, "")
}

// Approve records the approval, then asks the ApprovalExecutor to run it.
// POST /api/agent/approvals/{id}/approve
func (h *AgentHandler) Approve(w http.ResponseWriter, r *http.Request) { h.decide(w, r, true) }

// Reject records the rejection; nothing runs.
// POST /api/agent/approvals/{id}/reject
func (h *AgentHandler) Reject(w http.ResponseWriter, r *http.Request) { h.decide(w, r, false) }

func (h *AgentHandler) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	id := mux.Vars(r)["id"]
	by := sessionUser(r)
	a, err := h.store.DecideAgentApproval(id, approve, by, h.now())
	switch {
	case errors.Is(err, storage.ErrAgentApprovalNotFound):
		writeError(w, http.StatusNotFound, "Approval not found")
		return
	case errors.Is(err, storage.ErrAgentApprovalExpired):
		writeError(w, http.StatusConflict, "This approval has expired")
		return
	case errors.Is(err, storage.ErrAgentApprovalDecided):
		writeError(w, http.StatusConflict, "This approval was already decided")
		return
	case err != nil:
		h.logger.Error().Err(err).Str("approvalId", id).Msg("Failed to record approval decision")
		writeError(w, http.StatusInternalServerError, "Cannot record the decision")
		return
	}
	h.logger.Info().Str("approvalId", a.ID).Str("kind", a.Kind).Str("tokenId", a.TokenID).
		Str("status", a.Status).Str("by", by).Msg("Agent approval decided")
	result := ApprovalDecision{Approval: *a}
	if approve {
		// The decision is final and recorded; the approved work must not be
		// cut short because the admin's browser went away.
		if err := h.executor.ExecuteApproval(context.WithoutCancel(r.Context()), *a); err != nil {
			h.logger.Warn().Err(err).Str("approvalId", a.ID).Msg("Approved agent request was not executed")
			result.ExecutionError = err.Error()
		} else {
			result.Executed = true
		}
	}
	writeSuccess(w, http.StatusOK, result, "")
}
