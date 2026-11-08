package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
)

// MaxAgentTokenDays bounds expiresInDays on token creation.
const MaxAgentTokenDays = int(storage.MaxAPITokenLifetime / (24 * time.Hour))

// Token statuses reported to the editor.
const (
	AgentTokenActive  = "active"
	AgentTokenExpired = "expired"
	AgentTokenRevoked = "revoked"
)

// AgentTokenView is a stored token as listed in the editor (never the secret
// or its hash).
type AgentTokenView struct {
	storage.APIToken
	Status string `json:"status"`
}

// AgentTokenList is the GET /api/agent/tokens payload.
type AgentTokenList struct {
	Tokens []AgentTokenView `json:"tokens"`
}

// CreatedAgentToken is the POST /api/agent/tokens payload. Token is the
// plaintext, returned only here.
type CreatedAgentToken struct {
	Token string           `json:"token"`
	Info  storage.APIToken `json:"info"`
}

type createAgentTokenRequest struct {
	Name          string               `json:"name"`
	Scopes        []string             `json:"scopes"`
	Privileged    bool                 `json:"privileged"`
	AllTools      bool                 `json:"allTools"`
	Grants        []storage.TokenGrant `json:"grants"`
	ExpiresInDays int                  `json:"expiresInDays"`
	Password      string               `json:"password"`
}

// Grant actions for POST /api/agent/tokens/{id}/grants.
const (
	GrantConfirm = "confirm" // accept the tool's current fingerprint and resume
	GrantSuspend = "suspend"
	GrantRemove  = "remove"
	GrantAdd     = "add" // widens the token: needs password step-up
)

type grantUpdateRequest struct {
	Action        string `json:"action"`
	FlowID        string `json:"flowId"`
	TriggerNodeID string `json:"triggerNodeId"`
	Fingerprint   string `json:"fingerprint"`
	Password      string `json:"password"`
}

func agentTokenView(t storage.APIToken, now time.Time) AgentTokenView {
	status := AgentTokenActive
	switch {
	case t.RevokedAt != nil:
		status = AgentTokenRevoked
	case !now.Before(t.ExpiresAt):
		status = AgentTokenExpired
	}
	return AgentTokenView{APIToken: t, Status: status}
}

// validationMessage turns a wrapped validation error into an editor message.
func validationMessage(err, sentinel error) string {
	return "Invalid request: " + strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}

// ListTokens returns every token, newest first.
// GET /api/agent/tokens
func (h *AgentHandler) ListTokens(w http.ResponseWriter, _ *http.Request) {
	list, err := h.store.ListAPITokens()
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list agent tokens")
		writeError(w, http.StatusInternalServerError, "Cannot read agent tokens")
		return
	}
	now := h.now()
	views := make([]AgentTokenView, len(list))
	for i, t := range list {
		views[i] = agentTokenView(t, now)
	}
	writeSuccess(w, http.StatusOK, AgentTokenList{Tokens: views}, "")
}

// CreateToken creates a token after password step-up and returns its
// plaintext once.
// POST /api/agent/tokens
func (h *AgentHandler) CreateToken(w http.ResponseWriter, r *http.Request) {
	var req createAgentTokenRequest
	if !decodeAgentBody(w, r, &req) {
		return
	}
	name, err := storage.NormalizeAPITokenName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, validationMessage(err, storage.ErrInvalidAPITokenRequest))
		return
	}
	scopes, err := storage.NormalizeScopes(req.Scopes)
	if err != nil {
		writeError(w, http.StatusBadRequest, validationMessage(err, storage.ErrInvalidAPITokenRequest))
		return
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > MaxAgentTokenDays {
		writeError(w, http.StatusBadRequest, "Invalid request: expiresInDays must be between 1 and 365 (0 for the 90-day default)")
		return
	}
	grants := make([]storage.TokenGrant, len(req.Grants))
	for i, g := range req.Grants {
		g.Suspended = false
		grants[i] = g
	}
	if err := storage.ValidateGrants(grants); err != nil {
		writeError(w, http.StatusBadRequest, validationMessage(err, storage.ErrInvalidAPITokenRequest))
		return
	}
	if !h.scopesAllowed(w, scopes) {
		return
	}
	if !h.auth.verifyStepUp(w, r, req.Password) {
		return
	}
	now := h.now()
	var expires time.Time
	if req.ExpiresInDays > 0 {
		expires = now.Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
	}
	tok, raw, err := h.store.CreateAPIToken(storage.NewAPIToken{
		Name:       name,
		Scopes:     scopes,
		Privileged: req.Privileged,
		Grants:     grants,
		AllTools:   req.AllTools,
		ExpiresAt:  expires,
	}, now)
	if errors.Is(err, storage.ErrInvalidAPITokenRequest) {
		writeError(w, http.StatusBadRequest, validationMessage(err, storage.ErrInvalidAPITokenRequest))
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to create agent token")
		writeError(w, http.StatusInternalServerError, "Cannot create agent token")
		return
	}
	h.logger.Info().Str("tokenId", tok.ID).Str("name", tok.Name).Strs("scopes", tok.Scopes).
		Bool("privileged", tok.Privileged).Bool("allTools", tok.AllTools).Str("by", sessionUser(r)).
		Msg("Agent token created")
	writeSuccess(w, http.StatusCreated, CreatedAgentToken{Token: raw, Info: *tok}, "")
}

