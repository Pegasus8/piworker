package api

import (
	"fmt"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// LLMMemoryHandler shows and clears what the LLM nodes keep (ADR-0038,
// docs/design/llm.md §6): the conversations of the memory and the
// collections of the knowledge base. Conversation keys hold ':' and '/' and
// document IDs '#' and '/': the editor path-escapes them, the router
// matches the decoded path, so their routes take the rest of the path. The
// app's router cleans paths as sent (CleanPathsAsSent): a key or ID that
// holds "//" or ".." once decoded is never redirected to another one.
type LLMMemoryHandler struct {
	memory    LLMConversations
	knowledge KnowledgeBase
	logger    zerolog.Logger
}

// NewLLMMemoryHandler creates an LLMMemoryHandler.
func NewLLMMemoryHandler(memory LLMConversations, knowledge KnowledgeBase, logger zerolog.Logger) *LLMMemoryHandler {
	return &LLMMemoryHandler{memory: memory, knowledge: knowledge, logger: logger}
}

// RegisterRoutes registers the conversation and knowledge base routes.
func (h *LLMMemoryHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/llm/conversations", h.ListConversations).Methods(http.MethodGet)
	r.HandleFunc("/api/llm/conversations/{key:.+}", h.GetConversation).Methods(http.MethodGet)
	r.HandleFunc("/api/llm/conversations/{key:.+}", h.ClearConversation).Methods(http.MethodDelete)
	r.HandleFunc("/api/llm/knowledge", h.ListCollections).Methods(http.MethodGet)
	r.HandleFunc("/api/llm/knowledge/{collection}", h.ListDocuments).Methods(http.MethodGet)
	r.HandleFunc("/api/llm/knowledge/{collection}", h.DeleteCollection).Methods(http.MethodDelete)
	r.HandleFunc("/api/llm/knowledge/{collection}/documents/{id:.+}", h.DeleteDocument).Methods(http.MethodDelete)
}

// LLMConversationsResponse is GET /api/llm/conversations' answer, sorted by
// key.
type LLMConversationsResponse struct {
	Conversations []llm.ConversationInfo `json:"conversations"`
}

// LLMConversation is one conversation's turns, oldest first (text only).
type LLMConversation struct {
	Key   string     `json:"key"`
	Turns []llm.Turn `json:"turns"`
}

// KnowledgeCollectionsResponse is GET /api/llm/knowledge's answer, sorted
// by name.
type KnowledgeCollectionsResponse struct {
	Collections []llm.CollectionInfo `json:"collections"`
}

// KnowledgeDocumentPage is a page of a collection's documents (oldest
// first; never their embeddings) and how many it holds.
type KnowledgeDocumentPage struct {
	Documents []llm.Document `json:"documents"`
	Total     int            `json:"total"`
}

// Document pages: the first page by default, at most maxDocumentPage.
const (
	defaultDocumentPage = 50
	maxDocumentPage     = 100
)

// ListConversations lists the stored conversations.
func (h *LLMMemoryHandler) ListConversations(w http.ResponseWriter, _ *http.Request) {
	convs := h.memory.List()
	if convs == nil {
		convs = []llm.ConversationInfo{}
	}
	writeSuccess(w, http.StatusOK, LLMConversationsResponse{Conversations: convs}, "")
}

// conversationKey reads the path's key: 400 when it cannot be one.
func conversationKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := mux.Vars(r)["key"]
	if !llm.ValidConversationKey(key) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid conversation key (1 to %d characters, one line)", llm.MaxConversationKeyLength))
		return "", false
	}
	return key, true
}

// GetConversation answers one conversation's turns; 404 when it has none.
func (h *LLMMemoryHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	key, ok := conversationKey(w, r)
	if !ok {
		return
	}
	turns := h.memory.Turns(key)
	if len(turns) == 0 {
		writeError(w, http.StatusNotFound, "Conversation not found")
		return
	}
	writeSuccess(w, http.StatusOK, LLMConversation{Key: key, Turns: turns}, "")
}

