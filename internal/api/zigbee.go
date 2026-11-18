package api

import (
	"encoding/json"
	"net/http"

	"github.com/Pegasus8/piworker/internal/zigbee"
	"github.com/gorilla/mux"
)

// ZigbeeNetworks is what the Zigbee API reads and does (*zigbee.Manager).
type ZigbeeNetworks interface {
	Connections() []string
	Network(name string) (zigbee.Network, bool)
	PermitJoin(name string, seconds int) error
}

// ZigbeeHandler serves device discovery for the editor and the bridge's
// permit join (docs/design/zigbee.md, phase 2). Admin session only; no MCP
// tool reaches it.
type ZigbeeHandler struct{ nets ZigbeeNetworks }

// NewZigbeeHandler creates a ZigbeeHandler.
func NewZigbeeHandler(nets ZigbeeNetworks) *ZigbeeHandler { return &ZigbeeHandler{nets: nets} }

// RegisterRoutes registers the Zigbee routes.
func (h *ZigbeeHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/zigbee", h.List).Methods(http.MethodGet)
	r.HandleFunc("/api/zigbee/{name}", h.Get).Methods(http.MethodGet)
	r.HandleFunc("/api/zigbee/{name}/permit-join", h.PermitJoin).Methods(http.MethodPost)
}

// List returns the network of every connection.
func (h *ZigbeeHandler) List(w http.ResponseWriter, _ *http.Request) {
	nets := []zigbee.Network{}
	for _, name := range h.nets.Connections() {
		if n, ok := h.nets.Network(name); ok {
			nets = append(nets, n)
		}
	}
	writeSuccess(w, http.StatusOK, map[string]any{"networks": nets}, "")
}

// Get returns one connection's network.
func (h *ZigbeeHandler) Get(w http.ResponseWriter, r *http.Request) {
	n, ok := h.nets.Network(mux.Vars(r)["name"])
	if !ok {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	writeSuccess(w, http.StatusOK, n, "")
}

// PermitJoin opens the network to new devices for seconds (0 closes it).
func (h *ZigbeeHandler) PermitJoin(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if _, ok := h.nets.Network(name); !ok {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req struct {
		Seconds *int `json:"seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Seconds == nil ||
		*req.Seconds < 0 || *req.Seconds > zigbee.MaxPermitJoinSeconds {
		writeError(w, http.StatusBadRequest, "seconds must be between 0 and 254")
		return
	}
	if err := h.nets.PermitJoin(name, *req.Seconds); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeSuccess(w, http.StatusOK, nil, "Permit join sent")
}
