// Package llm connects PiWorker to large language models (ADR-0038,
// docs/design/llm.md): named LLM connections defined by the admin (a
// provider, an endpoint, an API key and a default model), the provider
// clients the LLM nodes use (Anthropic through its official Go SDK; OpenAI
// and every OpenAI-compatible server, such as Ollama, LM Studio or
// OpenRouter, over HTTP), a daily token budget per connection, the local
// conversation memory and the local knowledge base (embeddings in SQLite).
//
// Like the broker connections (internal/brokers) every store keeps its data
// in memory, writes through to a Persister (the SQLite store) and is resolved
// through the node's context (FromContext, MemoryFromContext,
// KnowledgeFromContext), so a sandboxed run (dry-run, node test) sees empty
// stores. API keys may hold {{secret.NAME}} references, resolved when a
// client is built (NewClient), never returned by the API and never quoted in
// an error.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// Provider is the API a connection speaks.
type Provider string

const (
	// ProviderAnthropic is the Claude API (Messages API), through the
	// official Anthropic Go SDK.
	ProviderAnthropic Provider = "anthropic"
	// ProviderOpenAI is the OpenAI API and every server compatible with its
	// chat completions, models, embeddings and audio transcription
	// endpoints (Ollama, LM Studio, OpenRouter, LocalAI, vLLM, ...).
	ProviderOpenAI Provider = "openai"
)

// Default endpoints, used when a connection leaves BaseURL empty.
const (
	DefaultAnthropicURL = "https://api.anthropic.com"
	DefaultOpenAIURL    = "https://api.openai.com/v1"
)

// MaxNameLength bounds a connection's (and a knowledge collection's) name.
const MaxNameLength = 64

// Connection is a named LLM connection. APIKey may hold {{secret.NAME}}
// references; it is never returned by the API.
type Connection struct {
	Name     string
	Provider Provider
	// BaseURL overrides the provider's default endpoint (an http or https
	// URL with a host and an optional path; no credentials, query or
	// fragment), e.g. http://192.168.1.20:11434/v1 for Ollama.
	BaseURL string
	APIKey  string
	// Model is the default model of the nodes that name no model.
	Model string
	// DailyTokenLimit caps the input plus output tokens the connection may
	// use per local calendar day (0: no limit).
	DailyTokenLimit int64
}

// Usage counts what a connection used.
type Usage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	Requests     int64 `json:"requests"`
}

// Total is the tokens a budget counts: input plus output.
func (u Usage) Total() int64 { return u.InputTokens + u.OutputTokens }

// Role is the author of a conversation message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Media is an image (or audio) sent to a model: inline bytes with their media
// type, or a public http(s) URL the provider fetches itself.
type Media struct {
	MediaType string // e.g. image/png, audio/mpeg
	Data      []byte // inline content (nil when URL is set)
	URL       string // http(s) URL (only for images)
}

// Tool is a function the model may call (an agent node's flow tool).
type Tool struct {
	Name        string
	Description string
	// InputSchema is the JSON Schema of the arguments (an object schema).
	InputSchema json.RawMessage
}

// ToolCall is a call the model asked for.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage // a JSON object
}

// ToolResult answers a ToolCall.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// Message is one message of a request's conversation. A user message holds
// Text (and Images) or ToolResults; an assistant message holds Text and
// ToolCalls. Native, when set, is the provider's own form of an assistant
// message a client returned (Response.Message): clients replay it unchanged,
// so provider-specific content (Claude's thinking blocks) survives a tool
// loop. Messages built from stored memory have no Native.
type Message struct {
	Role        Role
	Text        string
	Images      []Media
	ToolCalls   []ToolCall
	ToolResults []ToolResult
	Native      any
}

// StopReason says why a model stopped.
type StopReason string

const (
	StopEnd       StopReason = "end"        // finished its answer
	StopMaxTokens StopReason = "max_tokens" // hit MaxTokens (the answer is cut)
	StopToolUse   StopReason = "tool_use"   // asks for ToolCalls
	StopRefusal   StopReason = "refusal"    // declined (Response is never returned: ErrRefused)
	StopOther     StopReason = "other"
)

