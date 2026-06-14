package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// NodeTypesHandler handles HTTP requests for node type operations.
type NodeTypesHandler struct {
	registry *node.Registry
	logger   zerolog.Logger
}

// NewNodeTypesHandler creates a new NodeTypesHandler.
func NewNodeTypesHandler(registry *node.Registry, logger zerolog.Logger) *NodeTypesHandler {
	return &NodeTypesHandler{
		registry: registry,
		logger:   logger,
	}
}

// RegisterRoutes registers the node type routes on the given router.
func (h *NodeTypesHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/node-types", h.ListNodeTypes).Methods(http.MethodGet)
	r.HandleFunc("/api/node-types/{type}", h.GetNodeType).Methods(http.MethodGet)
	r.HandleFunc("/api/nodes/{type}/test", h.TestNode).Methods(http.MethodPost)
}

// testNodeTimeout bounds a single one-shot node execution from the editor.
const testNodeTimeout = 10 * time.Second

// TestNodeRequest is the body of a test-node request: the candidate config and a
// sample input payload.
type TestNodeRequest struct {
	Config  map[string]interface{} `json:"config"`
	Payload interface{}            `json:"payload"`
}

// TestNodeResult is the outcome of a one-shot node execution.
type TestNodeResult struct {
	Outputs []*types.Message `json:"outputs"`
	Error   string           `json:"error,omitempty"`
	DurMs   float64          `json:"durMs"`
}

// TestNode runs a single node once against a sample payload, without deploying a
// flow, so the editor can preview a node's behaviour. It is restricted to the
// processing category in v1: action/trigger nodes have real side effects (HTTP
// calls, shell commands, emails) that a "test" must not fire blindly.
// POST /api/nodes/{type}/test
func (h *NodeTypesHandler) TestNode(w http.ResponseWriter, r *http.Request) {
	nodeType := mux.Vars(r)["type"]

	info, ok := h.registry.Get(nodeType)
	if !ok {
		writeError(w, http.StatusNotFound, "Node type not found")
		return
	}
	if info.Category != types.NodeCategoryProcessing {
		writeError(w, http.StatusUnprocessableEntity, "Only processing nodes can be tested")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req TestNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	inst, err := h.registry.Create(nodeType, req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to create node: %v", err))
		return
	}
	if err := inst.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid config: %v", err))
		return
	}

	result, panicked := h.runOnce(r.Context(), inst, req.Payload)
	if panicked {
		// A node that panics is a server-side fault, distinct from a node that
		// returns a normal error (which is reported as 200 with an error field).
		writeError(w, http.StatusInternalServerError, result.Error)
		return
	}
	writeSuccess(w, http.StatusOK, result, "")
}

// runOnce executes a node a single time with a timeout, recovering panics so a
// misbehaving node returns an error instead of crashing the server. It reports
// whether the node panicked so the caller can distinguish a crash from a normal
// node error.
func (h *NodeTypesHandler) runOnce(parent context.Context, inst node.Node, payload interface{}) (result TestNodeResult, panicked bool) {
	ctx, cancel := context.WithTimeout(parent, testNodeTimeout)
	defer cancel()

	msg := types.NewMessage(payload, types.DataTypeAny)
	start := time.Now()
	defer func() {
		result.DurMs = float64(time.Since(start).Microseconds()) / 1000.0
		if rec := recover(); rec != nil {
			h.logger.Error().Interface("panic", rec).Msg("Panic during node test")
			result.Error = fmt.Sprintf("panic: %v", rec)
			panicked = true
		}
	}()

	outputs, err := inst.Process(ctx, msg)
	if err != nil {
		result.Error = err.Error()
		return result, false
	}
	result.Outputs = outputs
	return result, false
}

// NodeTypeResponse represents a node type in API responses.
type NodeTypeResponse struct {
	Type          string             `json:"type"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Documentation string             `json:"documentation,omitempty"`
	Category      string             `json:"category"`
	Inputs        []PortResponse     `json:"inputs"`
	Outputs       []PortResponse     `json:"outputs"`
	Config        *node.ConfigSchema `json:"config,omitempty"`
	Icon          string             `json:"icon,omitempty"`
}

// PortResponse represents a port in API responses.
type PortResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Multiple bool   `json:"multiple"`
	Required bool   `json:"required"`
}

// ListNodeTypesResponse contains the list of available node types.
type ListNodeTypesResponse struct {
	NodeTypes []NodeTypeResponse `json:"nodeTypes"`
	Total     int                `json:"total"`
}

// toNodeTypeResponse converts a node.NodeTypeInfo to a NodeTypeResponse.
func toNodeTypeResponse(info node.NodeTypeInfo) NodeTypeResponse {
	inputs := make([]PortResponse, 0, len(info.Inputs))
	for _, p := range info.Inputs {
		inputs = append(inputs, PortResponse{
			ID:       p.ID,
			Name:     p.Name,
			DataType: string(p.DataType),
			Multiple: p.Multiple,
			Required: p.Required,
		})
	}

	outputs := make([]PortResponse, 0, len(info.Outputs))
	for _, p := range info.Outputs {
		outputs = append(outputs, PortResponse{
			ID:       p.ID,
			Name:     p.Name,
			DataType: string(p.DataType),
			Multiple: p.Multiple,
			Required: p.Required,
		})
	}

	return NodeTypeResponse{
		Type:          info.Type,
		Name:          info.Name,
		Description:   info.Description,
		Documentation: info.Documentation,
		Category:      string(info.Category),
		Inputs:        inputs,
		Outputs:       outputs,
		Config:        info.Config,
		Icon:          info.Icon,
	}
}

// ListNodeTypes returns all available node types.
// GET /api/node-types
func (h *NodeTypesHandler) ListNodeTypes(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Msg("Listing node types")

	// Get optional category filter
	category := r.URL.Query().Get("category")

	var nodeTypes []node.NodeTypeInfo
	if category != "" {
		nodeTypes = h.registry.ListByCategory(parseCategory(category))
	} else {
		nodeTypes = h.registry.List()
	}

	// Convert to response format
	responseTypes := make([]NodeTypeResponse, 0, len(nodeTypes))
	for _, t := range nodeTypes {
		responseTypes = append(responseTypes, toNodeTypeResponse(t))
	}

	writeSuccess(w, http.StatusOK, ListNodeTypesResponse{
		NodeTypes: responseTypes,
		Total:     len(responseTypes),
	}, "")
}

// GetNodeType returns a specific node type by type identifier.
// GET /api/node-types/{type}
func (h *NodeTypesHandler) GetNodeType(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeType := vars["type"]

	if nodeType == "" {
		writeError(w, http.StatusBadRequest, "Node type is required")
		return
	}

	info, exists := h.registry.Get(nodeType)
	if !exists {
		writeError(w, http.StatusNotFound, "Node type not found")
		return
	}

	writeSuccess(w, http.StatusOK, toNodeTypeResponse(info), "")
}

// parseCategory converts a string to a types.NodeCategory.
func parseCategory(s string) types.NodeCategory {
	switch s {
	case "input":
		return types.NodeCategoryInput
	case "processing":
		return types.NodeCategoryProcessing
	case "output":
		return types.NodeCategoryOutput
	default:
		return types.NodeCategory(s)
	}
}
