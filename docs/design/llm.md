# LLM nodes, connections, memory, knowledge base and the flow assistant

Status: implemented with ADR-0038 … ADR-0041. This document is the design
those ADRs link to; it is also the contract the code follows.

PiWorker already lets agents call flows (flows as MCP tools, ADR-0009). This
design adds the other direction: flows that call large language models.

- **LLM connections**: named connections the admin defines once (provider,
  endpoint, API key, default model, daily token limit), referenced by name
  from the nodes. Providers: `anthropic` (the Claude API, through the
  official Anthropic Go SDK) and `openai` (OpenAI and every OpenAI-compatible
  server: Ollama, LM Studio, OpenRouter, LocalAI, vLLM, …). A local model on
  the Pi or the LAN is an `openai` connection with a `baseUrl`.
- **Nodes**: `process-llm` (prompt → text or JSON, optional images and
  conversation memory), `process-llm-classify` (one output port per
  category), `process-llm-agent` (a model that calls the admin's flow tools
  in a loop), `action-llm-forget` (clears a conversation),
  `process-transcribe` (audio → text), `action-knowledge-add` and
  `process-knowledge-search` (a local knowledge base: embeddings in SQLite,
  cosine search).
- **Memory** (conversation history per key) and the **knowledge base** live
  in PiWorker's own database; the editor shows and clears them.
- **The flow assistant**: the editor turns a description into a proposed
  flow (validated with `flow.Lint`), which the admin previews and applies to
  the canvas. It never saves or deploys anything.
- **Editor support**: list settings, outputs derived from a setting (the
  classifier's categories become ports), suggestions for connections,
  models, tools and collections, and dialogs for connections, memory and
  the knowledge base.

## 1. Package `internal/llm`

Contracts are in `internal/llm/types.go`. Files:

| File | Contents |
|---|---|
| `types.go` | Provider, Connection, Usage, Message/Request/Response, Client, errors, Turn, Document, … |
| `connections.go` | `Connection.Normalize/Validate/Endpoint`, `ValidName`, `Store` (connections + usage), `Persister`, `DefaultStore`, `WithStore`/`FromContext`, `Lookup`, `SecretReferences`, `resolvedKey` |
| `client.go` | `NewClient(ctx, c) (Client, error)`, the budget-and-usage wrapper, HTTP client, error classification and key redaction |
| `anthropic.go` | the Anthropic client (SDK) |
| `openai.go` | the OpenAI-compatible client (net/http, JSON) |
| `schema.go` | `PrepareSchema`, `ValidateOutput`, `ParseJSONAnswer` |
| `media.go` | `LoadMedia` (base64, data URL, http(s) URL, bytes) |
| `message.go` | `MessageValue`, `SetResult` (message paths and result fields for the nodes) |
| `memory.go` | `Memory` store (conversations) |
| `knowledge.go` | `Knowledge` store (documents, embeddings, search), `Chunk` |

`internal/llm` imports `internal/node` (for `node.Transient`, `node.Sandbox`),
`internal/secrets` and `internal/types`; never `internal/flow` or
`internal/service`.

### 1.1 Connections

- Name: 1–64 letters, digits, `_`, `-` (as broker connections).
- `Provider`: `anthropic` or `openai` (`Normalize` lower-cases and trims).
- `BaseURL`: optional; `http` or `https`, a host, an optional path, no
  userinfo, query or fragment. A trailing `/` is trimmed. `Endpoint()`
  returns it or the provider's default (`DefaultAnthropicURL`,
  `DefaultOpenAIURL`).
- `APIKey`: required for `anthropic`; optional for `openai` (local servers
  need none). May hold `{{secret.NAME}}` references. `resolvedKey(ctx)`
  expands them strictly through `secrets.FromContext(ctx)`: an undefined
  secret is an error naming the secret (never a value).
- `Model`: optional default model.
- `DailyTokenLimit`: ≥ 0 (0 = none).
- `Validate` errors never quote the key.

`Store` mirrors `brokers.Store`: `NewStore`, `Attach(Persister)`, `Get`,
`Set` (normalizes, validates, writes through first), `Delete`, `Names`,
`List`, plus usage:

- `RecordUsage(name string, u Usage)`: adds to today's counters (local
  calendar day, `2006-01-02`, from `s.now`) in memory and through
  `Persister.AddLLMUsage`; a persistence failure is logged by the caller,
  never fails the node (the call already happened).
