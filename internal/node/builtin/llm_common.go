package builtin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// Shared settings and helpers of the LLM nodes (ADR-0038,
// docs/design/llm.md §3): process-llm, process-llm-classify,
// process-llm-agent, action-llm-forget, process-transcribe,
// action-knowledge-add and process-knowledge-search.
//
// A node that calls a model typically:
//
//  1. declares its settings with the llmSchema* helpers below (the markers
//     the schema policy tests and the low agent-deploy policy rely on are
//     set there, once);
//  2. reads them in its factory with parseLLMSettings (connection, model,
//     timeout) plus parseLLMMaxTokens, parseLLMEffort, parseLLMMemory and
//     parseResultField as it needs, so a bad config fails the deploy;
//  3. embeds llmSettings, which makes it a node.LLMConnectionRequirer;
//  4. in Process, runs its whole call inside settings.run (the node's
//     timeout) and resolves the connection, the model and a client there
//     with settings.open: through the context, never in the factory, so a
//     changed connection applies to the next message and a sandboxed run
//     (dry-run, node test) never reaches a provider;
//  5. records llmMeta (meta.llm) on the message it emits, and stores its
//     result with llm.SetResult(out, resultField, value).
//
// Errors are returned as llm gives them: transient ones (network errors,
// 429, 5xx) stay wrapped with node.Transient so the runtime's opt-in
// retries apply; refusals, a spent budget, invalid answers and bad
// requests are permanent.

// Bounds of the shared settings.
const (
	// llmMaxTokensLimit bounds maxTokens (the answer's length).
	llmMaxTokensLimit = 64000
	// llmMaxTimeout bounds timeout, in seconds; llmDefaultTimeout is the
	// default of most nodes (the agent's is longer: it runs tools).
	llmMaxTimeout     = 600
	llmDefaultTimeout = 120
	// llmMinMemoryTurns..llmMaxMemoryTurns bound memoryTurns (llm.MaxTurns
	// is what the memory keeps at most); llmDefaultMemoryTurns is the
	// default.
	llmMinMemoryTurns     = 2
	llmMaxMemoryTurns     = llm.MaxTurns
	llmDefaultMemoryTurns = 20
	// llmMaxModelLength bounds a model name (as llm.Connection does).
	llmMaxModelLength = 256
	// llmMaxResultField bounds resultField.
	llmMaxResultField = 128
)

// llmEfforts are the values of an effort setting ("" = the model's default).
var llmEfforts = []string{"", "low", "medium", "high", "xhigh", "max"}

// Ports of the single-output LLM nodes.
var (
	llmInputs  = []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}}
	llmOutputs = []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
)

// ---------------------------------------------------------------------------
// Schema helpers: every LLM node declares these settings the same way.

func llmSchemaConnection() node.ConfigProperty {
	return node.ConfigProperty{Type: "string", Title: "Connection",
		Description:  "LLM connection to use (defined under LLM connections)",
		AgentVisible: true, Destination: true, Options: node.OptionsLLMConnections}
}

// llmSchemaModel is the model setting; description says what kind of model
// (a chat, embedding or transcription model) and what an empty value means.
func llmSchemaModel(description string) node.ConfigProperty {
	return node.ConfigProperty{Type: "string", Title: "Model", Description: description,
		AgentVisible: true, Options: node.OptionsLLMModels}
}

// llmSchemaTimeout is the timeout setting, in seconds, bounding the node's
// whole call (def is its default).
func llmSchemaTimeout(def int) node.ConfigProperty {
	one, max := float64(1), float64(llmMaxTimeout)
	return node.ConfigProperty{Type: "integer", Title: "Timeout (s)",
		Description: fmt.Sprintf("Longest time the whole call may take, in seconds (1 to %d)", llmMaxTimeout),
		Default:     def, Minimum: &one, Maximum: &max, Unit: "seconds", AgentVisible: true}
}

