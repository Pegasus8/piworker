package api

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
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
	// flows and conns, when set (WithUsage), say where each secret is used.
	flows FlowLister
	conns *brokers.Store
}

// FlowLister lists the stored flows (the SQLite store implements it).
type FlowLister interface {
	ListFlows(state *flow.FlowState) ([]*flow.Flow, error)
}

// WithUsage makes the listing say where each secret is referenced: in the
// settings of the flows of flows and in the credentials of the broker
// connections of conns (either may be nil).
func (h *SecretsHandler) WithUsage(flows FlowLister, conns *brokers.Store) *SecretsHandler {
	h.flows, h.conns = flows, conns
	return h
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

// SecretItem is a stored secret as the listing shows it: its name, whether
// its value is short (secrets.IsShort) and where it is used, never the value
// or its length.
type SecretItem struct {
	Name  string      `json:"name"`
	Short bool        `json:"short"`
	Uses  []SecretUse `json:"uses"`
}

// SecretUse is one place a secret is referenced ({{secret.NAME}}): a node
// setting of a flow (FlowID, FlowName, NodeID, NodeType, Setting; NodeDisabled
// when the node does not run; FlowRunning when the flow is deployed), or a
// broker connection's credential (Connection, Setting).
type SecretUse struct {
	FlowID       string `json:"flowId,omitempty"`
	FlowName     string `json:"flowName,omitempty"`
	FlowRunning  bool   `json:"flowRunning,omitempty"`
	NodeID       string `json:"nodeId,omitempty"`
	NodeType     string `json:"nodeType,omitempty"`
	NodeDisabled bool   `json:"nodeDisabled,omitempty"`
	Connection   string `json:"connection,omitempty"`
	Setting      string `json:"setting"`
}

// UndefinedSecret is a secret name referenced somewhere that the store does
// not define, with the places that reference it.
type UndefinedSecret struct {
	Name string      `json:"name"`
	Uses []SecretUse `json:"uses"`
}

// SecretsListResponse lists the stored secrets by name with where each is
// used, and the references to secrets that are not defined. ShortBelow is
// the threshold (secrets.ShortValueRunes) the editor names when it asks for
// a longer value.
type SecretsListResponse struct {
	Secrets    []SecretItem      `json:"secrets"`
	Undefined  []UndefinedSecret `json:"undefined"`
	ShortBelow int               `json:"shortBelow"`
}

// ListSecrets returns the stored secrets, each marked short or not, so the
// editor can tell the admin which values an agent could confirm a guess of
// (ADR-0021), and where each is used (WithUsage), so deleting or renaming
// one is not a surprise. Uses are listed connections first (by name), then
// flows (by name, then node order).
func (h *SecretsHandler) ListSecrets(w http.ResponseWriter, _ *http.Request) {
	uses, err := h.secretUses()
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list where secrets are used")
		writeError(w, http.StatusInternalServerError, "Failed to list where secrets are used")
		return
	}
	entries := h.store.List()
	items := make([]SecretItem, 0, len(entries))
	defined := map[string]bool{}
	for _, e := range entries {
		defined[e.Name] = true
		u := uses[e.Name]
		if u == nil {
			u = []SecretUse{}
		}
		items = append(items, SecretItem{Name: e.Name, Short: e.Short, Uses: u})
	}
	undefined := []UndefinedSecret{}
	for name, u := range uses {
		if !defined[name] {
			undefined = append(undefined, UndefinedSecret{Name: name, Uses: u})
		}
	}
	sort.Slice(undefined, func(i, j int) bool { return undefined[i].Name < undefined[j].Name })
	writeSuccess(w, http.StatusOK, SecretsListResponse{Secrets: items, Undefined: undefined, ShortBelow: secrets.ShortValueRunes}, "")
}

// secretUses maps each referenced secret name to where it is referenced.
func (h *SecretsHandler) secretUses() (map[string][]SecretUse, error) {
	uses := map[string][]SecretUse{}
	if h.conns != nil {
		for _, c := range h.conns.List() {
			for _, ref := range brokers.SecretReferences(c) {
				uses[ref.Name] = append(uses[ref.Name], SecretUse{Connection: c.Name, Setting: ref.Setting})
			}
		}
	}
	if h.flows != nil {
		flows, err := h.flows.ListFlows(nil)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(flows, func(i, j int) bool {
			if flows[i].Name != flows[j].Name {
				return flows[i].Name < flows[j].Name
			}
			return flows[i].ID < flows[j].ID
		})
		for _, f := range flows {
			for _, ref := range flow.SecretReferences(f) {
				uses[ref.Name] = append(uses[ref.Name], SecretUse{
					FlowID: f.ID, FlowName: f.Name, FlowRunning: f.State == flow.FlowStateRunning,
					NodeID: ref.NodeID, NodeType: ref.NodeType, NodeDisabled: !ref.Enabled, Setting: ref.Setting,
				})
			}
		}
	}
	return uses, nil
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