- `UsageToday(name) Usage`: today's counters (loaded from
  `Persister.LLMUsage` the first time a day is asked for).
- `CheckBudget(c Connection) error`: `ErrBudgetExhausted` (wrapped, saying
  the limit) when `c.DailyTokenLimit > 0` and today's total ≥ the limit.

`Persister`:

```go
type Persister interface {
	LoadLLMConnections() (map[string]Connection, error)
	SaveLLMConnection(c Connection) error
	DeleteLLMConnection(name string) error
	AddLLMUsage(day, name string, u Usage) error
	LLMUsage(day, name string) (Usage, error)
}
```

`DefaultStore`, `WithStore(ctx, s)` (nil → an empty store: isolation fails
closed), `FromContext(ctx)`, `Lookup(ctx, name)` (wraps `ErrUndefined`),
`SecretReferences(c) []SecretReference{Name, Setting: "apiKey"}`.

### 1.2 Clients

`NewClient(ctx, c Connection) (Client, error)` resolves the key and returns
a client that, for every call: checks the budget of the connection in
`FromContext(ctx)` (`CheckBudget` with the stored connection's limit),
runs the call, and records its `Usage` (`RecordUsage`), also when the call
then fails validation (tokens were spent). A sandboxed context
(`node.InSandbox(ctx)`) gets an error: LLM calls never run in a dry-run or
node test (the nodes are not `safe`, so `Registry.Derive` stubs them anyway).

HTTP: one `http.Client` per client with no overall timeout (the caller's
context bounds a call), dial timeout 10 s, TLS handshake 10 s, response
header timeout 120 s (local models can be slow to start), honouring
`HTTP(S)_PROXY`. Private and loopback addresses are allowed: the endpoint is
the admin's (agents only choose a connection's name). Response bodies are
read through `io.LimitReader` (chat 8 MiB, models 4 MiB, embeddings 64 MiB).

Errors:

- Provider status errors become `*APIError{Status, Message}`; the message is
  the provider's `error.message` (or the body's first 512 bytes), capped at
  512 bytes, with every occurrence of the resolved API key replaced by
  `<redacted>`.
- Transient (wrapped with `node.Transient`, so the runtime's opt-in retries
  apply): network errors, 408, 409, 425, 429, 500, 502, 503, 504, 529.
  Everything else is permanent.
- The key is never part of an error, a log line or a returned value.

`Chat`:

- `MaxTokens` must be > 0. `Model` must be set (`ErrNoModel`).
- `Schema` set: the client sends it as structured output (below), parses the
  answer and validates it (`ValidateOutput`); a mismatch is
  `ErrInvalidAnswer` (permanent). A `StopMaxTokens` answer with a schema or
  `JSON` is `ErrInvalidAnswer` ("the answer was cut at maxTokens").