func llmSchemaMaxTokens(def int) node.ConfigProperty {
	one, max := float64(1), float64(llmMaxTokensLimit)
	return node.ConfigProperty{Type: "integer", Title: "Max tokens",
		Description: fmt.Sprintf("Longest answer the model may give, in tokens (1 to %d); a longer answer is cut", llmMaxTokensLimit),
		Default:     def, Minimum: &one, Maximum: &max, Unit: "tokens", AgentVisible: true}
}

func llmSchemaEffort() node.ConfigProperty {
	return node.ConfigProperty{Type: "string", Title: "Effort",
		Description: "How much the model thinks before answering (empty: the model's default; models without the setting ignore it)",
		Default:     "", Enum: llmEfforts, AgentVisible: true}
}

func llmSchemaMemoryKey() node.ConfigProperty {
	return node.ConfigProperty{Type: "string", Title: "Memory key",
		Description:  "Conversation to remember, e.g. telegram:{{payload.message.chat.id}} (supports {{templates}}; empty: no memory)",
		AgentVisible: true}
}

func llmSchemaMemoryTurns() node.ConfigProperty {
	min, max := float64(llmMinMemoryTurns), float64(llmMaxMemoryTurns)
	return node.ConfigProperty{Type: "integer", Title: "Memory length",
		Description: fmt.Sprintf("How many past messages of the conversation to keep and send, in messages (%d to %d)", llmMinMemoryTurns, llmMaxMemoryTurns),
		Default:     llmDefaultMemoryTurns, Minimum: &min, Maximum: &max, Unit: "messages", AgentVisible: true}
}

func llmSchemaResultField() node.ConfigProperty {
	return node.ConfigProperty{Type: "string", Title: "Result field",
		Description:  "Payload field to store the result in, e.g. answer or ai.summary (empty: the result replaces the payload)",
		AgentVisible: true}
}

// ---------------------------------------------------------------------------
// Settings.

// llmSettings are the settings of a node that calls a model: its
// connection's name, the model (empty: the connection's) and the timeout of
// its whole call. Embedding it makes a node a node.LLMConnectionRequirer.
type llmSettings struct {
	connection string
	model      string
	timeout    time.Duration
}

// parseLLMSettings reads connection (required), model and timeout (whole
// seconds, 1 to llmMaxTimeout, default defTimeout).
func parseLLMSettings(base *node.BaseNode, defTimeout int) (llmSettings, error) {
	cfg := base.GetConfig()
	name, err := textSetting(cfg, "connection")
	if err != nil {
		return llmSettings{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return llmSettings{}, errors.New("connection is required (define one under LLM connections)")
	}
	if !llm.ValidName(name) {
		return llmSettings{}, errors.New("connection must be the name of an LLM connection (letters, digits, '_' or '-')")
	}
	model, err := textSetting(cfg, "model")
	if err != nil {
		return llmSettings{}, err
	}
	model = strings.TrimSpace(model)
	if utf8.RuneCountInString(model) > llmMaxModelLength || strings.ContainsAny(model, "\x00\r\n") {
		return llmSettings{}, fmt.Errorf("model must be one line of at most %d characters", llmMaxModelLength)
	}
	seconds, err := intSetting(cfg, "timeout", defTimeout, 1, llmMaxTimeout, "seconds")
	if err != nil {
		return llmSettings{}, err
	}
	return llmSettings{connection: name, model: model, timeout: time.Duration(seconds) * time.Second}, nil
}

// RequiredLLMConnections implements node.LLMConnectionRequirer: deploy
// refuses the flow while the connection is not defined.
func (s llmSettings) RequiredLLMConnections() map[string]string {
	return map[string]string{"connection": s.connection}
}

// llmSession is what a node calls a model through: the connection resolved
// through its context, the model to ask and a client of the connection
// (which checks the daily budget and records the usage).
type llmSession struct {
	conn   llm.Connection
	model  string
	client llm.Client
}

// open resolves the connection through ctx (llm.FromContext), the model
// (the node's, else the connection's, else fallbackModel; none is
// llm.ErrNoModel) and a client (llm.NewClient). A sandboxed context gets
// llm.ErrSandboxed before anything is looked up: LLM calls never run in a
// dry-run or a node test.
func (s llmSettings) open(ctx context.Context, fallbackModel string) (llmSession, error) {
	if node.InSandbox(ctx) {
		return llmSession{}, llm.ErrSandboxed
	}
	conn, err := llm.Lookup(ctx, s.connection)
	if err != nil {
		return llmSession{}, err
	}
	model := s.model
	if model == "" {
		model = conn.Model
	}
	if model == "" {
		model = fallbackModel
	}
	if model == "" {
		return llmSession{}, fmt.Errorf("LLM connection %q: %w", conn.Name, llm.ErrNoModel)
	}
	client, err := llm.NewClient(ctx, conn)
	if err != nil {
		return llmSession{}, err
	}
	return llmSession{conn: conn, model: model, client: client}, nil
}

// run calls fn with ctx bounded by the node's timeout. When the timeout
// (not the caller's context) ends the call, the error says so; it wraps
// context.DeadlineExceeded and is permanent (a retry would take as long).
func (s llmSettings) run(ctx context.Context, fn func(ctx context.Context) error) error {
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	err := fn(callCtx)
	if err != nil && ctx.Err() == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("the LLM call did not finish within %s (the node's timeout): %w", secondsText(s.timeout), context.DeadlineExceeded)
	}
	return err
}

