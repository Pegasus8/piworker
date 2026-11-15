# ADR-0038: LLM connections, providers and the official Anthropic Go SDK

**Status:** Accepted
**Date:** 2026-11-15
**Deciders:** maintainer
**Design:** [docs/design/llm.md](../design/llm.md) §1, §2, §6

## Context

Flows that call a large language model (summarize an e-mail, classify a
message, answer a chat, extract JSON from text, transcribe a voice note,
answer from the admin's own documents) need a model provider, a credential
and a way to bound what they cost. The admin may want a cloud model (Claude,
OpenAI, OpenRouter) or a local one (Ollama or LM Studio on the Pi or the
LAN), so data does not leave the network. Until now the only way was an
HTTP Request node with the API key in a header and the provider's JSON
written by hand: no structured output, no tool loop, no cost bound, and
every provider's quirks in the admin's flows. The nodes must keep working
on a Pi, resolve credentials per call like the other nodes (ADR-0020) and
never reach a provider from a dry-run or a node test.

## Options considered

### Option A: keep the HTTP Request node

**Pros:** no new component. **Cons:** keys in flows, no budget, no
structured output or tool loop, a prompt template per provider.

### Option B: one OpenAI-compatible client for every provider

Anthropic offers an OpenAI-compatible endpoint, so one HTTP client could
serve all providers.

**Pros:** one wire format, no SDK. **Cons:** the compatibility endpoint is a
translation layer, not the Messages API: the features the nodes rely on
(structured outputs, effort, automatic prompt caching, refusal details,
replaying thinking blocks in a tool loop) are the Messages API's own.

### Option C: a multi-provider framework library

**Pros:** many providers at once. **Cons:** a large dependency tree, an
abstraction that lags the providers, and the same narrowing to what the
nodes need would still be ours to write.

### Option D: named connections; Anthropic through its official Go SDK, every other provider through one OpenAI-compatible client

| Dimension | Assessment |
|---|---|
| Complexity | Medium: one package (`internal/llm`), two clients behind one interface, a store like the broker connections' |
| Security | Keys write-only, by `{{secret.NAME}}` reference, resolved per call and redacted from errors; agents only pick a connection by name |
| Pi resources | One shared HTTP transport; brute-force search over at most 20 000 documents per collection; the SDK and its dependencies add about 55 pure-Go packages to the binary |
| Maintenance | The SDK tracks the Claude API; the OpenAI-compatible client is plain JSON over `net/http` that every local server understands |

## Decision

Option D.

- **Connections.** `internal/llm` holds named LLM connections (`Store`,
  like `brokers.Store`): `name` (1–64 `[A-Za-z0-9_-]`), `provider`
  (`anthropic` or `openai`), optional `baseUrl` (http or https with a host;
  no credentials, query or fragment; private and loopback addresses
  allowed, since the endpoint is the admin's), `apiKey` (required for
  `anthropic`; may hold `{{secret.NAME}}` references), a default `model`
  and `dailyTokenLimit`. Nodes name a connection (`connection`,
  `AgentVisible`, `Destination`) and resolve it through their context
  (`llm.FromContext`) on every call, so a change applies to the next call
  and restarts nothing. Nodes implement `node.LLMConnectionRequirer`: deploy
  refuses an undefined connection and Lint reports it as
  `undefined_llm_connection`, an environment issue like
  `undefined_connection` (it blocks a deploy, not a draft or a dry-run).
  Deleting a connection a running flow uses is refused (409, `data.flows`)
  under the lifecycle lock.
- **Clients.** `llm.NewClient(ctx, c)` resolves the key strictly (an
  undefined secret is an error naming it) and returns a `Client` (`Chat`,
  `Models`, `Embed`, `Transcribe`); a sandboxed context gets
  `ErrSandboxed`. `anthropic` uses `github.com/anthropics/anthropic-sdk-go/v2`
  with `WithoutEnvironmentDefaults` (the connection is the only source of
  key and endpoint), no SDK retries (the runtime's retries stay opt-in,
  `node.Transient`) and streaming; `openai` is JSON over `net/http`
  (chat completions, models, embeddings, audio transcriptions) for OpenAI,
  Ollama, LM Studio, OpenRouter, LocalAI, vLLM and the like. Anthropic has
  no embeddings or transcription API: those calls fail with
  `ErrUnsupported`. Rate limits, overload, 5xx and network errors are
  transient; everything else is permanent.
- **Daily budget.** Every call that spends tokens (`Chat`, `Embed`,
  `Transcribe`) first checks the stored connection's `dailyTokenLimit`
  against today's input plus output tokens (local calendar day) and fails
  with `ErrBudgetExhausted` once it is reached; when today's usage cannot be
  read, a limited connection is refused (fail closed). The check precedes
  the call, so the call that crosses the limit completes. Usage is recorded
  after every answer, also when the answer then fails validation; Anthropic
  input counts cache writes and reads. Listing models spends nothing and is
  neither checked nor counted. Transcription counts requests only.
- **Storage.** Migration 2 (`llm`, `SchemaVersion = 2`, ADR-0035) adds
  `llm_connections`, `llm_usage`, `llm_conversations` and `llm_documents`
  (the last two: ADR-0041). Whole-state backups carry them (ADR-0032). The
  stores export (ADR-0031) carries LLM connections in format version 1 as
  new members: the key only inside the sealed part; a restore keeps a local
  key only while the file leaves the provider and endpoint unchanged, else
  restores the connection without a key (an Anthropic one, which cannot
  work without one, is left as it is) and lists it in
  `apiKeysNotRestored`. The Secrets dialog lists LLM connections whose key
  references a secret.
- **REST** (admin session, `/api/llm/…`, nothing on MCP): list (with
  `hasApiKey` and today's usage, never the key), put (no `apiKey` keeps the
  stored one, `""` clears it), delete, test (lists the models with the
  settings sent; spends no tokens) and the models of a stored connection;
  §6 of the design has the shapes and statuses.

## Consequences

- Easier: one place for a provider, its key and its cost bound; local
  models are a connection with a base URL; flows stay portable (they name
  the connection, not its key).
- Harder: a new significant dependency (the SDK and its JSON, OpenTelemetry
  API and schema packages; the Bedrock and Vertex packages are not
  imported, so their modules are not built in). The budget is a soft daily
  bound, not a price: one large call can pass it.
- Revisit when: a provider PiWorker needs is neither Anthropic nor
  OpenAI-compatible, or the admin needs budgets per flow instead of per
  connection.

## Action items

1. [x] `internal/llm` connections, clients, budget; migration 2.
2. [x] REST handlers, Secrets usage, stores export; editor dialog.
