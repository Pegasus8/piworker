package api

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"strconv"

	"github.com/Pegasus8/piworker/internal/service"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// The editor's side of agent drafts, flow versions and rollback
// (docs/design/mcp.md §10): thin adapters over service.Authoring, called as
// the editor. Every route sits under /api, so it needs the cookie session
// and, for writes, the CSRF header. Approving and rejecting need the session
// only (ADR-0017: they apply or refuse what the admin reviewed, widening no
// agent access).

// DraftService is the editor's side of drafts and versions
// (*service.Authoring).
type DraftService interface {
	ListDrafts(ctx context.Context, actor service.Actor, q service.DraftQuery) (service.DraftList, error)
	GetDraftDetail(ctx context.Context, actor service.Actor, draftID string) (service.DraftDetail, error)
	ApproveDraft(ctx context.Context, actor service.Actor, draftID string, deploy bool) (service.ApplyResult, error)
	RejectDraft(ctx context.Context, actor service.Actor, draftID, reason string) error
	Rollback(ctx context.Context, actor service.Actor, flowID string) (service.ApplyResult, error)
	Versions(ctx context.Context, actor service.Actor, flowID string, limit int) (service.VersionList, error)
}

// DraftsHandler serves /api/agent/drafts* and /api/flows/{id}/versions and
// /rollback.
type DraftsHandler struct {
	svc    DraftService
	logger zerolog.Logger
}

// NewDraftsHandler returns the drafts and versions handler.
func NewDraftsHandler(svc DraftService, logger zerolog.Logger) *DraftsHandler {
	return &DraftsHandler{svc: svc, logger: logger}
}

// RegisterRoutes registers the drafts routes (they exist only with
// authentication, like every /api/agent route).
func (h *DraftsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/agent/drafts", h.List).Methods(http.MethodGet)
	r.HandleFunc("/api/agent/drafts/{id}", h.Get).Methods(http.MethodGet)
	r.HandleFunc("/api/agent/drafts/{id}/approve", h.Approve).Methods(http.MethodPost)
	r.HandleFunc("/api/agent/drafts/{id}/reject", h.Reject).Methods(http.MethodPost)
	h.RegisterVersionRoutes(r)
}

// RegisterVersionRoutes registers the version history and rollback routes.
func (h *DraftsHandler) RegisterVersionRoutes(r *mux.Router) {
	r.HandleFunc("/api/flows/{id}/versions", h.Versions).Methods(http.MethodGet)
	r.HandleFunc("/api/flows/{id}/rollback", h.Rollback).Methods(http.MethodPost)
}

// queryLimit reads an optional positive limit (0 when absent).
func queryLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "Invalid limit")
		return 0, false
	}
	return n, true
}

// decodeOptionalBody is decodeAgentBody for a body that may be absent.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, dst, json.RejectUnknownMembers(true)); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// List lists drafts, newest first.
// GET /api/agent/drafts?status=pending|decided|all&limit=&beforeId=
func (h *DraftsHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryLimit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	list, err := h.svc.ListDrafts(r.Context(), service.EditorActor(), service.DraftQuery{
		Status: q.Get("status"), Limit: limit, BeforeID: q.Get("beforeId"),
	})
	if err != nil {
		writeServiceError(w, h.logger, err, "Cannot list drafts")
		return
	}
	writeSuccess(w, http.StatusOK, list, "")
}

// Get shows one draft with its diff and alerts.
// GET /api/agent/drafts/{id}
func (h *DraftsHandler) Get(w http.ResponseWriter, r *http.Request) {
	detail, err := h.svc.GetDraftDetail(r.Context(), service.EditorActor(), mux.Vars(r)["id"])
	if err != nil {
		writeServiceError(w, h.logger, err, "Cannot read the draft")
		return
	}
	writeSuccess(w, http.StatusOK, detail, "")
}

// ApproveDraftRequest is the body of an approval: whether to deploy the
// saved version (a running flow is redeployed whatever it says).
type ApproveDraftRequest struct {
	Deploy bool `json:"deploy"`
}

// Approve saves a pending draft as a new admin-approved version and, when
// asked or when the flow runs, deploys it.
// POST /api/agent/drafts/{id}/approve {"deploy": bool}
func (h *DraftsHandler) Approve(w http.ResponseWriter, r *http.Request) {
	var req ApproveDraftRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	id := mux.Vars(r)["id"]
	// Approving may deploy; the decision must not be cut short because the
	// admin's browser went away once the save started.
	res, err := h.svc.ApproveDraft(context.WithoutCancel(r.Context()), service.EditorActor(), id, req.Deploy)
	if err != nil {
		writeServiceError(w, h.logger, err, "Cannot approve the draft")
		return
	}
	h.logger.Info().Str("draftId", id).Str("by", sessionUser(r)).Str("flowID", res.FlowID).
		Int("version", res.Version).Bool("deployed", res.Deployed).Msg("Agent draft approved")
	writeSuccess(w, http.StatusOK, res, "")
}

// RejectDraftRequest is the body of a rejection: the reason shown to the
// agent.
type RejectDraftRequest struct {
	Reason string `json:"reason,omitempty"`
}

// Reject rejects a pending draft.
// POST /api/agent/drafts/{id}/reject {"reason"?}
func (h *DraftsHandler) Reject(w http.ResponseWriter, r *http.Request) {
	var req RejectDraftRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	id := mux.Vars(r)["id"]
	if err := h.svc.RejectDraft(r.Context(), service.EditorActor(), id, req.Reason); err != nil {
		writeServiceError(w, h.logger, err, "Cannot reject the draft")
		return
	}
	h.logger.Info().Str("draftId", id).Str("by", sessionUser(r)).Msg("Agent draft rejected")
	writeSuccess(w, http.StatusOK, struct{}{}, "")
}

// Versions lists a flow's versions, newest first.
// GET /api/flows/{id}/versions?limit=
func (h *DraftsHandler) Versions(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryLimit(w, r)
	if !ok {
		return
	}
	list, err := h.svc.Versions(r.Context(), service.EditorActor(), mux.Vars(r)["id"], limit)
	if err != nil {
		writeServiceError(w, h.logger, err, "Cannot list the flow's versions")
		return
	}
	writeSuccess(w, http.StatusOK, list, "")
}

// Rollback restores the version the admin last approved as a new version,
// redeploying a running flow.
// POST /api/flows/{id}/rollback
func (h *DraftsHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	res, err := h.svc.Rollback(context.WithoutCancel(r.Context()), service.EditorActor(), id)
	if err != nil {
		writeServiceError(w, h.logger, err, "Cannot roll the flow back")
		return
	}
	h.logger.Info().Str("flowID", id).Str("by", sessionUser(r)).Int("version", res.Version).
		Bool("deployed", res.Deployed).Msg("Flow rolled back to the version the admin last approved")
	writeSuccess(w, http.StatusOK, res, "")
}
