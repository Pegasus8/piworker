package api

import (
	"encoding/json"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/gorilla/mux"
	"io"
	"net/http"
	"regexp"
)

// VariablesHandler manages the same global store used by Get/Set Variable nodes.
// Values are ordinary JSON data; credential values belong in the secret store.
type VariablesHandler struct{ store *flowvars.Store }

func NewVariablesHandler(store *flowvars.Store) *VariablesHandler {
	return &VariablesHandler{store: store}
}
func (h *VariablesHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/variables", h.List).Methods(http.MethodGet)
	r.HandleFunc("/api/variables/{name}", h.Set).Methods(http.MethodPut)
	r.HandleFunc("/api/variables/{name}", h.Delete).Methods(http.MethodDelete)
}
func (h *VariablesHandler) List(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, http.StatusOK, map[string]interface{}{"variables": h.store.Snapshot()}, "")
}

var variableName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (h *VariablesHandler) Set(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if len(name) > 128 || !variableName.MatchString(name) {
		writeError(w, 400, "Invalid variable name")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var body struct {
		Value json.RawMessage `json:"value"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil || len(body.Value) == 0 {
		writeError(w, 400, "A JSON value is required")
		return
	}
	if decoder.Decode(new(interface{})) != io.EOF {
		writeError(w, 400, "Expected one JSON object")
		return
	}
	var value interface{}
	if json.Unmarshal(body.Value, &value) != nil {
		writeError(w, 400, "Invalid JSON value")
		return
	}
	if err := h.store.Set(name, value); err != nil {
		writeError(w, 500, "Failed to persist variable")
		return
	}
	writeSuccess(w, 200, nil, "Variable saved")
}
func (h *VariablesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(mux.Vars(r)["name"]); err != nil {
		writeError(w, 500, "Failed to delete variable")
		return
	}
	writeSuccess(w, 200, nil, "Variable deleted")
}
