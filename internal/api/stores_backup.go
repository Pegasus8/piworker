package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storesbackup"
	"github.com/Pegasus8/piworker/internal/vars"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// MaxStoresImportSize bounds an import request: the variables of a busy
// installation can outgrow MaxRequestBodySize.
const MaxStoresImportSize = 8 << 20

// StoresBackupOptions wires the export and restore of the stores (ADR-0031).
type StoresBackupOptions struct {
	Flows         FlowLister       // the flows whose variables are exported
	Globals       *vars.Store      // global variables
	FlowVariables *vars.FlowStores // each flow's variables
	Secrets       *secrets.Store
	Connections   *brokers.Store
	// Auth verifies the account password before secret values leave the
	// device; nil (authentication disabled) refuses such an export.
	Auth *AuthHandler
	// Restarter restarts the running flows that use a connection the
	// import changed (ADR-0029); nil restarts nothing.
	Restarter ConnectionUsers
	Logger    zerolog.Logger
}

// StoresBackupHandler exports and restores the global and flow variables,
// the broker connections and the secrets (internal/storesbackup).
type StoresBackupHandler struct {
	opts StoresBackupOptions
}

// NewStoresBackupHandler creates the handler.
func NewStoresBackupHandler(opts StoresBackupOptions) *StoresBackupHandler {
	return &StoresBackupHandler{opts: opts}
}

// RegisterRoutes registers the routes.
func (h *StoresBackupHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/stores/export", h.Export).Methods(http.MethodPost)
	r.HandleFunc("/api/stores/import", h.Import).Methods(http.MethodPost)
}

// sources reads the stores, with every stored flow's variable store.
func (h *StoresBackupHandler) sources() (storesbackup.Sources, error) {
	src := storesbackup.Sources{Globals: h.opts.Globals, Secrets: h.opts.Secrets, Connections: h.opts.Connections}
	if h.opts.Flows == nil || h.opts.FlowVariables == nil {
		return src, nil
	}
	flows, err := h.opts.Flows.ListFlows(nil)
	if err != nil {
		return src, err
	}
	for _, f := range flows {
		store, err := h.opts.FlowVariables.Get(f.ID)
		if err != nil {
			return src, err
		}
		src.Flows = append(src.Flows, storesbackup.FlowStore{ID: f.ID, Name: f.Name, Vars: store})
	}
	return src, nil
}

// StoresExportRequest is POST /api/stores/export's body. With a
// passphrase the secret values and the connections with their passwords are
// sealed in the file, which needs the account password (step-up).
type StoresExportRequest struct {
	Passphrase string `json:"passphrase"`
	Password   string `json:"password"`
}

// Export answers the stores file as an attachment (the bundle itself, not
// the {success, data} envelope). Without a passphrase it carries no secret
// value or password. With one, the account password must be confirmed:
// secrets are otherwise write-only, and a stolen session must not read them
// all in one request. With authentication disabled such an export is
// refused (403).
func (h *StoresBackupHandler) Export(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req StoresExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Passphrase != "" {
		if h.opts.Auth == nil {
			writeError(w, http.StatusForbidden, "Exporting secret values requires authentication, which is disabled on this installation")
			return
		}
		if err := storesbackup.CheckPassphrase(req.Passphrase); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !h.opts.Auth.verifyStepUp(w, r, req.Password) {
			return
		}
	}
	src, err := h.sources()
	if err != nil {
		h.opts.Logger.Error().Err(err).Msg("Failed to read the stores for an export")
		writeError(w, http.StatusInternalServerError, "Failed to read the stores")
		return
	}
	bundle, err := storesbackup.Export(src, req.Passphrase)
	if err != nil {
		if errors.Is(err, storesbackup.ErrWeakPassphrase) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.opts.Logger.Error().Err(err).Msg("Failed to export the stores")
		writeError(w, http.StatusInternalServerError, "Failed to export the stores")
		return
	}
	h.opts.Logger.Info().Bool("secretValues", req.Passphrase != "").Int("secrets", len(bundle.Secrets)).Msg("Stores exported")
	name := fmt.Sprintf("piworker-stores-%s.json", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, bundle)
}

// StoresImportRequest is POST /api/stores/import's body: the file, the
// passphrase that opens its sealed part (empty: restore the readable part
// only) and whether existing values are replaced.
type StoresImportRequest struct {
	Bundle     *storesbackup.Bundle `json:"bundle"`
	Passphrase string               `json:"passphrase"`
	Overwrite  bool                 `json:"overwrite"`
}

// StoresImportResponse is what the import did, and the running flows it
// restarted because a connection they use changed.
type StoresImportResponse struct {
	Report    storesbackup.Report  `json:"report"`
	Restarted []FlowRef            `json:"restarted"`
	Failed    []FlowRestartFailure `json:"failed"`
}

// Import restores a stores file. A wrong passphrase or an invalid file is a
// 400 and writes nothing; the report counts what was added, replaced and
// kept, and never echoes a value.
func (h *StoresBackupHandler) Import(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxStoresImportSize)
	var req StoresImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Bundle == nil {
		writeError(w, http.StatusBadRequest, "Invalid request body: expected the exported file as bundle")
		return
	}
	dst, err := h.sources()
	if err != nil {
		h.opts.Logger.Error().Err(err).Msg("Failed to read the stores for an import")
		writeError(w, http.StatusInternalServerError, "Failed to read the stores")
		return
	}
	report, err := storesbackup.Restore(req.Bundle, req.Passphrase, dst, storesbackup.Options{Overwrite: req.Overwrite})
	switch {
	case errors.Is(err, storesbackup.ErrWrongPassphrase), errors.Is(err, storesbackup.ErrInvalidBundle):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		h.opts.Logger.Error().Err(err).Msg("Import of the stores stopped")
		writeErrorData(w, http.StatusInternalServerError, "The import stopped partway: "+err.Error(), StoresImportResponse{Report: report})
		return
	}
	resp := StoresImportResponse{Report: report, Restarted: []FlowRef{}, Failed: []FlowRestartFailure{}}
	if h.opts.Restarter != nil {
		for _, name := range report.ChangedConnections {
			restarted, failed := h.opts.Restarter.RestartFlowsUsing(r.Context(), name)
			resp.Restarted = append(resp.Restarted, restarted...)
			resp.Failed = append(resp.Failed, failed...)
		}
	}
	h.opts.Logger.Info().Int("secrets", report.Secrets.Added+report.Secrets.Replaced).
		Int("connections", report.Connections.Added+report.Connections.Replaced).Msg("Stores imported")
	writeSuccess(w, http.StatusOK, resp, "")
}