// secondsText renders a whole number of seconds: "1 second", "120 seconds".
func secondsText(d time.Duration) string {
	n := int64(d / time.Second)
	if n == 1 {
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", n)
}

// parseLLMMaxTokens reads maxTokens (1 to llmMaxTokensLimit, default def).
func parseLLMMaxTokens(base *node.BaseNode, def int) (int, error) {
	return intSetting(base.GetConfig(), "maxTokens", def, 1, llmMaxTokensLimit, "tokens")
}

// parseLLMEffort reads effort: one of llmEfforts.
func parseLLMEffort(base *node.BaseNode) (string, error) {
	effort, err := textSetting(base.GetConfig(), "effort")
	if err != nil {
		return "", err
	}
	if !containsString(llmEfforts, effort) {
		return "", errors.New("effort must be low, medium, high, xhigh or max (or empty for the model's default)")
	}
	return effort, nil
}

// resultFieldRe is a payload field path: names of letters, digits, '_' or
// '-' separated by dots.
var resultFieldRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

// parseResultField reads resultField: "" (the result replaces the payload)
// or a payload field path such as answer or ai.summary (llm.SetResult).
func parseResultField(base *node.BaseNode) (string, error) {
	field, err := textSetting(base.GetConfig(), "resultField")
	if err != nil {
		return "", err
	}
	if field == "" {
		return "", nil
	}
	if len(field) > llmMaxResultField || !resultFieldRe.MatchString(field) {
		return "", fmt.Errorf("resultField must be a payload field such as answer or ai.summary (names of letters, digits, '_' or '-' separated by dots, at most %d characters)", llmMaxResultField)
	}
	return field, nil
}

// messagePathRe is a message path llm.MessageValue reads: payload and its
// fields, meta.<key> and its fields, or topic.
var messagePathRe = regexp.MustCompile(`^(payload(\.[^.\s{}]+)*|meta(\.[^.\s{}]+)+|topic)$`)

// checkMessagePath checks a setting holding a message path (images, audio):
// payload, payload.<field>[.<field>…] (list items by index, e.g.
// payload.items.0), meta.<key>[.<field>…] or topic.
func checkMessagePath(setting, path string) error {
	if !messagePathRe.MatchString(path) {
		return fmt.Errorf("%s must be a message path such as payload, payload.image, payload.items.0 or meta.photo", setting)
	}
	return nil
}

// textSetting reads an optional text setting: "" when absent or null, an
// error when it is not text.
func textSetting(cfg map[string]any, key string) (string, error) {
	switch v := cfg[key].(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	default:
		return "", fmt.Errorf("%s must be text", key)
	}
}

// templateSetting reads a template setting that may not be empty: def when
// absent or null, an error when it is set to blank text.
func templateSetting(cfg map[string]any, key, def string) (string, error) {
	v, present := cfg[key]
	if !present || v == nil {
		return def, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be text", key)
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return s, nil
}

// boolSetting reads an optional boolean setting (false when absent).
func boolSetting(cfg map[string]any, key string) (bool, error) {
	switch v := cfg[key].(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	default:
		return false, fmt.Errorf("%s must be true or false", key)
	}
}

// stringList reads a list-of-text setting ([]any as decoded from JSON, or
// []string). Absent or null is nil; anything else is an error.
func stringList(cfg map[string]any, key string) ([]string, error) {
	switch v := cfg[key].(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), v...), nil
	case []any:
		out := make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s: item %d is not text", key, i+1)
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be a list", key)
	}
}

