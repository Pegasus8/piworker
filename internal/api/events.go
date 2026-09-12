package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Pegasus8/piworker/internal/events"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// sseKeepalive is how often a comment frame is sent to hold the connection open.
// Streaming responses manage their deadline independently of normal requests.
const sseKeepalive = 10 * time.Second

// EventsHandler serves the live SSE stream of node-execution events plus the
// persisted run/event history.
type EventsHandler struct {
	hub    *events.Hub
	store  *storage.SQLiteStore
	logger zerolog.Logger
}

// NewEventsHandler creates a new EventsHandler.
func NewEventsHandler(hub *events.Hub, store *storage.SQLiteStore, logger zerolog.Logger) *EventsHandler {
	return &EventsHandler{hub: hub, store: store, logger: logger}
}

// RegisterRoutes registers the observability routes on the given router.
func (h *EventsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/flows/{id}/events", h.StreamEvents).Methods(http.MethodGet)
	r.HandleFunc("/api/flows/{id}/runs", h.ListRuns).Methods(http.MethodGet)
	r.HandleFunc("/api/flows/{id}/runs/{runId}/events", h.ListRunEvents).Methods(http.MethodGet)
}

// RunsResponse is the payload for the run-history listing.
type RunsResponse struct {
	Runs []storage.FlowRun `json:"runs"`
}

// RunEventsResponse is the payload for a single run's node events.
type RunEventsResponse struct {
	Events []storage.NodeEventRecord `json:"events"`
}

// StreamEvents streams a flow's live node-execution events as Server-Sent Events.
// GET /api/flows/{id}/events
func (h *EventsHandler) StreamEvents(w http.ResponseWriter, r *http.Request) {
	flowID := mux.Vars(r)["id"]

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// WriteTimeout is an absolute deadline for the entire response, not an
	// inactivity timeout. SSE responses remain open until the client disconnects.
	// Recorders used in handler tests do not implement connection deadlines.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusInternalServerError, "cannot configure streaming deadline")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsub := h.hub.Subscribe(flowID)
	defer unsub()

	keepalive := time.NewTicker(sseKeepalive)
	defer keepalive.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-ch:
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: ")); err != nil {
				return
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ListRuns returns a flow's recent runs.
// GET /api/flows/{id}/runs?limit=&offset=
func (h *EventsHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	flowID := mux.Vars(r)["id"]
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	runs, err := h.store.ListFlowRuns(flowID, limit, offset)
	if err != nil {
		h.logger.Error().Err(err).Str("flowID", flowID).Msg("Failed to list flow runs")
		writeError(w, http.StatusInternalServerError, "Failed to list runs")
		return
	}
	if runs == nil {
		runs = []storage.FlowRun{}
	}
	writeSuccess(w, http.StatusOK, RunsResponse{Runs: runs}, "")
}

// ListRunEvents returns the node events for a single run.
// GET /api/flows/{id}/runs/{runId}/events
func (h *EventsHandler) ListRunEvents(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["runId"]

	evs, err := h.store.ListNodeEvents(runID)
	if err != nil {
		h.logger.Error().Err(err).Str("runID", runID).Msg("Failed to list run events")
		writeError(w, http.StatusInternalServerError, "Failed to list run events")
		return
	}
	if evs == nil {
		evs = []storage.NodeEventRecord{}
	}
	writeSuccess(w, http.StatusOK, RunEventsResponse{Events: evs}, "")
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}
