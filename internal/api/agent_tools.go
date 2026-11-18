package api

import (
	"encoding/json"
	"net/http"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/gorilla/mux"
)

// ToolLister lists the tools of running flows (*flow.RuntimeManager).
type ToolLister interface {
	Tools() (uint64, []flow.ToolInfo)
	GetRuntime(flowID string) (*flow.FlowRuntime, bool)
}

// AgentToolsHandler serves GET /api/agent/tools: the published tools, for
// the editor's grant pickers (docs/design/mcp.md §7).
type AgentToolsHandler struct {
	tools ToolLister
}

// NewAgentToolsHandler returns the published-tools handler.
func NewAgentToolsHandler(tools ToolLister) *AgentToolsHandler {
	return &AgentToolsHandler{tools: tools}
}

// RegisterRoutes registers GET /api/agent/tools.
func (h *AgentToolsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/agent/tools", h.List).Methods(http.MethodGet)
}

// AgentTool is one published tool as the editor sees it.
type AgentTool struct {
	FlowID        string           `json:"flowId"`
	FlowName      string           `json:"flowName"`
	TriggerNodeID string           `json:"triggerNodeId"`
	ToolName      string           `json:"toolName"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Risk          node.Risk        `json:"risk"`
	Annotations   flow.Annotations `json:"annotations"`
	Fingerprint   string           `json:"fingerprint"`
	Confirm       string           `json:"confirm"`
	InputSchema   json.RawMessage  `json:"inputSchema"`
}

// AgentToolList is the GET /api/agent/tools payload. Version changes with
// every deploy, undeploy or stop.
type AgentToolList struct {
	Version uint64      `json:"version"`
	Tools   []AgentTool `json:"tools"`
}

// List answers the published tools.
// GET /api/agent/tools
func (h *AgentToolsHandler) List(w http.ResponseWriter, _ *http.Request) {
	version, tools := h.tools.Tools()
	out := AgentToolList{Version: version, Tools: make([]AgentTool, 0, len(tools))}
	for _, ti := range tools {
		name := ""
		if rt, ok := h.tools.GetRuntime(ti.FlowID); ok {
			name = rt.Flow().Name
		}
		out.Tools = append(out.Tools, AgentTool{
			FlowID:        ti.FlowID,
			FlowName:      name,
			TriggerNodeID: ti.TriggerNodeID,
			ToolName:      ti.ToolName,
			Title:         ti.Title,
			Description:   ti.Description,
			Risk:          ti.Risk,
			Annotations:   ti.Annotations,
			Fingerprint:   ti.Fingerprint,
			Confirm:       ti.Confirm,
			InputSchema:   ti.Schema.JSON(),
		})
	}
	writeSuccess(w, http.StatusOK, out, "")
}