// ---------------------------------------------------------------------------
// Memory (conversations, llm.Memory).

// llmMemory is a node's conversation memory: the key template (empty: no
// memory) and how many turns to keep and send.
type llmMemory struct {
	key   string
	turns int
}

// parseLLMMemory reads memoryKey (a template) and memoryTurns
// (llmMinMemoryTurns to llmMaxMemoryTurns, default llmDefaultMemoryTurns).
func parseLLMMemory(base *node.BaseNode) (llmMemory, error) {
	cfg := base.GetConfig()
	key, err := textSetting(cfg, "memoryKey")
	if err != nil {
		return llmMemory{}, err
	}
	turns, err := intSetting(cfg, "memoryTurns", llmDefaultMemoryTurns, llmMinMemoryTurns, llmMaxMemoryTurns, "messages")
	if err != nil {
		return llmMemory{}, err
	}
	return llmMemory{key: strings.TrimSpace(key), turns: turns}, nil
}

// enabled reports whether the node remembers conversations.
func (m llmMemory) enabled() bool { return m.key != "" }

// conversation renders the key for msg and returns it with the stored
// history of that conversation (its last m.turns turns, as request
// messages that start with the user). Without memory both are empty.
func (m llmMemory) conversation(ctx context.Context, msg *types.Message) (string, []llm.Message, error) {
	if !m.enabled() {
		return "", nil, nil
	}
	key, err := renderMemoryKey(ctx, m.key, msg)
	if err != nil {
		return "", nil, err
	}
	turns := llm.MemoryFromContext(ctx).Turns(key)
	if len(turns) > m.turns {
		turns = turns[len(turns)-m.turns:]
	}
	return key, llm.MessagesFromTurns(turns), nil
}

// remember appends a successful exchange to the conversation key, keeping
// its last m.turns turns. A failure is logged, not returned: the answer was
// paid for and is passed on; only the next message loses this exchange.
func (m llmMemory) remember(ctx context.Context, key, prompt, answer string) {
	if key == "" {
		return
	}
	err := llm.MemoryFromContext(ctx).Append(key, m.turns,
		llm.Turn{Role: llm.RoleUser, Text: prompt},
		llm.Turn{Role: llm.RoleAssistant, Text: answer})
	if err != nil {
		log.Warn().Err(err).Msg("llm: the conversation was not saved")
	}
}

// renderMemoryKey renders a memoryKey template for msg and checks the key
// (llm.ValidConversationKey). The rendered key is never quoted: it is
// message data.
func renderMemoryKey(ctx context.Context, tmpl string, msg *types.Message) (string, error) {
	key := strings.TrimSpace(renderTemplate(tmpl, msg, ctx))
	if key == "" {
		return "", errors.New("memoryKey rendered to an empty key for this message")
	}
	if !llm.ValidConversationKey(key) {
		return "", fmt.Errorf("memoryKey rendered to an invalid conversation key for this message (1 to %d characters, one line)", llm.MaxConversationKeyLength)
	}
	return key, nil
}

