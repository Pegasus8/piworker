package api

import (
	"net/http"

	"github.com/Pegasus8/piworker/internal/storage"
)

// AgentSettings is the GET/PUT /api/agent/settings payload. AuthorEnabled,
// MCPEnabled and LANAccess reflect mcp.author_enabled, mcp.enabled and
// mcp.lan_access and are read-only here (they live in the TOML).
type AgentSettings struct {
	AgentDeploy   storage.AgentDeployLevel `json:"agentDeploy"`
	AuthorEnabled bool                     `json:"authorEnabled"`
	MCPEnabled    bool                     `json:"mcpEnabled"`
	LANAccess     bool                     `json:"lanAccess"`
}

type putAgentSettingsRequest struct {
	AgentDeploy string `json:"agentDeploy"`
	Password    string `json:"password"`
}

func (h *AgentHandler) settings(w http.ResponseWriter) (AgentSettings, bool) {
	level, err := h.store.AgentDeployLevel()
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to read agent deploy level")
		writeError(w, http.StatusInternalServerError, "Cannot read agent settings")
		return AgentSettings{}, false
	}
	return AgentSettings{AgentDeploy: level, AuthorEnabled: h.authorEnabled, MCPEnabled: h.mcpEnabled, LANAccess: h.lanAccess}, true
}

// GetSettings returns the agent settings.
// GET /api/agent/settings
func (h *AgentHandler) GetSettings(w http.ResponseWriter, _ *http.Request) {
	if s, ok := h.settings(w); ok {
		writeSuccess(w, http.StatusOK, s, "")
	}
}

// PutSettings changes the agent deploy level after password step-up: raising
// it is an explicit acceptance of risk (docs/design/mcp.md §10).
// PUT /api/agent/settings
func (h *AgentHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var req putAgentSettingsRequest
	if !decodeAgentBody(w, r, &req) {
		return
	}
	level, err := storage.ParseAgentDeployLevel(req.AgentDeploy)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request: agentDeploy must be off, low or all")
		return
	}
	if !h.auth.verifyStepUp(w, r, req.Password) {
		return
	}
	if err := h.store.SetAgentDeployLevel(level); err != nil {
		h.logger.Error().Err(err).Msg("Failed to store agent deploy level")
		writeError(w, http.StatusInternalServerError, "Cannot save agent settings")
		return
	}
	h.logger.Warn().Str("agentDeploy", string(level)).Str("by", sessionUser(r)).Msg("Agent deploy level changed")
	if s, ok := h.settings(w); ok {
		writeSuccess(w, http.StatusOK, s, "")
	}
}