// scopesAllowed refuses scopes the current configuration does not permit.
func (h *AgentHandler) scopesAllowed(w http.ResponseWriter, scopes []string) bool {
	if slices.Contains(scopes, storage.ScopeAuthor) && !h.authorEnabled {
		writeError(w, http.StatusForbidden, "The author scope requires mcp.author_enabled = true in the configuration")
		return false
	}
	if slices.Contains(scopes, storage.ScopeDeploy) {
		level, err := h.store.AgentDeployLevel()
		if err != nil {
			h.logger.Error().Err(err).Msg("Failed to read agent deploy level")
			writeError(w, http.StatusInternalServerError, "Cannot read the agent deploy setting")
			return false
		}
		if level == storage.AgentDeployOff {
			writeError(w, http.StatusForbidden, "The deploy scope requires agent deploy to be enabled in Settings")
			return false
		}
	}
	return true
}

// RevokeToken revokes a token; it stops working immediately.
// DELETE /api/agent/tokens/{id}
func (h *AgentHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	err := h.tokens.Revoke(id)
	if errors.Is(err, storage.ErrAPITokenNotFound) {
		writeError(w, http.StatusNotFound, "Token not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Str("tokenId", id).Msg("Failed to revoke agent token")
		writeError(w, http.StatusInternalServerError, "Cannot revoke agent token")
		return
	}
	h.logger.Info().Str("tokenId", id).Str("by", sessionUser(r)).Msg("Agent token revoked")
	writeSuccess(w, http.StatusOK, nil, "Token revoked")
}

// UpdateGrant confirms, suspends, removes or adds one grant of a token.
// POST /api/agent/tokens/{id}/grants
func (h *AgentHandler) UpdateGrant(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req grantUpdateRequest
	if !decodeAgentBody(w, r, &req) {
		return
	}
	switch req.Action {
	case GrantConfirm, GrantSuspend, GrantRemove, GrantAdd:
	default:
		writeError(w, http.StatusBadRequest, "Invalid request: action must be confirm, suspend, remove or add")
		return
	}
	if req.FlowID == "" || req.TriggerNodeID == "" {
		writeError(w, http.StatusBadRequest, "Invalid request: flowId and triggerNodeId are required")
		return
	}
	if (req.Action == GrantConfirm || req.Action == GrantAdd) && req.Fingerprint == "" {
		writeError(w, http.StatusBadRequest, "Invalid request: fingerprint is required")
		return
	}
	tok, err := h.store.GetAPIToken(id)
	if errors.Is(err, storage.ErrAPITokenNotFound) || (err == nil && tok.RevokedAt != nil) {
		writeError(w, http.StatusNotFound, "Token not found or revoked")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Str("tokenId", id).Msg("Failed to read agent token")
		writeError(w, http.StatusInternalServerError, "Cannot read agent token")
		return
	}
	grants := slices.Clone(tok.Grants)
	idx := slices.IndexFunc(grants, func(g storage.TokenGrant) bool {
		return g.FlowID == req.FlowID && g.TriggerNodeID == req.TriggerNodeID
	})
	if req.Action == GrantAdd {
		if idx >= 0 {
			writeError(w, http.StatusConflict, "The token already has a grant for this tool")
			return
		}
		if !h.auth.verifyStepUp(w, r, req.Password) {
			return
		}
		grants = append(grants, storage.TokenGrant{FlowID: req.FlowID, TriggerNodeID: req.TriggerNodeID, Fingerprint: req.Fingerprint})
	} else {
		if idx < 0 {
			writeError(w, http.StatusNotFound, "Grant not found")
			return
		}
		switch req.Action {
		case GrantConfirm:
			grants[idx].Fingerprint, grants[idx].Suspended = req.Fingerprint, false
		case GrantSuspend:
			grants[idx].Suspended = true
		case GrantRemove:
			grants = slices.Delete(grants, idx, idx+1)
		}
	}
	err = h.store.SetAPITokenGrants(id, grants)
	h.tokens.Evict(id)
	switch {
	case errors.Is(err, storage.ErrAPITokenNotFound):
		writeError(w, http.StatusNotFound, "Token not found or revoked")
		return
	case errors.Is(err, storage.ErrInvalidAPITokenRequest):
		writeError(w, http.StatusBadRequest, validationMessage(err, storage.ErrInvalidAPITokenRequest))
		return
	case err != nil:
		h.logger.Error().Err(err).Str("tokenId", id).Msg("Failed to update agent token grants")
		writeError(w, http.StatusInternalServerError, "Cannot update grants")
		return
	}
	h.logger.Info().Str("tokenId", id).Str("action", req.Action).Str("flowId", req.FlowID).
		Str("triggerNodeId", req.TriggerNodeID).Str("by", sessionUser(r)).Msg("Agent token grant updated")
	tok.Grants = grants
	writeSuccess(w, http.StatusOK, agentTokenView(*tok, h.now()), "")
}
