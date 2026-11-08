package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
)

// ApprovalView is an approval as shown in the editor. TokenStatus is the
// requesting token's state now (active, expired, revoked or unknown): only a
// request from an active token can be approved.
type ApprovalView struct {
	storage.AgentApproval
	TokenStatus string `json:"tokenStatus"`
}

func approvalView(a storage.AgentApproval, now time.Time) ApprovalView {
	status := AgentTokenActive
	switch {
	case a.TokenExpiresAt == nil:
		status = AgentTokenUnknown
	case a.TokenRevokedAt != nil:
		status = AgentTokenRevoked
	case !now.Before(*a.TokenExpiresAt):
		status = AgentTokenExpired
	}
	return ApprovalView{AgentApproval: a, TokenStatus: status}
}

// ApprovalList is the GET /api/agent/approvals payload. Pending counts the
// approvals still awaiting a decision (the editor badge), whatever the filter.
type ApprovalList struct {
	Approvals []ApprovalView `json:"approvals"`
	Pending   int            `json:"pending"`
}

// ApprovalDecision is the payload of approve/reject. Executed reports whether
// the ApprovalExecutor ran the approved request; ExecutionError says why not.
type ApprovalDecision struct {
	Approval       ApprovalView `json:"approval"`
	Executed       bool         `json:"executed"`
	ExecutionError string       `json:"executionError,omitempty"`
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
	views := make([]ApprovalView, len(list))
	for i, a := range list {
		views[i] = approvalView(a, now)
	}
	writeSuccess(w, http.StatusOK, ApprovalList{Approvals: views, Pending: pending}, "")
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
	now := h.now()
	a, err := h.store.DecideAgentApproval(id, approve, by, now)
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
	case errors.Is(err, storage.ErrAgentApprovalTokenRevoked):
		h.logger.Warn().Str("approvalId", id).Str("by", by).Msg("Refused to approve a request from a revoked or expired agent token")
		writeError(w, http.StatusConflict, "The token that asked for this was revoked or has expired, so it was rejected")
		return
	case err != nil:
		h.logger.Error().Err(err).Str("approvalId", id).Msg("Failed to record approval decision")
		writeError(w, http.StatusInternalServerError, "Cannot record the decision")
		return
	}
	h.logger.Info().Str("approvalId", a.ID).Str("kind", a.Kind).Str("tokenId", a.TokenID).
		Str("status", a.Status).Str("by", by).Msg("Agent approval decided")
	result := ApprovalDecision{Approval: approvalView(*a, now)}
	if approve {
		// Running an approved tool call waits for it (up to its timeout),
		// longer than the server's default write timeout.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(toolCallWriteDeadline))
		// The decision is final and recorded; the approved work must not be
		// cut short because the admin's browser went away.
		if err := h.executor.ExecuteApproval(context.WithoutCancel(r.Context()), *a); err != nil {
			h.logger.Warn().Err(err).Str("approvalId", a.ID).Msg("Approved agent request was not executed")
			result.ExecutionError = err.Error()
		} else {
			result.Executed = true
		}
		// Show what the executor stored (a tool call's result).
		if updated, err := h.store.GetAgentApproval(a.ID); err == nil {
			result.Approval = approvalView(*updated, now)
		}
	}
	writeSuccess(w, http.StatusOK, result, "")
}
