// Package api provides HTTP handlers for the PiWorker REST API.
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// MaxRequestBodySize is the maximum size of request bodies (1MB).
const MaxRequestBodySize = 1 << 20

// FlowsHandler handles HTTP requests for flow operations.
type FlowsHandler struct {
	store    *storage.SQLiteStore
	manager  *flow.RuntimeManager
	registry *node.Registry
	logger   zerolog.Logger
}

// NewFlowsHandler creates a new FlowsHandler.
func NewFlowsHandler(store *storage.SQLiteStore, manager *flow.RuntimeManager, registry *node.Registry, logger zerolog.Logger) *FlowsHandler {
	return &FlowsHandler{
		store:    store,
		manager:  manager,
		registry: registry,
		logger:   logger,
	}
}

// RegisterRoutes registers the flow routes on the given router.
func (h *FlowsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/flows", h.ListFlows).Methods(http.MethodGet)
	r.HandleFunc("/api/flows", h.CreateFlow).Methods(http.MethodPost)
	r.HandleFunc("/api/flows/{id}", h.GetFlow).Methods(http.MethodGet)
	r.HandleFunc("/api/flows/{id}", h.UpdateFlow).Methods(http.MethodPut)
	r.HandleFunc("/api/flows/{id}", h.DeleteFlow).Methods(http.MethodDelete)
	r.HandleFunc("/api/flows/{id}/toggle", h.ToggleFlow).Methods(http.MethodPatch)
	r.HandleFunc("/api/flows/{id}/deploy", h.DeployFlow).Methods(http.MethodPost)
	r.HandleFunc("/api/flows/{id}/stop", h.StopFlow).Methods(http.MethodPost)
	r.HandleFunc("/api/flows/{id}/inject/{nodeId}", h.InjectNode).Methods(http.MethodPost)
}

// APIResponse is a standard API response wrapper.
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

// ListFlowsResponse contains the list of flows.
type ListFlowsResponse struct {
	Flows []*flow.Flow `json:"flows"`
	Total int          `json:"total"`
}

// FlowStatusResponse contains the status of a flow.
type FlowStatusResponse struct {
	Flow    *flow.Flow `json:"flow"`
	Running bool       `json:"running"`
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Log the error - can't write to response as headers already sent
		log.Error().Err(err).Msg("Failed to encode JSON response")
	}
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, APIResponse{
		Success: false,
		Error:   message,
	})
}

// writeSuccess writes a success response.
func writeSuccess(w http.ResponseWriter, status int, data interface{}, message string) {
	writeJSON(w, status, APIResponse{
		Success: true,
		Data:    data,
		Message: message,
	})
}

// ListFlows returns all flows.
// GET /api/flows
func (h *FlowsHandler) ListFlows(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Msg("Listing flows")

	flows, err := h.store.ListFlows(nil)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list flows")
		writeError(w, http.StatusInternalServerError, "Failed to list flows")
		return
	}

	if flows == nil {
		flows = []*flow.Flow{}
	}

	writeSuccess(w, http.StatusOK, ListFlowsResponse{
		Flows: flows,
		Total: len(flows),
	}, "")
}

// CreateFlow creates a new flow.
// POST /api/flows
func (h *FlowsHandler) CreateFlow(w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent DoS attacks
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	var f flow.Flow
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		h.logger.Error().Err(err).Msg("Failed to decode flow")
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Generate ID if not provided
	if f.ID == "" {
		newFlow := flow.NewFlow(f.Name)
		f.ID = newFlow.ID
		f.CreatedAt = newFlow.CreatedAt
		f.UpdatedAt = newFlow.UpdatedAt
	}

	// Set default state
	if f.State == "" {
		f.State = flow.FlowStateInactive
	}

	if err := h.store.CreateFlow(&f); err != nil {
		if err == storage.ErrFlowAlreadyExists {
			writeError(w, http.StatusConflict, "Flow already exists")
			return
		}
		h.logger.Error().Err(err).Msg("Failed to create flow")
		writeError(w, http.StatusInternalServerError, "Failed to create flow")
		return
	}

	h.logger.Info().Str("flowID", f.ID).Str("name", f.Name).Msg("Flow created")
	writeSuccess(w, http.StatusCreated, &f, "Flow created successfully")
}

// GetFlow returns a specific flow by ID.
// GET /api/flows/{id}
func (h *FlowsHandler) GetFlow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		writeError(w, http.StatusBadRequest, "Flow ID is required")
		return
	}

	f, err := h.store.GetFlow(id)
	if err != nil {
		if err == storage.ErrFlowNotFound {
			writeError(w, http.StatusNotFound, "Flow not found")
			return
		}
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to get flow")
		writeError(w, http.StatusInternalServerError, "Failed to get flow")
		return
	}

	// Check if running
	running := false
	if rt, ok := h.manager.GetRuntime(id); ok {
		running = rt.IsRunning()
	}

	writeSuccess(w, http.StatusOK, FlowStatusResponse{
		Flow:    f,
		Running: running,
	}, "")
}

