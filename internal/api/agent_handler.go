package api

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// Editor-facing agent administration (docs/design/mcp.md §7, §9, §10): agent
// tokens, the approval queue, the MCP audit log and the agent deploy level.
// Every route sits under /api, so it needs the cookie session and the CSRF
// header; agent tokens are never accepted here, so no token can manage
// tokens, approvals or settings.

// ErrApprovalExecutionUnavailable is returned by the default ApprovalExecutor.
var ErrApprovalExecutionUnavailable = errors.New("not available")

// ApprovalExecutor runs what an approved request asked for (a tool call or a
// draft deploy). It is called after the decision is recorded; the default
// returns ErrApprovalExecutionUnavailable until the MCP service is wired in.
type ApprovalExecutor interface {
	ExecuteApproval(ctx context.Context, approval storage.AgentApproval) error
}

type unavailableExecutor struct{}

func (unavailableExecutor) ExecuteApproval(context.Context, storage.AgentApproval) error {
	return ErrApprovalExecutionUnavailable
}

// AgentHandlerOptions configures an AgentHandler.
type AgentHandlerOptions struct {
	// AuthorEnabled mirrors mcp.author_enabled: only then can tokens get the
	// author scope.
	AuthorEnabled bool
	// Executor runs approved requests (default: unavailable).
	Executor ApprovalExecutor
	// Now is the clock (default time.Now).
	Now func() time.Time
	// Logger reports internal failures.
	Logger zerolog.Logger
}

// AgentHandler serves /api/agent/*.
type AgentHandler struct {
	store         *storage.SQLiteStore
	auth          *AuthHandler
	tokens        *agentauth.TokenVerifier
	authorEnabled bool
	executor      ApprovalExecutor
	now           func() time.Time
	logger        zerolog.Logger
}

// NewAgentHandler returns the agent administration handler. auth provides the
// password step-up and its limiter; tokens applies revocations and grant
// changes to the verifier cache immediately.
func NewAgentHandler(store *storage.SQLiteStore, auth *AuthHandler, tokens *agentauth.TokenVerifier, opts AgentHandlerOptions) *AgentHandler {
	h := &AgentHandler{
		store:         store,
		auth:          auth,
		tokens:        tokens,
		authorEnabled: opts.AuthorEnabled,
		executor:      opts.Executor,
		now:           opts.Now,
		logger:        opts.Logger,
	}
	if h.executor == nil {
		h.executor = unavailableExecutor{}
	}
	if h.now == nil {
		h.now = time.Now
	}
	return h
}

// RegisterRoutes registers the agent administration routes.
func (h *AgentHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/agent/tokens", h.ListTokens).Methods(http.MethodGet)
	r.HandleFunc("/api/agent/tokens", h.CreateToken).Methods(http.MethodPost)
	r.HandleFunc("/api/agent/tokens/revoke-all", h.RevokeAllTokens).Methods(http.MethodPost)
	r.HandleFunc("/api/agent/tokens/{id}", h.RevokeToken).Methods(http.MethodDelete)
	r.HandleFunc("/api/agent/tokens/{id}/grants", h.UpdateGrant).Methods(http.MethodPost)
	r.HandleFunc("/api/agent/audit", h.ListAudit).Methods(http.MethodGet)
	r.HandleFunc("/api/agent/approvals", h.ListApprovals).Methods(http.MethodGet)
	r.HandleFunc(PendingApprovalsCountPath, h.PendingCount).Methods(http.MethodGet)
	r.HandleFunc("/api/agent/approvals/{id}/approve", h.Approve).Methods(http.MethodPost)
	r.HandleFunc("/api/agent/approvals/{id}/reject", h.Reject).Methods(http.MethodPost)
	r.HandleFunc("/api/agent/settings", h.GetSettings).Methods(http.MethodGet)
	r.HandleFunc("/api/agent/settings", h.PutSettings).Methods(http.MethodPut)
}

// decodeAgentBody strictly decodes a bounded JSON body: unknown or mistyped
// members are errors, so a misspelled security field is never ignored.
func decodeAgentBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	if err := json.UnmarshalRead(r.Body, dst, json.RejectUnknownMembers(true)); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// sessionUser returns the signed-in username (AuthMiddleware sets it).
func sessionUser(r *http.Request) string {
	if s, ok := GetUserFromContext(r.Context()); ok && s != nil {
		return s.Username
	}
	return ""
}