- `JSON` set (no schema): the system prompt gets "Answer with one JSON
  object and nothing else." appended; the answer is parsed with
  `ParseJSONAnswer` (trims, strips a ```json fence, takes the first JSON
  object or array).
- A refusal (Anthropic `stop_reason: refusal`; OpenAI `finish_reason:
  content_filter` or a `refusal` field) returns an error wrapping
  `ErrRefused` (with the category or explanation when present; permanent).

**Anthropic** (`anthropic.go`, `github.com/anthropics/anthropic-sdk-go/v2`):

- Client options: `option.WithAPIKey`, `option.WithBaseURL(Endpoint())`,
  `option.WithMaxRetries(0)` (retries are the flow runtime's, opt-in),
  `option.WithHTTPClient`.
- Requests use streaming and accumulate the final message
  (`Messages.NewStreaming` + `Message.Accumulate`): large `max_tokens` values
  must stream to avoid HTTP timeouts.
- `System` as one text block; messages map to `MessageParam`s; an assistant
  message with `Native` (an `anthropic.MessageParam` from `resp.ToParam()`)
  is replayed unchanged (thinking blocks are kept, as tool loops require);
  tool results are `tool_result` blocks in one user message; images are
  base64 or URL image blocks.
- `Schema` → `OutputConfig.Format` (`JSONOutputFormatParam`); `Effort` →
  `OutputConfig.Effort`; `Cache` → the request's top-level automatic
  `cache_control`.
- No `temperature`, `top_p`, `top_k` or `thinking` (the model's defaults;
  sampling parameters are rejected by current API versions).
- Tools: `ToolParam{Name, Description, InputSchema}` (properties, required,
  … taken from the tool's schema).
- `Models`: `Models.ListAutoPaging` (ID and display name).
- `Embed`, `Transcribe`: `ErrUnsupported` ("Anthropic has no embeddings
  API; use an OpenAI-compatible connection such as OpenAI or Ollama").

**OpenAI-compatible** (`openai.go`, plain JSON over HTTP; `Authorization:
Bearer <key>` only when a key is set):

- `POST {endpoint}/chat/completions` with `model`, `messages` (a `system`
  message first; user content as a string, or parts `text` / `image_url`
  with a data URL or the URL; assistant `tool_calls`; one `tool` message per
  tool result), `tools` (`{type: "function", function: {name, description,
  parameters}}`), `max_completion_tokens` when the endpoint is the default
  OpenAI one and `max_tokens` otherwise (the field local servers know),
  `reasoning_effort` when `Effort` is set (`xhigh`/`max` → `high`),
  `response_format: {type: "json_schema", json_schema: {name: "answer",
  schema, strict}}` with `strict` true only when every object in the schema
  lists all its properties as required, and `{type: "json_object"}` for
  `JSON`. `finish_reason`: `stop` → end, `length` → max_tokens,
  `tool_calls` → tool_use, `content_filter` → refused.
- `GET {endpoint}/models` → `data[].id`.
- `POST {endpoint}/embeddings` `{model, input: [...]}` → `data[].embedding`
  ordered by `index`; usage `prompt_tokens`.
- `POST {endpoint}/audio/transcriptions` (multipart: `file`, `model`,
  optional `language`, `response_format=json`) → `{text}`; at most 25 MiB.
  Transcription spends no tokens: it counts one request.

### 1.3 Structured output helpers (`schema.go`)

- `PrepareSchema(schema map[string]any) (map[string]any, error)`: a deep
  copy where every object schema without `additionalProperties` gets
  `false` (structured outputs require closed objects); the root must be an
  object schema; at most 64 KiB once encoded.
- `ValidateOutput(schema map[string]any, value any) error`: resolves and
  validates with `github.com/google/jsonschema-go` (already a dependency);
  errors wrap `ErrInvalidAnswer` and quote at most 512 bytes.
- `StrictCompatible(schema) bool`: every object lists all its properties as
  required (OpenAI `strict`).
- `ParseJSONAnswer(text string) (any, error)`.

### 1.4 Media (`media.go`)

`LoadMedia(ctx, value any, maxBytes int64, allowURL bool) ([]Media, error)`:
`value` is a string or a list of strings (or `[]byte`). A string is a data
URL (`data:<type>;base64,...`), an `http(s)` URL (kept as `URL` for images
when `allowURL`; fetched for audio by the caller through the guarded HTTP
client of the nodes), or raw base64 (the media type sniffed with
`http.DetectContentType`). Over `maxBytes` is an error. Images are limited to
image/png, image/jpeg, image/gif, image/webp.

### 1.5 Message helpers (`message.go`)

- `MessageValue(msg *types.Message, path string) (any, bool)`: `payload`,
  `payload.a.b`, `payload.items.0`, `meta.x`, `topic`.
- `SetResult(msg *types.Message, field string, value any)`: `field == ""`
  replaces the payload; otherwise sets `payload[field]` (dots are a path),
  turning a non-object payload into `{"input": <old payload>, field: value}`.

### 1.6 Memory (`memory.go`)

A conversation is a key (1–200 characters, rendered by the node from a
template, e.g. `telegram:{{payload.chat.id}}`) and its turns.

```go
type MemoryPersister interface {
	LoadLLMConversations() (map[string][]Turn, error)
	SaveLLMConversation(key string, turns []Turn) error
	DeleteLLMConversation(key string) error
}
```

`Memory`: `NewMemory`, `Attach`, `Turns(key) []Turn` (a copy),
`Append(key string, keep int, turns ...Turn) error` (appends, keeps the last
`keep` turns, trims each text to 32 KiB, writes through), `Clear(key)`,
`List() []ConversationInfo` (by key), and caps: at most 200 turns per
conversation, at most 5 000 conversations (adding one past it evicts the
least recently updated). `DefaultMemory`, `WithMemory(ctx, m)` (nil → empty),
`MemoryFromContext(ctx)`. Only text turns are stored (no images, no tool
calls): a conversation replays as alternating user/assistant messages; a
history that would start with an assistant turn drops it.

### 1.7 Knowledge base (`knowledge.go`)

```go
type KnowledgePersister interface {
	LoadLLMDocuments() (map[string][]Document, error)
	SaveLLMDocument(collection string, d Document) error
	DeleteLLMDocument(collection, id string) error
	DeleteLLMCollection(collection string) error
}
```

`Knowledge`: `NewKnowledge`, `Attach`, `Add(collection string, d Document)
error` (replaces the same ID; normalizes the embedding to unit length; every
document of a collection has the same model and dimensions, else
`ErrModelMismatch`), `Search(collection, model string, query Embedding, k
int, minScore float64) ([]Hit, error)` (brute-force cosine, best first;
`ErrModelMismatch` when the collection's model differs), `Delete`,
`DeleteCollection`, `Collections() []CollectionInfo`, `Documents(collection,
offset, limit int) ([]Document, int)`. Limits: collection names as
connection names; ID 1–200 characters; text ≤ 32 KiB; ≤ 20 000 documents
per collection; ≤ 8 192 dimensions; metadata ≤ 8 KiB once encoded.
`DefaultKnowledge`, `WithKnowledge`, `KnowledgeFromContext`.
`Chunk(text string, size int) []string` splits on paragraph, then sentence,
then word boundaries into chunks of at most `size` runes with a 10 % overlap.

## 2. Storage

Migration 2 (`llm`, appended to `internal/storage/migrations.go`,
`SchemaVersion = 2`):

```sql
CREATE TABLE llm_connections (
	name TEXT PRIMARY KEY, provider TEXT NOT NULL, base_url TEXT NOT NULL DEFAULT '',
	api_key TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
	daily_token_limit INTEGER NOT NULL DEFAULT 0, updated_at DATETIME NOT NULL);
CREATE TABLE llm_usage (
	day TEXT NOT NULL, connection TEXT NOT NULL,
	input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
	requests INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (day, connection));
CREATE TABLE llm_conversations (
	key TEXT PRIMARY KEY, turns TEXT NOT NULL, updated_at DATETIME NOT NULL);
CREATE TABLE llm_documents (
	collection TEXT NOT NULL, id TEXT NOT NULL, text TEXT NOT NULL,
	metadata TEXT NOT NULL DEFAULT '{}', model TEXT NOT NULL, dimensions INTEGER NOT NULL,
	embedding BLOB NOT NULL, created_at DATETIME NOT NULL, PRIMARY KEY (collection, id));
```

Embeddings are little-endian float32. `*storage.SQLiteStore` implements the
three persisters (`llm_connections.go`, `llm_memory.go`, `llm_knowledge.go`).
The whole-state backups (ADR-0032) carry the tables as part of the database.
The stores export (ADR-0031) carries LLM connections like broker
connections: the API key only inside the encrypted content.

## 3. Nodes

Shared rules: every LLM node names a `connection` (string, required,
`AgentVisible`, `Destination`, `Options: "llm-connections"`) and implements
`node.LLMConnectionRequirer`; deploy refuses an undefined one
(`undefined_llm_connection`, an environment issue like
`undefined_connection`). `model` is a string (`AgentVisible`, `Options:
"llm-models"`); empty uses the connection's model (`ErrNoModel` when both
are empty). Connections, memory and knowledge are resolved through the
node's context at run time, never in the factory. Every LLM node is `Risk:
external` (it sends message data to the provider) except `action-llm-forget`
(`safe`). Templates are rendered with the nodes' `renderTemplate`. Errors
follow §1.2 (transient errors are retried only where the runtime's opt-in
retries apply). Each node records `meta.llm` (or `meta.knowledge`) with the
connection, the model that answered, the tokens and the stop reason.
`maxTokens` is an integer (`Unit: "tokens"`, 1–64 000). `timeout` is an
integer number of seconds (`Unit: "seconds"`, 1–600, default 120) bounding
the node's whole call.

### `process-llm` (LLM prompt; processing; icon `sparkles`)

Settings: `connection`, `model`, `system` (template), `prompt` (template,
required, default `{{payload}}`), `images` (a message path such as
`payload.image`: base64, data URL, http(s) URL or a list; empty = none;
`AgentVisible`), `output` (`text` | `json`, default `text`, `AgentVisible`),
`schema` (`json`: the JSON Schema of a `json` answer; optional),
`maxTokens` (default 1 024), `effort` (enum `""`, `low`, `medium`, `high`,
`xhigh`, `max`; `AgentVisible`), `memoryKey` (template; empty = no memory;
`AgentVisible`), `memoryTurns` (integer, `Unit: "messages"`, 2–200, default
20), `resultField` (`AgentVisible`; empty replaces the payload), `timeout`.

With a memory key, the stored turns precede the prompt and the prompt and
answer are appended after a successful call (`Cache` is set). Output: one
port `output` with the text, or the parsed JSON (`output: json`).

### `process-llm-classify` (LLM classify; processing; icon `tags`)

Settings: `connection`, `model`, `categories` (array of strings, required,
1–20 unique names of 1–64 characters, none `other` or `error`,
`AgentVisible`), `instructions` (template, optional), `input` (template,
default `{{payload}}`), `timeout`. `NodeTypeInfo.OutputsFrom = "categories"`
(§5): one output port per category plus the static `other`. The request
uses a schema `{category: enum(categories + other), confidence: number
0..1}` (both required) and `maxTokens` 256. The message leaves on the port
of the category with the payload unchanged; `meta.llm.category` and
`meta.llm.confidence` are set.

### `process-llm-agent` (AI agent; processing; icon `bot`)

Settings: `connection`, `model`, `system`, `prompt`, `tools` (array of tool
names, `AgentVisible`, `Destination`, `Options: "tools"`), `maxSteps`
(integer, `Unit: "steps"`, 1–32, default 8), `allowPrivileged` (boolean,
`RiskSwitch`: privileged tools may be called), `maxTokens` (default 4 096),
`effort`, `memoryKey`, `memoryTurns`, `resultField`, `timeout` (default
300).

Loop: the node lists its tools through `node.ToolCallerFromContext(ctx)`
(§4), sends the request with them (`Cache` set), runs every tool call the
answer asks for (in order) through the caller, sends the results back (a
refused or failed call is a tool result with `IsError`, so the model can
react) and repeats until an answer asks for no tool, or fails with "the
agent did not finish within N steps" after `maxSteps` requests. Tool names
are sanitized for the provider (`[A-Za-z0-9_-]`, ≤ 64 characters, unique)
and mapped back. Output: the final text; `meta.llm.steps` lists `{tool,
status, durationMs}` (never arguments or results).

### `action-llm-forget` (Forget conversation; output; icon `eraser`; `safe`)

Settings: `memoryKey` (template, required). Clears the conversation from
the memory of its context and passes the message on.

### `process-transcribe` (Transcribe audio; processing; icon `audio-lines`)

Settings: `connection` (an `openai` connection: Anthropic answers
`ErrUnsupported`), `model` (default: the connection's, then `whisper-1`),
`audio` (a message path, required, default `payload`: base64, data URL,
bytes, or an http(s) URL fetched with the nodes' guarded HTTP client, which
refuses private addresses), `language`, `resultField`, `timeout`. At most
25 MiB of audio. Output: the text.

### `action-knowledge-add` (Add to knowledge base; output; icon `book-plus`)

Settings: `connection`, `model` (an embedding model, required),
`collection` (required, `AgentVisible`, `Destination`, `Options:
"knowledge-collections"`), `text` (template, default `{{payload}}`),
`documentId` (template; empty = a new UUID), `metadata` (object of
templates), `chunkSize` (integer, `Unit: "characters"`, 0 = one document,
else 200–8 000), `timeout`. Embeds the text (or its chunks, IDs
`<id>#<n>`), stores the documents and passes the message on with
`meta.knowledge = {collection, ids, inputTokens}`.

### `process-knowledge-search` (Search knowledge base; processing; icon `book-open`)

Settings: `connection`, `model` (the collection's embedding model),
`collection`, `query` (template, default `{{payload}}`), `topK` (integer 1–50,
default 4), `minScore` (number 0–1, default 0), `resultField`, `timeout`.
Output: `{query, results: [{id, text, score, metadata}], context}` where
`context` joins the texts with `\n\n---\n\n`, ready for a prompt
(`{{payload.context}}`).

## 4. Agent nodes call flow tools through the service (ADR-0039)

`internal/node` gains the contract (builtin nodes never import `flow` or
`service`):

```go
type AgentTool struct {
	Name, Description string
	InputSchema       json.RawMessage
	Risk              Risk
	Confirm           string
}
type AgentCaller struct {
	FlowID, NodeID  string
	Tools           []string // the names the node lists
	AllowPrivileged bool
}
type ToolCallOutcome struct {
	Status string          // "ok", "error", "running"
	Result json.RawMessage // redacted and capped, as agents see results
	Error  string
}
type ToolCaller interface {
	AgentTools(caller AgentCaller) []AgentTool
	CallAgentTool(ctx context.Context, caller AgentCaller, name string, args map[string]any) (ToolCallOutcome, error)
}
```

`node.WithToolCaller(ctx, tc)` / `node.ToolCallerFromContext(ctx)` (never
nil: outside a runtime that has one, it lists nothing and refuses every
call). The flow runtime puts the caller it was configured with
(`flow.WithToolCaller`, a `RuntimeManager` option set by the composition
root) on every node's context, with the run's flow and node IDs
(`node.WithNodeInfo` / `node.NodeInfoFromContext`). Dry-runs and node tests
get none.

`service.Tools` implements it with a new actor kind `ActorAgentNode` (no
token):

- only tools the node lists, by name; any other is `not_found`;
- tools published by the agent's own flow are refused (a call would wait
  for the node that is waiting for it);
- privileged tools only with `allowPrivileged`;
- only tools whose `confirm` is `none`: an agent node has no human to
  confirm a call (`client`) and cannot wait for the admin's approval
  (`admin`); both are refused with a message saying so;
- arguments are validated against the tool's schema; the tool's own
  `maxConcurrent` and `cooldown` apply, and each agent node is one slot key
  limited like a token (`DefaultPerTokenInflight`);
- results and errors are redacted as an MCP agent's (`resultView{agent:
  true}`): secrets removed, errors of nodes holding hidden settings
  withheld;
- the run's actor is `agent-node:<flowID>/<nodeID>`; there is no MCP audit
  row (no token), the run is in the flow's activity.

Risk: `process-llm-agent` is `external`; `allowPrivileged` is a
`RiskSwitch`, so turning it on makes the instance `privileged`
(`instanceRisk`), the drafts' approval page flags it and agent drafts may
not turn it on. `tools` is `Destination`: changing what an agent node may
call is flagged like a changed destination.

## 5. Outputs from a setting (ADR-0040)

`node.NodeTypeInfo` gains `OutputsFrom string` (`json:"outputsFrom,omitempty"`):
the name of an array-of-strings setting whose items are extra output ports
(ID and name = the item) after the static `Outputs`. The node's `Ports()`
returns them; deploy, Lint and the router already use the instance's ports.
`GET /api/node-types` sends `outputsFrom`; the editor renders the ports from
the node's config, and removing an item removes the edges leaving its port
in the same undo step. The implicit `error` port (ADR-0036) still comes
last; an item may not be named `error`.

`node.ConfigProperty` of `Type: "array"` is a list of strings in the editor
(a list input), checked by `node.CheckConfig` as before.

New suggestion sources (`ConfigProperty.Options`): `llm-connections`,
`llm-models` (the models of the node's `connection`), `tools` (published
tool names), `knowledge-collections`.

## 6. REST API (admin session, under `/api`; none of it on MCP)

| Route | Answer |
|---|---|
| `GET /api/llm/connections` | `{connections: [{name, provider, baseUrl, hasApiKey, model, dailyTokenLimit, usageToday: {inputTokens, outputTokens, requests}}]}` |
| `PUT /api/llm/connections/{name}` | body `{provider, baseUrl, apiKey?, model, dailyTokenLimit}` (no `apiKey`: keep the stored one; `""`: clear) → the stored item |
| `DELETE /api/llm/connections/{name}` | 409 while a running flow uses it (`data.flows`), under the lifecycle lock |
| `POST /api/llm/connections/{name}/test` | body as PUT; lists the models (spends no tokens) → `{ok, error?, models?: [{id, name}]}` |
| `GET /api/llm/connections/{name}/models` | `{models: [{id, name}]}` with the stored settings |
| `GET /api/llm/conversations` | `{conversations: [{key, turns, updatedAt}]}` |
| `GET /api/llm/conversations/{key}` | `{key, turns: [{role, text, at}]}` (key path-escaped) |
| `DELETE /api/llm/conversations/{key}` | clears it |
| `GET /api/llm/knowledge` | `{collections: [{name, documents, model, dimensions, updatedAt}]}` |
| `GET /api/llm/knowledge/{collection}?offset=&limit=` | `{documents: [{id, text, metadata, model, createdAt}], total}` (limit ≤ 100) |
| `DELETE /api/llm/knowledge/{collection}` | deletes the collection |
| `DELETE /api/llm/knowledge/{collection}/documents/{id}` | deletes one document |
| `POST /api/llm/assistant/flow` | §7 |

Changing a connection restarts nothing (the nodes resolve it per call).
`GET /api/secrets` usage lists LLM connections whose key references a
secret. The response types are added to the generated wire types
(`TestWireTypesAreCurrent`) and `web/src/types/wire.check.ts`.

## 7. The flow assistant (ADR-0041)

`POST /api/llm/assistant/flow` body `{connection, model?, description,
flow?}` (`flow`: the editor's current, unsaved definition, for "change this
flow"). `service.Assistant`:

1. builds a catalog from the registry (type, name, description, category,
   risk, ports, `outputsFrom`, settings with type, enum, default, required
   and description; documentation is left out) and a system prompt with the
   flow model (nodes with `id`, `type`, `position`, `config`; connections
   with `sourceNode`, `sourcePort`, `targetNode`, `targetPort`), the rules
   (templates, secrets as `{{secret.NAME}}` references never values, the
   admin's connections by name: the names of the LLM and broker
   connections are listed, never their settings) and the current flow;
2. asks for one JSON object `{name, description, notes, nodes,
   connections}` (`JSON` mode; `maxTokens` 16 000);
3. lays it out when positions are missing (`flow.Layout` if present, else a
   simple left-to-right layering), runs `flow.Lint`, and when it finds
   blocking issues sends them back once or twice (at most 2 repair rounds);
4. answers `{flow: {name, description, nodes, connections}, notes, issues,
   usage}`.

It stores, deploys and runs nothing: the editor shows the proposal (nodes,
connections, notes, remaining issues) and the admin applies it to the canvas
(replacing an empty canvas or added beside the current nodes with new IDs)
as one undo step, then saves and deploys as usual. One assistant request
runs at a time (409 while busy); the call is bounded to 3 minutes.

## 8. Editor

- **LLM connections dialog** (navigation shell, like Broker connections):
  list with provider, endpoint, model, usage today / limit; form with
  provider presets (Anthropic, OpenAI, Ollama `http://localhost:11434/v1`,
  LM Studio `http://localhost:1234/v1`, OpenRouter
  `https://openrouter.ai/api/v1`), write-only API key with secret reference
  suggestions, default model with suggestions from the connection's models,
  daily token limit; test (shows the models found); delete (shows the
  server's refusal and the flows using it).
- **AI memory dialog**: conversations (key, turns, updated; view as plain
  text through `AgentText`-like escaping; clear) and knowledge collections
  (documents, model, dimensions; browse documents; delete a document or a
  collection).
- **Node settings**: list input for `array` settings; suggestions for the
  new sources; the classifier's ports follow its categories.
- **Flow assistant**: "Create with AI" in the editor toolbar (and on an
  empty canvas): connection and model, description; the proposal's nodes,
  connections, notes and issues; "Apply" (empty canvas) or "Add to canvas".
- All text in English and Spanish (`web/src/i18n/{en,es}/llm.ts`,
  `assistant.ts`).

## 9. Security notes

- Text that reaches a node (webhooks, Telegram, RSS, e-mail) is untrusted:
  a prompt can be injected. `process-llm` and the classifier only produce
  data; the agent node is the one that acts, and what it may do is bounded
  by the tools the admin listed, their `confirm` (only `none`), their risk
  (`allowPrivileged`), `maxSteps` and the tools' own limits.
- API keys stay in the connection store (secrets by reference), are never
  sent to the browser, to agents or to the model, and are redacted from
  errors.
- Message data sent to a cloud provider leaves the device; a local model
  (Ollama, LM Studio) keeps it on the LAN. The user guide says so.
- The daily token limit bounds cost per connection; usage is shown in the
  connections dialog.