// Request is one chat request.
type Request struct {
	Model    string
	System   string
	Messages []Message
	Tools    []Tool
	// MaxTokens caps the answer (required, > 0).
	MaxTokens int
	// Schema, when set, asks for a JSON answer matching this JSON Schema
	// (structured outputs); the client validates the answer against it
	// (ValidateOutput) and puts the parsed value in Response.JSON.
	Schema map[string]any
	// JSON asks for a JSON object answer without a schema: the client
	// instructs the model and parses the answer (ParseJSONAnswer).
	JSON bool
	// Effort is "", "low", "medium", "high", "xhigh" or "max": Anthropic's
	// output_config.effort; OpenAI's reasoning_effort (xhigh and max are
	// sent as high). "" leaves the model's default.
	Effort string
	// Cache asks Anthropic to cache the request's prefix (automatic prompt
	// caching): set for tool loops and conversations with memory, which send
	// the same prefix again. Ignored by OpenAI-compatible servers.
	Cache bool
}

// Response is a chat answer.
type Response struct {
	Text      string
	JSON      any // the parsed answer when Request.Schema or Request.JSON was set
	ToolCalls []ToolCall
	Stop      StopReason
	Model     string // the model that answered, as the provider reports it
	Usage     Usage  // Requests is 1
	// Message is the answer as an assistant Message (with Native set) to
	// append to the conversation of the next request of a tool loop.
	Message Message
}

// ModelInfo is a model a connection offers.
type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"` // display name, when the provider has one
}

// Embedding is one embedded input.
type Embedding []float32

// Client talks to one connection's provider. Built with NewClient, every
// method that spends tokens (Chat, Embed, Transcribe) checks the
// connection's daily budget first (CheckBudget) and records what it used
// (Store.RecordUsage); Models spends none and is neither checked nor
// recorded, so a connection can be tested with its budget spent.
type Client interface {
	// Chat sends one chat request.
	Chat(ctx context.Context, req Request) (*Response, error)
	// Models lists the models the connection offers (spends no tokens).
	Models(ctx context.Context) ([]ModelInfo, error)
	// Embed embeds the inputs, in order (ErrUnsupported on Anthropic).
	Embed(ctx context.Context, model string, inputs []string) ([]Embedding, Usage, error)
	// Transcribe turns audio into text (ErrUnsupported on Anthropic).
	// language is an optional ISO-639-1 code.
	Transcribe(ctx context.Context, model string, audio Media, language string) (string, error)
}

// Errors.
var (
	// ErrUndefined: a node names a connection its context's store does not
	// define (deploy refuses this first: undefined_llm_connection).
	ErrUndefined = errors.New("LLM connection is not defined")
	// ErrUnsupported: the provider does not offer the feature (embeddings or
	// transcription on Anthropic).
	ErrUnsupported = errors.New("not supported by this provider")
	// ErrBudgetExhausted: the connection's daily token limit is spent.
	ErrBudgetExhausted = errors.New("the connection's daily token limit is spent")
	// ErrRefused: the model declined the request (a refusal stop reason or
	// a content filter).
	ErrRefused = errors.New("the model declined the request")
	// ErrInvalidAnswer: the answer is not the JSON asked for, or does not
	// match the schema.
	ErrInvalidAnswer = errors.New("the model's answer is not valid")
	// ErrNoModel: neither the node nor the connection names a model.
	ErrNoModel = errors.New("no model: set one on the node or on the connection")
)

// APIError is an error status a provider answered. Message is the
// provider's error message, capped and with the API key removed.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return "LLM API error " + strconv.Itoa(e.Status)
	}
	return "LLM API error " + strconv.Itoa(e.Status) + ": " + e.Message
}

// Turn is one message of a stored conversation (memory).
type Turn struct {
	Role Role      `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// ConversationInfo summarizes a stored conversation.
type ConversationInfo struct {
	Key       string    `json:"key"`
	Turns     int       `json:"turns"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Document is one entry of the knowledge base.
type Document struct {
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Model     string         `json:"model"`
	Embedding Embedding      `json:"-"` // normalized to unit length
	CreatedAt time.Time      `json:"createdAt"`
}

// Hit is a knowledge-base search result.
type Hit struct {
	Document
	Score float64 `json:"score"` // cosine similarity, -1..1
}

// CollectionInfo summarizes a knowledge collection.
type CollectionInfo struct {
	Name       string    `json:"name"`
	Documents  int       `json:"documents"`
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