// UpdateFlow updates an existing flow.
// PUT /api/flows/{id}
func (h *FlowsHandler) UpdateFlow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		writeError(w, http.StatusBadRequest, "Flow ID is required")
		return
	}

	// Refuse to edit a running flow: the live runtime keeps executing the old
	// definition, so persisting a new one would silently desync storage/UI from
	// what's actually running. Require an explicit stop first.
	if rt, ok := h.manager.GetRuntime(id); ok && rt.IsRunning() {
		writeError(w, http.StatusConflict, "Cannot update a running flow; stop it first")
		return
	}

	// Limit request body size to prevent DoS attacks
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	// Check if flow exists
	existing, err := h.store.GetFlow(id)
	if err != nil {
		if err == storage.ErrFlowNotFound {
			writeError(w, http.StatusNotFound, "Flow not found")
			return
		}
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to get flow")
		writeError(w, http.StatusInternalServerError, "Failed to get flow")
		return
	}

	var f flow.Flow
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		h.logger.Error().Err(err).Msg("Failed to decode flow")
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Preserve the ID and creation time
	f.ID = id
	f.CreatedAt = existing.CreatedAt

	// Don't trust the client's state field. The flow is not running (guarded
	// above), so never persist a "running" state coming from the request body.
	if f.State == flow.FlowStateRunning {
		f.State = flow.FlowStateInactive
	}

	if err := h.store.UpdateFlow(&f); err != nil {
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to update flow")
		writeError(w, http.StatusInternalServerError, "Failed to update flow")
		return
	}

	h.logger.Info().Str("flowID", id).Msg("Flow updated")
	writeSuccess(w, http.StatusOK, &f, "Flow updated successfully")
}

// DeleteFlow deletes a flow.
// DELETE /api/flows/{id}
func (h *FlowsHandler) DeleteFlow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		writeError(w, http.StatusBadRequest, "Flow ID is required")
		return
	}

	// Stop the flow if it's running
	if rt, ok := h.manager.GetRuntime(id); ok && rt.IsRunning() {
		if err := h.manager.Undeploy(id); err != nil {
			h.logger.Warn().Err(err).Str("flowID", id).Msg("Failed to stop flow before deletion")
		}
	}

	if err := h.store.DeleteFlow(id); err != nil {
		if err == storage.ErrFlowNotFound {
			writeError(w, http.StatusNotFound, "Flow not found")
			return
		}
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to delete flow")
		writeError(w, http.StatusInternalServerError, "Failed to delete flow")
		return
	}

	h.logger.Info().Str("flowID", id).Msg("Flow deleted")
	writeSuccess(w, http.StatusOK, nil, "Flow deleted successfully")
}

// ToggleRequest is the request body for toggling a flow.
type ToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// ToggleFlow toggles a flow on or off.
// PATCH /api/flows/{id}/toggle
func (h *FlowsHandler) ToggleFlow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		writeError(w, http.StatusBadRequest, "Flow ID is required")
		return
	}

	// Limit request body size to prevent DoS attacks
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	var req ToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error().Err(err).Msg("Failed to decode toggle request")
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	f, err := h.store.GetFlow(id)
	if err != nil {
		if err == storage.ErrFlowNotFound {
			writeError(w, http.StatusNotFound, "Flow not found")
			return
		}
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to get flow")
		writeError(w, http.StatusInternalServerError, "Failed to get flow")
		return
	}

	// Check current running state
	rt, hasRuntime := h.manager.GetRuntime(id)
	isRunning := hasRuntime && rt.IsRunning()

	if req.Enabled && !isRunning {
		// Deploy the flow
		if err := h.manager.Deploy(r.Context(), f); err != nil {
			h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to deploy flow")
			writeError(w, http.StatusInternalServerError, "Failed to deploy flow: "+err.Error())
			return
		}
		f.State = flow.FlowStateRunning
		h.logger.Info().Str("flowID", id).Msg("Flow deployed via toggle")
	} else if !req.Enabled && isRunning {
		// Stop the flow
		if err := h.manager.Undeploy(id); err != nil {
			h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to stop flow")
			writeError(w, http.StatusInternalServerError, "Failed to stop flow: "+err.Error())
			return
		}
		f.State = flow.FlowStateInactive
		h.logger.Info().Str("flowID", id).Msg("Flow stopped via toggle")
	}

	// Update state in storage
	if err := h.store.UpdateFlow(f); err != nil {
		h.logger.Warn().Err(err).Str("flowID", id).Msg("Failed to update flow state")
	}

	writeSuccess(w, http.StatusOK, FlowStatusResponse{
		Flow:    f,
		Running: req.Enabled,
	}, "")
}

// DeployFlow deploys (starts) a flow.
// POST /api/flows/{id}/deploy
func (h *FlowsHandler) DeployFlow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		writeError(w, http.StatusBadRequest, "Flow ID is required")
		return
	}

	f, err := h.store.GetFlow(id)
	if err != nil {
		if err == storage.ErrFlowNotFound {
			writeError(w, http.StatusNotFound, "Flow not found")
			return
		}
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to get flow")
		writeError(w, http.StatusInternalServerError, "Failed to get flow")
		return
	}

	// Deploy the flow
	if err := h.manager.Deploy(r.Context(), f); err != nil {
		if err == flow.ErrFlowAlreadyRunning {
			writeError(w, http.StatusConflict, "Flow is already running")
			return
		}
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to deploy flow")
		writeError(w, http.StatusInternalServerError, "Failed to deploy flow: "+err.Error())
		return
	}

	// Update state in storage
	f.State = flow.FlowStateRunning
	if err := h.store.UpdateFlow(f); err != nil {
		h.logger.Warn().Err(err).Str("flowID", id).Msg("Failed to update flow state")
	}

	h.logger.Info().Str("flowID", id).Msg("Flow deployed")
	writeSuccess(w, http.StatusOK, FlowStatusResponse{
		Flow:    f,
		Running: true,
	}, "Flow deployed successfully")
}

// StopFlow stops a running flow.
// POST /api/flows/{id}/stop
func (h *FlowsHandler) StopFlow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		writeError(w, http.StatusBadRequest, "Flow ID is required")
		return
	}

	f, err := h.store.GetFlow(id)
	if err != nil {
		if err == storage.ErrFlowNotFound {
			writeError(w, http.StatusNotFound, "Flow not found")
			return
		}
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to get flow")
		writeError(w, http.StatusInternalServerError, "Failed to get flow")
		return
	}

	// Stop the flow
	if err := h.manager.Undeploy(id); err != nil {
		if err == flow.ErrFlowNotFound {
			writeError(w, http.StatusConflict, "Flow is not running")
			return
		}
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to stop flow")
		writeError(w, http.StatusInternalServerError, "Failed to stop flow")
		return
	}

	// Update state in storage
	f.State = flow.FlowStateInactive
	if err := h.store.UpdateFlow(f); err != nil {
		h.logger.Warn().Err(err).Str("flowID", id).Msg("Failed to update flow state")
	}

	h.logger.Info().Str("flowID", id).Msg("Flow stopped")
	writeSuccess(w, http.StatusOK, FlowStatusResponse{
		Flow:    f,
		Running: false,
	}, "Flow stopped successfully")
}

// InjectRequest is the request body for injecting a message into a manual trigger.
type InjectRequest struct {
	Payload interface{} `json:"payload,omitempty"`
}

// InjectNode triggers a manual trigger node in a running flow.
// POST /api/flows/{id}/inject/{nodeId}
func (h *FlowsHandler) InjectNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	flowID := vars["id"]
	nodeID := vars["nodeId"]

	if flowID == "" {
		writeError(w, http.StatusBadRequest, "Flow ID is required")
		return
	}
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "Node ID is required")
		return
	}

	// Get the runtime for this flow
	runtime, exists := h.manager.GetRuntime(flowID)
	if !exists {
		writeError(w, http.StatusNotFound, "Flow is not running")
		return
	}

	// Parse optional payload from request body
	var req InjectRequest
	if r.Body != nil && r.ContentLength > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
	}

	// Inject the trigger
	if err := runtime.InjectTrigger(nodeID, req.Payload); err != nil {
		h.logger.Warn().Err(err).
			Str("flowID", flowID).
			Str("nodeID", nodeID).
			Msg("Failed to inject trigger")
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.logger.Info().
		Str("flowID", flowID).
		Str("nodeID", nodeID).
		Msg("Trigger injected successfully")

	writeSuccess(w, http.StatusOK, map[string]string{
		"flowId": flowID,
		"nodeId": nodeID,
	}, "Trigger injected successfully")
}

// HealthHandler handles health check requests.
type HealthHandler struct {
	store *storage.SQLiteStore
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(store *storage.SQLiteStore) *HealthHandler {
	return &HealthHandler{store: store}
}

// Health returns the health status of the API.
// GET /api/health
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	status := "healthy"
	if h.store != nil && h.store.IsClosed() {
		status = "unhealthy"
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": status,
	})
}

// NotFoundHandler handles 404 responses.
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "Resource not found")
}

// MethodNotAllowedHandler handles 405 responses.
func MethodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// Server represents the HTTP server for the API.
type Server struct {
	router  *mux.Router
	handler *FlowsHandler
	logger  zerolog.Logger
}

// NewServer creates a new API server.
func NewServer(store *storage.SQLiteStore, registry *node.Registry, logger zerolog.Logger) *Server {
	manager := flow.NewRuntimeManager(registry, logger)
	handler := NewFlowsHandler(store, manager, registry, logger)

	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	// Health check
	healthHandler := NewHealthHandler(store)
	router.HandleFunc("/api/health", healthHandler.Health).Methods(http.MethodGet)

	// Error handlers
	router.NotFoundHandler = http.HandlerFunc(NotFoundHandler)
	router.MethodNotAllowedHandler = http.HandlerFunc(MethodNotAllowedHandler)

	return &Server{
		router:  router,
		handler: handler,
		logger:  logger,
	}
}

// Router returns the HTTP router.
func (s *Server) Router() *mux.Router {
	return s.router
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Start starts the HTTP server on the given address.
func (s *Server) Start(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	s.logger.Info().Str("addr", addr).Msg("Starting API server")
	return srv.ListenAndServe()
}
