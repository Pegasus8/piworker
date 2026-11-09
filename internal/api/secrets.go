package api

import (
	"encoding/json"
	"net/http"

	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// SecretsHandler exposes CRUD for named secrets. Values are write-only over the
// API: they can be set, but the listing returns names and short marks only,
// never values or lengths.
type SecretsHandler struct {
	store  *secrets.Store
	logger zerolog.Logger
}

// NewSecretsHandler creates a new SecretsHandler.
func NewSecretsHandler(store *secrets.Store, logger zerolog.Logger) *SecretsHandler {
	return &SecretsHandler{store: store, logger: logger}
}

// RegisterRoutes registers the secret routes on the given router.
func (h *SecretsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/secrets", h.ListSecrets).Methods(http.MethodGet)
	r.HandleFunc("/api/secrets/{name}", h.SetSecret).Methods(http.MethodPut)
	r.HandleFunc("/api/secrets/{name}", h.DeleteSecret).Methods(http.MethodDelete)
}

// SecretItem is a stored secret as the listing shows it: its name and
// whether its value is short (secrets.IsShort), never the value or its
// length.
type SecretItem struct {
	Name  string `json:"name"`
	Short bool   `json:"short"`
}

// SecretsListResponse lists the stored secrets by name. ShortBelow is the
// threshold (secrets.ShortValueRunes) the editor names when it asks for a
// longer value.
type SecretsListResponse struct {
	Secrets    []SecretItem `json:"secrets"`
	ShortBelow int          `json:"shortBelow"`
}

// ListSecrets returns the stored secrets, each marked short or not, so the
// editor can tell the admin which values an agent could guess (ADR-0021).
func (h *SecretsHandler) ListSecrets(w http.ResponseWriter, _ *http.Request) {
	entries := h.store.List()
	items := make([]SecretItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, SecretItem{Name: e.Name, Short: e.Short})
	}
	writeSuccess(w, http.StatusOK, SecretsListResponse{Secrets: items, ShortBelow: secrets.ShortValueRunes}, "")
}

// SetSecret stores or replaces a secret value.
func (h *SecretsHandler) SetSecret(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !secrets.ValidName(name) {
		writeError(w, http.StatusBadRequest, "Invalid secret name (use letters, digits, '_' or '-')")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.store.Set(name, req.Value); err != nil {
		h.logger.Error().Err(err).Str("name", name).Msg("Failed to set secret")
		writeError(w, http.StatusInternalServerError, "Failed to store secret")
		return
	}
	writeSuccess(w, http.StatusOK, nil, "Secret stored")
}

// DeleteSecret removes a secret.
func (h *SecretsHandler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if err := h.store.Delete(name); err != nil {
		h.logger.Error().Err(err).Str("name", name).Msg("Failed to delete secret")
		writeError(w, http.StatusInternalServerError, "Failed to delete secret")
		return
	}
	writeSuccess(w, http.StatusOK, nil, "Secret deleted")
}
