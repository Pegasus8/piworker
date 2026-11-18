package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// FlowAssistant proposes a flow from a description (*service.Assistant,
// docs/design/llm.md §7, ADR-0041).
type FlowAssistant interface {
	ProposeFlow(ctx context.Context, req service.AssistantFlowRequest) (service.AssistantFlowResult, error)
}

// AssistantHandler serves the editor's flow assistant. It is a thin adapter:
// the service stores, deploys and runs nothing; the editor shows the
// proposal and the admin applies it to the canvas.
type AssistantHandler struct {
	svc    FlowAssistant
	logger zerolog.Logger
}

// NewAssistantHandler creates an AssistantHandler.
func NewAssistantHandler(svc FlowAssistant, logger zerolog.Logger) *AssistantHandler {
	return &AssistantHandler{svc: svc, logger: logger}
}

// RegisterRoutes registers POST /api/llm/assistant/flow.
func (h *AssistantHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/llm/assistant/flow", h.ProposeFlow).Methods(http.MethodPost)
}

// assistantWriteDeadline outlasts the assistant's own bound: the server's
// WriteTimeout (15 s) would cut every answer.
const assistantWriteDeadline = service.AssistantTimeout + 15*time.Second

// ProposeFlow answers the assistant's proposal (service.AssistantFlowResult).
// A refused request is 400, a busy assistant 409 (one request at a time), a
// spent daily token limit 429, the time bound 504 and any other failure of
// the model request (the provider's error, an answer that is not a flow)
// 502, each with the service's message: it never holds a key or a secret
// value.
// POST /api/llm/assistant/flow {connection, model?, description, flow?}
func (h *AssistantHandler) ProposeFlow(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req service.AssistantFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(assistantWriteDeadline))
	res, err := h.svc.ProposeFlow(r.Context(), req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeSuccess(w, http.StatusOK, res, "")
}

func (h *AssistantHandler) writeError(w http.ResponseWriter, err error) {
	var te *service.ToolError
	switch {
	case errors.As(err, &te) && errors.Is(te.Kind, service.ErrBusy):
		writeError(w, http.StatusConflict, te.Error())
	case errors.As(err, &te) && errors.Is(te.Kind, service.ErrInvalidArguments):
		writeError(w, http.StatusBadRequest, te.Error())
	case errors.Is(err, service.ErrAssistantModel) && errors.Is(err, llm.ErrBudgetExhausted):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, service.ErrAssistantModel) && errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "The flow assistant did not finish within 3 minutes: try a shorter description or a faster model")
	case errors.Is(err, service.ErrAssistantModel):
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		h.logger.Error().Err(err).Msg("Flow assistant failed")
		writeError(w, http.StatusInternalServerError, "The flow assistant failed")
	}
}