// ClearConversation deletes a conversation: its next message starts afresh.
func (h *LLMMemoryHandler) ClearConversation(w http.ResponseWriter, r *http.Request) {
	key, ok := conversationKey(w, r)
	if !ok {
		return
	}
	if len(h.memory.Turns(key)) == 0 {
		writeError(w, http.StatusNotFound, "Conversation not found")
		return
	}
	if err := h.memory.Clear(key); err != nil {
		h.logger.Error().Err(err).Msg("Failed to clear an LLM conversation")
		writeError(w, http.StatusInternalServerError, "Failed to clear the conversation")
		return
	}
	writeSuccess(w, http.StatusOK, nil, "Conversation cleared")
}

// ListCollections lists the knowledge base's collections.
func (h *LLMMemoryHandler) ListCollections(w http.ResponseWriter, _ *http.Request) {
	cols := h.knowledge.Collections()
	if cols == nil {
		cols = []llm.CollectionInfo{}
	}
	writeSuccess(w, http.StatusOK, KnowledgeCollectionsResponse{Collections: cols}, "")
}

// collection reads the path's collection name: 400 when it cannot be one,
// 404 when the knowledge base has no such collection.
func (h *LLMMemoryHandler) collection(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := mux.Vars(r)["collection"]
	if !llm.ValidName(name) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid collection name (1 to %d letters, digits, '_' or '-')", llm.MaxNameLength))
		return "", false
	}
	if _, ok := h.knowledge.Collection(name); !ok {
		writeError(w, http.StatusNotFound, "Collection not found")
		return "", false
	}
	return name, true
}

// queryCount reads an optional non-negative integer query parameter: 400
// for anything else (events.go's queryInt ignores it instead).
func queryCount(w http.ResponseWriter, r *http.Request, name string, fallback int) (int, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "Invalid "+name)
		return 0, false
	}
	return n, true
}

// ListDocuments answers a page of a collection's documents
// (?offset=&limit=, limit at most 100; 50 when absent or 0).
func (h *LLMMemoryHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	offset, ok := queryCount(w, r, "offset", 0)
	if !ok {
		return
	}
	limit, ok := queryCount(w, r, "limit", defaultDocumentPage)
	if !ok {
		return
	}
	if limit == 0 {
		limit = defaultDocumentPage
	}
	limit = min(limit, maxDocumentPage)
	name, ok := h.collection(w, r)
	if !ok {
		return
	}
	docs, total := h.knowledge.Documents(name, offset, limit)
	if docs == nil {
		docs = []llm.Document{}
	}
	writeSuccess(w, http.StatusOK, KnowledgeDocumentPage{Documents: docs, Total: total}, "")
}

// DeleteCollection deletes a collection and its documents.
func (h *LLMMemoryHandler) DeleteCollection(w http.ResponseWriter, r *http.Request) {
	name, ok := h.collection(w, r)
	if !ok {
		return
	}
	if err := h.knowledge.DeleteCollection(name); err != nil {
		h.logger.Error().Err(err).Str("collection", name).Msg("Failed to delete a knowledge collection")
		writeError(w, http.StatusInternalServerError, "Failed to delete the collection")
		return
	}
	writeSuccess(w, http.StatusOK, nil, "Collection deleted")
}

// DeleteDocument deletes one document of a collection (deleting one that
// is already gone succeeds: the knowledge base cannot tell it cheaply).
func (h *LLMMemoryHandler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !utf8.ValidString(id) || utf8.RuneCountInString(id) > llm.MaxDocumentIDLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid document ID (1 to %d characters)", llm.MaxDocumentIDLength))
		return
	}
	name, ok := h.collection(w, r)
	if !ok {
		return
	}
	if err := h.knowledge.Delete(name, id); err != nil {
		h.logger.Error().Err(err).Str("collection", name).Msg("Failed to delete a knowledge document")
		writeError(w, http.StatusInternalServerError, "Failed to delete the document")
		return
	}
	writeSuccess(w, http.StatusOK, nil, "Document deleted")
}
