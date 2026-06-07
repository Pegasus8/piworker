package api

import (
	"net/http"

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
