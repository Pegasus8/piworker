package api

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// toolCallWriteDeadline covers the longest tool timeout (30 s) plus margin:
// the server's default WriteTimeout (15 s) would cut a slow call's answer.
const toolCallWriteDeadline = 35 * time.Second

// ToolCaller calls a published tool (*service.Tools).
type ToolCaller interface {
	Call(ctx context.Context, actor service.Actor, ref service.ToolRef, args map[string]any) (flow.CallResult, error)
}

// ToolCallHandler is the editor's REST twin of an MCP tool call ("Test
// tool"): the same service call as an agent's, as the editor actor, so the
// input schema, concurrency limits and cooldown apply but confirm does not.
type ToolCallHandler struct {
	tools  ToolCaller
	logger zerolog.Logger
}

// NewToolCallHandler returns the REST twin handler.
func NewToolCallHandler(tools ToolCaller, logger zerolog.Logger) *ToolCallHandler {
	return &ToolCallHandler{tools: tools, logger: logger}
}

// RegisterRoutes registers POST /api/flows/{id}/call/{nodeId}.
func (h *ToolCallHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/flows/{id}/call/{nodeId}", h.Call).Methods(http.MethodPost)
}

// ToolCallRequest is the body of a test call; no body means no arguments.
type ToolCallRequest struct {
	Args map[string]any `json:"args"`
}

// ToolCallErrorData is the data of a refused call: the error kind's code,
// the schema issues of invalid arguments, and how long a cooldown lasts.
type ToolCallErrorData struct {
	Code         string             `json:"code"`
	Issues       []toolschema.Issue `json:"issues,omitempty"`
	RetryAfterMs int64              `json:"retryAfterMs,omitempty"`
}

// Call runs the tool of a trigger-tool node with the given arguments and
// answers its flow.CallResult.
// POST /api/flows/{id}/call/{nodeId} {"args": {…}}
func (h *ToolCallHandler) Call(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	var req ToolCallRequest
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &req, json.RejectUnknownMembers(true)); err != nil {
			writeError(w, http.StatusBadRequest, `Invalid request body: expected {"args": {…}}`)
			return
		}
	}
	if req.Args == nil {
		req.Args = map[string]any{}
	}

	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(toolCallWriteDeadline))
	res, err := h.tools.Call(r.Context(), service.EditorActor(), service.ToolByNode(vars["id"], vars["nodeId"]), req.Args)
	if err != nil && res.RunID == "" {
		h.writeCallError(w, err, vars["id"], vars["nodeId"])
		return
	}
	writeSuccess(w, http.StatusOK, res, "")
}

func (h *ToolCallHandler) writeCallError(w http.ResponseWriter, err error, flowID, nodeID string) {
	var te *service.ToolError
	if !errors.As(err, &te) {
		h.logger.Error().Err(err).Str("flowID", flowID).Str("nodeID", nodeID).Msg("Test tool call failed")
		writeError(w, http.StatusInternalServerError, "The tool call failed")
		return
	}
	status := http.StatusInternalServerError
	switch te.Kind {
	case service.ErrToolNotFound, service.ErrRunNotFound:
		status = http.StatusNotFound
	case service.ErrPermissionDenied:
		status = http.StatusForbidden
	case service.ErrReconfirmRequired:
		status = http.StatusConflict
	case service.ErrInvalidArguments:
		status = http.StatusBadRequest
	case service.ErrBusy, service.ErrCooldown:
		status = http.StatusTooManyRequests
	case service.ErrUnavailable:
		status = http.StatusServiceUnavailable
	}
	data := ToolCallErrorData{Code: te.Code(), Issues: te.Issues, RetryAfterMs: te.RetryAfter.Milliseconds()}
	if te.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(te.RetryAfter.Seconds()))))
	}
	writeJSON(w, status, APIResponse{Success: false, Error: te.Error(), Data: data})
}
