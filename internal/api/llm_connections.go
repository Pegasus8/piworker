package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// LLMConnectionsHandler manages the named LLM connections (ADR-0038,
// docs/design/llm.md §6). API keys are write-only: set, replaced or cleared,
// never returned. Changing a connection restarts nothing: the nodes resolve
// it on every call.
type LLMConnectionsHandler struct {
	store *llm.Store
	// secrets resolve {{secret.NAME}} references in a key when a test or
	// the models list builds a client.
	secrets *secrets.Store
	logger  zerolog.Logger
	flows   LLMConnectionUsers // no users until WithFlows
}

// LLMConnectionUsers is the lifecycle path as the LLM connections see it.
type LLMConnectionUsers interface {
	// UnlessRunningFlowsUseLLMConnection runs fn (the deletion of the LLM
	// connection name) under the lifecycle lock unless a running flow uses
	// the connection; then it returns those flows and fn does not run.
	UnlessRunningFlowsUseLLMConnection(name string, fn func() error) ([]FlowRef, error)
}

// noLLMUsers is LLMConnectionUsers without flows: deletes run.
type noLLMUsers struct{}

func (noLLMUsers) UnlessRunningFlowsUseLLMConnection(_ string, fn func() error) ([]FlowRef, error) {
	return nil, fn()
}

// The lifecycle path (service.Lifecycle) decides under its lock (ADR-0034),
// so no deploy starts a user between the check and the deletion.
var _ LLMConnectionUsers = (*service.Lifecycle)(nil)

// NewLLMConnectionsHandler creates an LLMConnectionsHandler over the
// connections in store, resolving secret references in keys through
// secretStore (nil: none).
func NewLLMConnectionsHandler(store *llm.Store, secretStore *secrets.Store, logger zerolog.Logger) *LLMConnectionsHandler {
	return &LLMConnectionsHandler{store: store, secrets: secretStore, logger: logger, flows: noLLMUsers{}}
}

// WithFlows refuses deleting a connection a running flow uses (the
// lifecycle path, service.Lifecycle).
func (h *LLMConnectionsHandler) WithFlows(flows LLMConnectionUsers) *LLMConnectionsHandler {
	h.flows = flows
	return h
}

// RegisterRoutes registers the LLM connection routes.
func (h *LLMConnectionsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/llm/connections", h.List).Methods(http.MethodGet)
	r.HandleFunc("/api/llm/connections/{name}", h.Put).Methods(http.MethodPut)
	r.HandleFunc("/api/llm/connections/{name}", h.Delete).Methods(http.MethodDelete)
	r.HandleFunc("/api/llm/connections/{name}/test", h.Test).Methods(http.MethodPost)
	r.HandleFunc("/api/llm/connections/{name}/models", h.Models).Methods(http.MethodGet)
}

// LLMConnectionItem is an LLM connection as the API shows it: never its API
// key, only whether one is stored, and what it used today.
type LLMConnectionItem struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// BaseURL is "" for the provider's default endpoint.
	BaseURL   string `json:"baseUrl"`
	HasAPIKey bool   `json:"hasApiKey"`
	Model     string `json:"model"`
	// DailyTokenLimit caps input plus output tokens per local day (0: none).
	DailyTokenLimit int64     `json:"dailyTokenLimit"`
	UsageToday      llm.Usage `json:"usageToday"`
}

// LLMConnectionsResponse is GET /api/llm/connections' answer.
type LLMConnectionsResponse struct {
	Connections []LLMConnectionItem `json:"connections"`
}

// LLMConnectionTest is a test's answer: whether the models could be listed
// with the settings sent, why not, and the models.
type LLMConnectionTest struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Models []llm.ModelInfo `json:"models,omitempty"`
}

// LLMModelsResponse is GET /api/llm/connections/{name}/models' answer.
type LLMModelsResponse struct {
	Models []llm.ModelInfo `json:"models"`
}

// llmConnectionRequest is a PUT or test body. A missing apiKey keeps the
// stored one; "" clears it.
type llmConnectionRequest struct {
	Provider        string  `json:"provider"`
	BaseURL         string  `json:"baseUrl"`
	APIKey          *string `json:"apiKey"`
	Model           string  `json:"model"`
	DailyTokenLimit int64   `json:"dailyTokenLimit"`
}

func (h *LLMConnectionsHandler) item(c llm.Connection) LLMConnectionItem {
	return LLMConnectionItem{
		Name: c.Name, Provider: string(c.Provider), BaseURL: c.BaseURL, HasAPIKey: c.APIKey != "",
		Model: c.Model, DailyTokenLimit: c.DailyTokenLimit, UsageToday: h.store.UsageToday(c.Name),
	}
}

