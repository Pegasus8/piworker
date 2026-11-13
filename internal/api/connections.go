package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// ConnectionTester tries a broker connection (connect, then disconnect).
type ConnectionTester interface {
	TestConnection(ctx context.Context, c brokers.Connection) error
}

// ConnectionsHandler manages the named MQTT broker connections (ADR-0026).
// Passwords are write-only: set or replaced, never returned.
type ConnectionsHandler struct {
	store  *brokers.Store
	tester ConnectionTester
	logger zerolog.Logger
	flows  ConnectionUsers // nil: no flows are restarted or checked
}

// ConnectionUsers is the editor's lifecycle as the connections see it
// (FlowsHandler, ADR-0029).
type ConnectionUsers interface {
	// RestartFlowsUsing restarts the running flows that use the connection.
	RestartFlowsUsing(ctx context.Context, name string) ([]FlowRef, []FlowRestartFailure)
	// UnlessRunningFlowsUse runs fn unless a running flow uses the
	// connection (then it returns them and ErrConnectionInUse).
	UnlessRunningFlowsUse(name string, fn func() error) ([]FlowRef, error)
}

// WithFlows makes a change restart the running flows that use the
// connection and refuses deleting one a running flow uses.
func (h *ConnectionsHandler) WithFlows(flows ConnectionUsers) *ConnectionsHandler {
	h.flows = flows
	return h
}

// ConnectionStoredResponse is PUT's answer: the running flows that use the
// connection and were restarted with its new settings, and those that could
// not start again (now failed). Both are lists, empty when none.
type ConnectionStoredResponse struct {
	Restarted []FlowRef            `json:"restarted"`
	Failed    []FlowRestartFailure `json:"failed"`
}

// NewConnectionsHandler creates a ConnectionsHandler.
func NewConnectionsHandler(store *brokers.Store, tester ConnectionTester, logger zerolog.Logger) *ConnectionsHandler {
	return &ConnectionsHandler{store: store, tester: tester, logger: logger}
}

// RegisterRoutes registers the connection routes.
func (h *ConnectionsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/connections", h.List).Methods(http.MethodGet)
	r.HandleFunc("/api/connections/{name}", h.Put).Methods(http.MethodPut)
	r.HandleFunc("/api/connections/{name}", h.Delete).Methods(http.MethodDelete)
	r.HandleFunc("/api/connections/{name}/test", h.Test).Methods(http.MethodPost)
}

// ConnectionItem is a connection as the API shows it: never its password.
type ConnectionItem struct {
	Name        string `json:"name"`
	Broker      string `json:"broker"`
	Username    string `json:"username"`
	HasPassword bool   `json:"hasPassword"`
	BaseTopic   string `json:"baseTopic"`
}

// connectionRequest is a PUT or test body. A missing password keeps the
// stored one; "" clears it.
type connectionRequest struct {
	Broker    string  `json:"broker"`
	Username  string  `json:"username"`
	Password  *string `json:"password"`
	BaseTopic string  `json:"baseTopic"`
}

// List returns the connections sorted by name.
func (h *ConnectionsHandler) List(w http.ResponseWriter, _ *http.Request) {
	conns := h.store.List()
	items := make([]ConnectionItem, 0, len(conns))
	for _, c := range conns {
		items = append(items, ConnectionItem{Name: c.Name, Broker: c.Broker, Username: c.Username, HasPassword: c.Password != "", BaseTopic: c.BaseTopic})
	}
	writeSuccess(w, http.StatusOK, map[string]any{"connections": items}, "")
}

// connection decodes the request into the connection named by the path,
// keeping the stored password when the request has none.
func (h *ConnectionsHandler) connection(w http.ResponseWriter, r *http.Request) (brokers.Connection, bool) {
	name := mux.Vars(r)["name"]
	if !brokers.ValidName(name) {
		writeError(w, http.StatusBadRequest, "Invalid connection name (use letters, digits, '_' or '-')")
		return brokers.Connection{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req connectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return brokers.Connection{}, false
	}
	c := brokers.Connection{Name: name, Broker: req.Broker, Username: req.Username, BaseTopic: req.BaseTopic}
	if req.Password != nil {
		c.Password = *req.Password
	} else if old, ok := h.store.Get(name); ok {
		c.Password = old.Password
	}
	c.Normalize()
	if err := c.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return brokers.Connection{}, false
	}
	return c, true
}

// Put creates or replaces a connection, then restarts the running flows
// that use it so they run with the new settings (ADR-0029).
func (h *ConnectionsHandler) Put(w http.ResponseWriter, r *http.Request) {
	c, ok := h.connection(w, r)
	if !ok {
		return
	}
	if err := h.store.Set(c); err != nil {
		h.logger.Error().Err(err).Str("name", c.Name).Msg("Failed to store broker connection")
		writeError(w, http.StatusInternalServerError, "Failed to store the connection")
		return
	}
	resp := ConnectionStoredResponse{Restarted: []FlowRef{}, Failed: []FlowRestartFailure{}}
	if h.flows != nil {
		resp.Restarted, resp.Failed = h.flows.RestartFlowsUsing(r.Context(), c.Name)
	}
	writeSuccess(w, http.StatusOK, resp, "Connection stored")
}

// Delete removes a connection, unless a running flow uses it (409, with
// the flows in data.flows): stopping it would be the next deploy's refusal
// arriving unannounced (ADR-0029). Stopped flows that name it are refused at
// their next deploy (undefined_connection).
func (h *ConnectionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if _, ok := h.store.Get(name); !ok {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	del := func() error { return h.store.Delete(name) }
	var err error
	if h.flows != nil {
		var users []FlowRef
		users, err = h.flows.UnlessRunningFlowsUse(name, del)
		if errors.Is(err, ErrConnectionInUse) {
			names := make([]string, 0, len(users))
			for _, f := range users {
				names = append(names, fmt.Sprintf("%q", f.Name))
			}
			writeErrorData(w, http.StatusConflict,
				"The connection is used by running flows: "+strings.Join(names, ", ")+". Stop them, or change their nodes to another connection, first.",
				map[string]any{"flows": users})
			return
		}
	} else {
		err = del()
	}
	if err != nil {
		h.logger.Error().Err(err).Str("name", name).Msg("Failed to delete broker connection")
		writeError(w, http.StatusInternalServerError, "Failed to delete the connection")
		return
	}
	writeSuccess(w, http.StatusOK, nil, "Connection deleted")
}

// connectionTestTimeout bounds a connection test.
const connectionTestTimeout = 8 * time.Second

// Test tries the settings sent (with the stored password when none is sent)
// without storing them, and answers {ok, error}.
func (h *ConnectionsHandler) Test(w http.ResponseWriter, r *http.Request) {
	c, ok := h.connection(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), connectionTestTimeout)
	defer cancel()
	if err := h.tester.TestConnection(ctx, c); err != nil {
		writeSuccess(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()}, "")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]any{"ok": true}, "")
}
