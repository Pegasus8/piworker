package api

import (
	"net/http"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// Server is the tests' flows API: the flow routes and the health check
// over a store, a runtime manager of its own and its own per-flow variable
// stores. The real wiring is buildApp in package main.
type Server struct {
	router  *mux.Router
	handler *FlowsHandler
}

// NewServer creates the tests' flows API.
func NewServer(store *storage.SQLiteStore, registry *node.Registry, logger zerolog.Logger) *Server {
	manager := flow.NewRuntimeManager(registry, logger)
	handler := NewFlowsHandler(store, service.NewLifecycle(store, manager, logger), registry, logger,
		&flowvars.FlowStores{Factory: store.FlowVariables})

	router := mux.NewRouter()
	handler.RegisterRoutes(router)
	router.HandleFunc("/api/health", NewHealthHandler(store).Health).Methods(http.MethodGet)
	router.NotFoundHandler = http.HandlerFunc(NotFoundHandler)
	router.MethodNotAllowedHandler = http.HandlerFunc(MethodNotAllowedHandler)
	return &Server{router: router, handler: handler}
}

// Router returns the HTTP router.
func (s *Server) Router() *mux.Router { return s.router }

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }
