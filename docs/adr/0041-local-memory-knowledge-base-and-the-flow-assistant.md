# ADR-0041: Local conversation memory, knowledge base and the flow assistant

**Status:** Accepted
**Date:** 2026-11-15
**Deciders:** maintainer
**Design:** [docs/design/llm.md](../design/llm.md) §1.6, §1.7, §7, §8

## Context

Three uses of the LLM connections (ADR-0038) need state or a new editor
path:

- a chat bot must remember what was said in each conversation;
- answering from the admin's own documents needs embeddings and a
  similarity search (retrieval-augmented generation);
- admins want to describe a flow in words and get a first version of it.

PiWorker is one binary on a Pi with one SQLite database. Dry-runs and node
tests run nodes against empty, private stores (ADR-0020). Flows only change
through a save the admin makes or a draft the admin approves (ADR-0018).

## Options considered

### Memory and knowledge base

- **A: flow variables.** History kept with Set/Get Variable. **Pros:** no
  new store. **Cons:** turns, trimming and size bounds in templates; no view
  of the conversations; concurrent messages race on one value.
- **B: an external vector database or the provider's own memory.**
  **Pros:** scale. **Cons:** another service to run on a Pi, or the data on
  the provider's side and tied to one provider.
- **C: local stores in PiWorker's database.** In memory with write-through
  to SQLite, resolved through the node's context, with brute-force cosine
  search. **Pros:** no new process, works offline with a local embedding
  model, isolates like the variable stores. **Cons:** search is linear in
  the documents of a collection (bounded at 20 000).

### Flow assistant

- **D: no assistant.** Agents can already draft flows over MCP
  (`pw_save_draft`). **Cons:** needs an MCP client set up; nothing for an
  admin who only has the editor.
- **E: the assistant saves or deploys.** **Cons:** skips the review every
  other path to a running flow has.
- **F: the assistant proposes; the admin places it on the canvas.**
  **Pros:** the proposal is checked with the same Lint as a deploy and
  reviewed before anything is saved; one undo step takes it back.
  **Cons:** a model request of up to a few minutes, paid from the chosen
  connection's budget.

| Dimension | C + F |
|---|---|
| Complexity | Medium: two stores, two REST groups, one service, two dialogs |
| Security | Data stays in the database the admin controls; the assistant never sees credentials and can change nothing by itself |
| Pi resources | Embeddings in RAM (8 192 dimensions at most, float32); one assistant request at a time |
| Maintenance | Stores follow the existing persister pattern |

## Decision

C and F.

- **Memory** (`llm.Memory`, table `llm_conversations`): a conversation is
  a key the node renders from a template (1–200 characters, one line, e.g.
  `telegram:{{payload.body.message.chat.id}}`) and its text turns (no images,
  no tool calls). LLM Prompt and AI Agent send the last `memoryTurns`
  turns before the prompt and append the prompt and the answer after a
  successful call (a failed save is logged; the answer is still passed
  on). Bounds: 32 KiB per turn (cut), 200 turns per conversation, 5 000
  conversations (adding one past it evicts the least recently updated).
  Forget Conversation clears one; an exchange in flight for a key cleared
  during it is not saved, so a forgotten conversation never comes back.
- **Knowledge base** (`llm.Knowledge`, table `llm_documents`): named
  collections of documents with a unit-length float32 embedding, text and
  metadata; one embedding model and dimension per collection
  (`ErrModelMismatch` otherwise); brute-force cosine search. Bounds: 32 KiB
  of text and 8 KiB of metadata per document, 20 000 documents per
  collection, 8 192 dimensions. Embeddings come from an OpenAI-compatible
  connection (OpenAI, Ollama); only the text to embed and the query leave
  the device, never the stored documents.
- **Isolation.** Nodes resolve both stores, and the connections, through
  their context (`MemoryFromContext`, `KnowledgeFromContext`,
  `llm.FromContext`; a nil store given to `WithMemory`, `WithKnowledge` or
  `WithStore` is an empty one). The dry-run's sandbox gives its nodes empty
  ones, as it gives them an empty secret store; nodes that call a model
  refuse a sandbox anyway (`ErrSandboxed`), and Forget Conversation, which
  is `safe` and so runs there, clears the sandbox's own memory. The
  editor's payload playground runs LLM nodes for real but with an empty
  memory, so a test never writes a real conversation. Knowledge
  collections and memory keys are `Destination` settings: changing which
  collection a flow writes or searches, or which conversation it reads,
  writes or forgets (conversations are shared by the whole process), is
  flagged on drafts.
- **Editor.** The AI memory dialog lists, shows (as plain text with hidden
  characters escaped) and clears conversations, and browses and deletes
  documents and collections, through `/api/llm/conversations` and
  `/api/llm/knowledge` (admin session only, never MCP).
- **Flow assistant** (`service.Assistant`, `POST
  /api/llm/assistant/flow`): the prompt holds the flow model, the rules
  (credentials only as `{{secret.NAME}}` references), a catalog of the
  registered node types (settings, enums, bounds, ports; no documentation),
  the **names** of the LLM and broker connections (never their settings;
  no secret, tool or collection names) and, for "change this flow", the
  editor's current flow with every credential replaced by `<redacted>`
  (Sensitive values that are not only secret references, and every value
  whose sensitivity is unknown); the withheld values come back on the
  nodes the model kept (same ID and type). The answer is one JSON object;
  missing positions are laid out; `flow.LintContext` checks it against the
  assistant's stores and its blocking issues go back to the model for at
  most 2 repair rounds. The answer is the proposal, its notes, the
  remaining issues and the tokens spent. It stores, deploys and runs
  nothing; one request runs at a time (409), bounded to 3 minutes (504).
  The editor shows the proposal and places it as one undo step: on an
  empty canvas, replacing the canvas for a changed flow, or beside the
  current nodes with new IDs. Closing the dialog cancels the request.

## Consequences

- Easier: chat bots, routing and answers from documents with no extra
  service; a first draft of a flow from a sentence, reviewed like any edit.
- Harder: conversations and documents are personal data in PiWorker's
  database and its backups (ADR-0032); the admin clears them in the AI
  memory dialog. The assistant's prompt is long (about 11 000 tokens with
  the node types of this release), so a local model needs a large context
  window, and each repair round sends it again (Anthropic caches it).
- Revisit when: collections outgrow a linear search on a Pi, or the
  assistant should draft through the same path as agents (a draft the
  admin approves).

## Action items

1. [x] Stores, migration 2 tables, REST, AI memory dialog.
2. [x] Assistant service, route and editor dialog.
3. [x] Dry-runs and node tests get empty LLM stores
   (`flow.sandboxContext`); the payload playground an empty memory.