// List returns the connections sorted by name, with today's usage.
func (h *LLMConnectionsHandler) List(w http.ResponseWriter, _ *http.Request) {
	conns := h.store.List()
	resp := LLMConnectionsResponse{Connections: make([]LLMConnectionItem, 0, len(conns))}
	for _, c := range conns {
		resp.Connections = append(resp.Connections, h.item(c))
	}
	writeSuccess(w, http.StatusOK, resp, "")
}

// connection decodes the request into the connection named by the path,
// keeping the stored key when the request has none. Validation errors
// never quote the key or the base URL (llm.Connection.Validate).
func (h *LLMConnectionsHandler) connection(w http.ResponseWriter, r *http.Request) (llm.Connection, bool) {
	name := mux.Vars(r)["name"]
	if !llm.ValidName(name) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid connection name (1 to %d letters, digits, '_' or '-')", llm.MaxNameLength))
		return llm.Connection{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req llmConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return llm.Connection{}, false
	}
	c := llm.Connection{Name: name, Provider: llm.Provider(req.Provider), BaseURL: req.BaseURL, Model: req.Model,
		DailyTokenLimit: req.DailyTokenLimit}
	if req.APIKey != nil {
		c.APIKey = *req.APIKey
	} else if old, ok := h.store.Get(name); ok {
		c.APIKey = old.APIKey
	}
	c.Normalize()
	if err := c.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return llm.Connection{}, false
	}
	return c, true
}

// Put creates or replaces a connection and answers it as stored.
func (h *LLMConnectionsHandler) Put(w http.ResponseWriter, r *http.Request) {
	c, ok := h.connection(w, r)
	if !ok {
		return
	}
	if err := h.store.Set(c); err != nil {
		h.logger.Error().Err(err).Str("name", c.Name).Msg("Failed to store LLM connection")
		writeError(w, http.StatusInternalServerError, "Failed to store the connection")
		return
	}
	stored, _ := h.store.Get(c.Name)
	writeSuccess(w, http.StatusOK, h.item(stored), "Connection stored")
}

// Delete removes a connection, unless a running flow uses it (409, with the
// flows in data.flows): the flow would fail on its next call. Stopped flows
// that name it are refused at their next deploy (undefined_llm_connection).
func (h *LLMConnectionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	_, exists := h.store.Get(name)
	deleteConnection(w, h.logger, "LLM connection", name, exists, h.flows.UnlessRunningFlowsUseLLMConnection, h.store.Delete)
}

// listModels lists c's models within connectionTestTimeout. Listing spends
// no tokens, so neither the daily budget nor the usage is involved; the key
// resolves through the handler's secret store. A client that cannot be
// built (an undefined secret in the key) is reported apart, as config.
func (h *LLMConnectionsHandler) listModels(ctx context.Context, c llm.Connection) (models []llm.ModelInfo, configErr, err error) {
	ctx, cancel := context.WithTimeout(ctx, connectionTestTimeout)
	defer cancel()
	ctx = llm.WithStore(secrets.WithStore(ctx, h.secrets), h.store)
	client, configErr := llm.NewClient(ctx, c)
	if configErr != nil {
		return nil, configErr, nil
	}
	if models, err = client.Models(ctx); err != nil {
		return nil, nil, err
	}
	if models == nil {
		models = []llm.ModelInfo{}
	}
	return models, nil, nil
}

// Test lists the models with the settings sent (the stored key when none is
// sent) without storing them, and answers {ok, error, models}: a listing
// checks the endpoint, the key and the provider without spending tokens.
// Errors never hold the key (the clients redact it).
func (h *LLMConnectionsHandler) Test(w http.ResponseWriter, r *http.Request) {
	c, ok := h.connection(w, r)
	if !ok {
		return
	}
	models, configErr, err := h.listModels(r.Context(), c)
	if configErr != nil {
		err = configErr
	}
	if err != nil {
		writeSuccess(w, http.StatusOK, LLMConnectionTest{OK: false, Error: err.Error()}, "")
		return
	}
	writeSuccess(w, http.StatusOK, LLMConnectionTest{OK: true, Models: models}, "")
}

// Models lists the models of a stored connection (the editor's suggestions
// for a node's model): 404 for an unknown connection, 400 when its client
// cannot be built, 502 when the provider fails (its message, redacted).
func (h *LLMConnectionsHandler) Models(w http.ResponseWriter, r *http.Request) {
	c, ok := h.store.Get(mux.Vars(r)["name"])
	if !ok {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	models, configErr, err := h.listModels(r.Context(), c)
	switch {
	case configErr != nil:
		writeError(w, http.StatusBadRequest, configErr.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, "Cannot list the models: "+err.Error())
	default:
		writeSuccess(w, http.StatusOK, LLMModelsResponse{Models: models}, "")
	}
}