// ---------------------------------------------------------------------------
// Results.

// llmMeta is the meta.llm of an answer: the connection, the model that
// answered (as the provider reports it), the tokens and requests spent and
// why the model stopped. Nodes add their own keys (category, steps, …).
func llmMeta(connection, model string, u llm.Usage, stop llm.StopReason) map[string]any {
	return map[string]any{
		"connection":   connection,
		"model":        model,
		"inputTokens":  u.InputTokens,
		"outputTokens": u.OutputTokens,
		"requests":     u.Requests,
		"stop":         string(stop),
	}
}

// addUsage sums two usages.
func addUsage(a, b llm.Usage) llm.Usage {
	return llm.Usage{InputTokens: a.InputTokens + b.InputTokens, OutputTokens: a.OutputTokens + b.OutputTokens, Requests: a.Requests + b.Requests}
}

// ---------------------------------------------------------------------------
// Providers without a feature.

// unsupportedOnAnthropic refuses, before anything is fetched or sent, a
// feature (embeddings, transcription) on an Anthropic connection: the
// Claude API has none of them. alternatives names the connections that
// offer it. The error wraps llm.ErrUnsupported (permanent).
func unsupportedOnAnthropic(conn llm.Connection, feature, alternatives string) error {
	if conn.Provider != llm.ProviderAnthropic {
		return nil
	}
	return fmt.Errorf("%s: %w: LLM connection %q is an Anthropic connection, which has no %s API; use an OpenAI-compatible connection such as %s",
		feature, llm.ErrUnsupported, conn.Name, feature, alternatives)
}

// ---------------------------------------------------------------------------
// Knowledge base (action-knowledge-add, process-knowledge-search).

// llmEmbeddingProviders names the connections that offer embeddings.
const llmEmbeddingProviders = "OpenAI or Ollama"

// llmSchemaCollection is a knowledge collection setting (description says
// what the node does with it).
func llmSchemaCollection(description string) node.ConfigProperty {
	return node.ConfigProperty{Type: "string", Title: "Collection", Description: description,
		AgentVisible: true, Options: node.OptionsKnowledgeCollections}
}

// parseKnowledgeCollection reads collection (required): a collection name
// (llm.ValidName). It is not a template: a flow names its collections, so
// the editor and the approval page show where documents go.
func parseKnowledgeCollection(base *node.BaseNode) (string, error) {
	name, err := textSetting(base.GetConfig(), "collection")
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("collection is required: the knowledge collection, e.g. manuals")
	}
	if !llm.ValidName(name) {
		return "", fmt.Errorf("collection must be a collection name (1 to %d letters, digits, '_' or '-')", llm.MaxNameLength)
	}
	return name, nil
}

// knowledgeCollection returns the summary of the collection name of kb;
// false when it holds no document.
func knowledgeCollection(kb *llm.Knowledge, name string) (llm.CollectionInfo, bool) {
	for _, c := range kb.Collections() {
		if c.Name == name {
			return c, c.Documents > 0
		}
	}
	return llm.CollectionInfo{}, false
}

// checkCollectionModel refuses, before any token is spent, embeddings of
// model for a collection that holds another model's (they cannot be
// compared: llm.ErrModelMismatch). A new or empty collection takes any
// model. The dimensions are checked when the embeddings are stored or
// searched.
func checkCollectionModel(kb *llm.Knowledge, name, model string) error {
	info, ok := knowledgeCollection(kb, name)
	if !ok || info.Model == model {
		return nil
	}
	return fmt.Errorf("%w: collection %q holds embeddings of %q, not %q (set the node's model to %q, or delete the collection under AI memory to rebuild it)",
		llm.ErrModelMismatch, name, info.Model, model, info.Model)
}
