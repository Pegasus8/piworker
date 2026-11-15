# AI in flows

Flows can ask a language model: summarize a text, sort messages into
categories, extract data as JSON, answer a chat, transcribe a voice note,
answer from your own documents, or act through tools you publish. The model
can be a cloud service (Claude, OpenAI, OpenRouter) or a model running on
your own network (Ollama, LM Studio).

This page is about flows that **use** a model. For the opposite direction,
an AI assistant such as Claude using your flows, see [AI agents](agents.md).

- [What you can build](#what-you-can-build)
- [Step 1: add an LLM connection](#step-1-add-an-llm-connection)
- [What leaves your device](#what-leaves-your-device)
- [Daily limits and usage](#daily-limits-and-usage)
- [The nodes, with examples](#the-nodes-with-examples)
- [Conversation memory](#conversation-memory)
- [Knowledge base: answers from your documents](#knowledge-base-answers-from-your-documents)
- [The AI Agent and its limits](#the-ai-agent-and-its-limits)
- [Create a flow with AI](#create-a-flow-with-ai)
- [Troubleshooting](#troubleshooting)

Ready-made flows are in Recipes: [a Telegram chat bot with
memory](recipes.md#a-telegram-chat-bot-with-memory), [routing support
e-mails](recipes.md#route-support-e-mails-by-topic), [an agent that switches
the lights](recipes.md#an-agent-that-switches-the-lights) and [answers from
your documents](recipes.md#answer-questions-from-your-documents).

## What you can build

| Node | What it does |
|------|--------------|
| **LLM Prompt** | Sends a prompt built from the message (and images, if you want) and passes on the answer as text or JSON. |
| **LLM Classify** | Picks one of your categories for the message and sends it out of that category's port. |
| **AI Agent** | Lets the model call tools you publish as flows, until it can answer. |
| **Forget Conversation** | Clears a conversation the LLM nodes remember. |
| **Transcribe Audio** | Turns speech in an audio message into text. |
| **Add to Knowledge Base** | Stores text so it can be found by meaning later. |
| **Search Knowledge Base** | Finds the stored texts closest to a question, ready for a prompt. |

Every node and setting is in the [node reference](nodes.md); each node's
**?** button in the editor shows the same documentation with examples.

## Step 1: add an LLM connection

The nodes do not hold API keys or addresses. You define a **connection**
once and the nodes name it. In the navigation, under Settings, open **LLM
connections** and add one:

| Setting | What to enter |
|---------|---------------|
| Name | A short name the nodes use, for example `claude` or `ollama` (letters, digits, `_`, `-`). It cannot change later. |
| Provider | A preset (below) or **Other server**. |
| Base URL | Filled in by the preset; change it when the server runs on another computer. Empty means the provider's own address. |
| API key | Write-only: never shown again. Better: store the key under **Secrets** and choose it with **Use a secret**, which writes `{{secret.NAME}}`. |
| Default model | The model nodes use when they name none. **Test connection** lists the models the connection offers. |
| Daily token limit | Optional. See [Daily limits and usage](#daily-limits-and-usage). |

**Test connection** lists the models with what you typed, without saving
it, and spends no tokens.

### Presets

| Preset | Base URL | API key | Notes |
|--------|----------|---------|-------|
| **Anthropic** (Claude) | `https://api.anthropic.com` (default) | Required | Chat, images, JSON and agents. No embeddings or transcription: add an OpenAI or Ollama connection for those. |
| **OpenAI** | `https://api.openai.com/v1` (default) | Required | Chat, images, JSON, agents, embeddings (`text-embedding-3-small`) and transcription (`whisper-1`). |
| **Ollama** | `http://localhost:11434/v1` | None | Runs models on your own hardware. Install a model first (`ollama pull <model>`); an embedding model such as `nomic-embed-text` for the knowledge base. |
| **LM Studio** | `http://localhost:1234/v1` | None | Start LM Studio's local server and load a model. |
| **OpenRouter** | `https://openrouter.ai/api/v1` | Required | Many providers' models through one key; model names look like `vendor/model`. |
| **Other server** | The server's API address, for example `http://192.168.1.20:8000/v1` | If it asks for one | Any server that speaks the OpenAI API: vLLM, LocalAI, llama.cpp and others. |

`localhost` is the device PiWorker runs on. For a server on another
computer, use that computer's address (`http://192.168.1.20:11434/v1`) and
make the server listen on the network: Ollama only listens on its own
computer unless it is started with `OLLAMA_HOST=0.0.0.0`; LM Studio has an
option to serve on the local network. A Raspberry Pi can run small models
itself, slowly; a PC with a graphics card on the same network answers much
faster.

Changing a connection takes effect at the next call; nothing restarts. A
connection a running flow uses cannot be deleted: stop those flows first. A
stopped flow whose nodes name a connection that does not exist is refused
when you deploy it (`undefined_llm_connection`).

### Choosing a model

- Leave **Model** empty in the nodes to use the connection's default. Set it
  on a node when that node needs another model (a cheaper one for
  classifying, an embedding model for the knowledge base).
- The **AI Agent** needs a model that supports tool use (function calling).
  Current Claude and OpenAI models do; with local models check the model's
  description.
- **Effort** (LLM Prompt, AI Agent) asks a model that can think before
  answering to think less or more. Models without the setting ignore it.
  Thinking counts towards **Max tokens**.

## What leaves your device

With a **cloud provider**, what a node sends goes to that provider's
servers:

| Node | Sent to the provider |
|------|----------------------|
| LLM Prompt | The system prompt, the prompt, the images and the remembered conversation |
| LLM Classify | The text to classify, your categories and instructions |
| AI Agent | The system prompt, the prompt, the remembered conversation, the names, descriptions and input schemas of its tools, and the results of the tools it calls |
| Transcribe Audio | The audio |
| Add to Knowledge Base | The text being added (to compute its embedding) |
| Search Knowledge Base | The query only; the stored documents are not sent |
| Create with AI | Your description, the list of node types, the **names** of your LLM and broker connections and, when you change a flow, that flow without its credentials |

With a **model on your network** (Ollama, LM Studio, another server), all of
this stays on your network.

- API keys never reach the browser, the model or AI agents, and they are
  removed from error messages.
- Conversations, the knowledge base and the daily usage are stored in
  PiWorker's database, and so are in its [backups](concepts.md#backups).
  **AI memory** (in the navigation, under Settings) shows and clears the
  conversations and the knowledge base.
- Text from outside (a chat message, an e-mail, a web page) can contain
  instructions aimed at the model. Treat what the model answers as data;
  see [The AI Agent and its limits](#the-ai-agent-and-its-limits) for the
  node that acts.

## Daily limits and usage

Each connection counts its tokens per day (input plus output, local time).
The **LLM connections** dialog shows today's tokens and requests, warns from
80 % of the limit and says when it is reached.

- With a **daily token limit**, once today's tokens reach it, every node
  using the connection fails with *the connection's daily token limit is
  spent* until midnight. The check happens before each call, so the call
  that crosses the limit still finishes: the limit is a daily bound, not an
  exact cap. Empty or `0` is no limit.
- Every input token the model read counts, including text the provider read
  from its cache (cheaper at Anthropic, but counted in full here). Tokens of
  an answer that then fails (a refusal, an answer that is not the JSON asked
  for) count too: they were spent.
- Embeddings count their input tokens. Transcription counts requests only.
  Testing a connection and listing its models count nothing.
- The limit counts tokens, not money. Your provider's own spending limits
  are the place for a budget in currency.

## The nodes, with examples

Each LLM node records what it did in **meta.llm** (or **meta.knowledge**):
the connection, the model that answered, the input and output tokens, the
requests, and why the model stopped (`end`, or `max_tokens` when the answer
was cut). Read it in a **Debug** node.

All of them have **Timeout (s)** (default 120 seconds; 300 for the AI
Agent), which bounds the whole call. Network errors, rate limits and an
overloaded provider are temporary: the flow's retries apply to them.
Everything else (a refusal, a spent limit, an invalid answer, the timeout)
fails the message, which then leaves through the node's **On error**
port if you connected it ([When a node fails](concepts.md#when-a-node-fails)).

**Result field** decides where the result goes: empty replaces the payload;
`answer` or `ai.summary` keeps the payload and adds the result there.

### LLM Prompt

Summarize every new RSS item in one sentence and keep the item:

| Setting | Value |
|---------|-------|
| Connection | `claude` |
| System prompt | `You write one-sentence summaries in plain English.` |
| Prompt | `Summarize: {{payload.title}}. {{payload.description}}` |
| Result field | `summary` |

The next node reads `{{payload.summary}}`.

Extract data as JSON with **Output** `json` and an optional **JSON
schema**:

```
Prompt: Extract the amount and due date from: {{payload.body.text}}
Output: json
JSON schema: {"type": "object", "properties": {"amount": {"type": "number"}, "due": {"type": "string"}}, "required": ["amount", "due"]}
```

An answer that does not match the schema fails the message instead of
passing on wrong data. **Images** takes a message path (`payload.image`)
holding a base64 image, a data URL, a public image URL or a list of them
(PNG, JPEG, GIF or WebP, at most 20 MiB together); the model must support
images.

### LLM Classify

Add your **categories** as items of the list; each becomes an output
port, plus **other** for messages that fit none (at most 20, up to 64
characters each). The message leaves
unchanged through the chosen port, with `meta.llm.category` and
`meta.llm.confidence` (0 to 1). **Instructions** say what each category
means:

```
Categories: billing, technical, sales
Instructions: billing covers invoices, payments and refunds; technical covers errors and how-to questions.
Input: {{payload.subject}} {{payload.text}}
```

Whatever the text says, the model can only choose one of these ports.
Removing a category removes the connections of its port; renaming it in
place keeps them, even when you clear its text or pass through another
category's name while you type (the connection comes back once the row has
a name of its own). `other` and `error` are reserved names. To send uncertain
messages to a person, follow a port with a **Switch** on
`meta.llm.confidence > 0.7`.

### Transcribe Audio

Needs an OpenAI-compatible connection with a speech-to-text model: OpenAI
(`whisper-1`) or a local server with a Whisper model. **Audio** is the
message path of the audio (base64, a data URL, bytes or a public URL the
node downloads; at most 25 MiB). Set **Language** (`en`, `es`) when you know
it: it improves accuracy. Put the text in a **Result field** and continue
with an LLM Prompt to answer it.

### AI Agent

Answer questions about the house with tools you published (`lights_set`,
`thermostat_get`):

| Setting | Value |
|---------|-------|
| System prompt | `You control the house with your tools. Answer in one short sentence.` |
| Prompt | `{{payload.body.message.text}}` |
| Tools | `lights_set`, `thermostat_get` |
| Max steps | `4` |
| Result field | `answer` |

What it may call, and how to keep it safe, is in
[The AI Agent and its limits](#the-ai-agent-and-its-limits).

### Testing in the editor

**Test node** in the **Payload playground** of a node's settings runs an
LLM node for real: it calls the provider and spends tokens from the
connection's daily limit. A test stops after 2 minutes (or the node's own
**Timeout**, if shorter). It starts with an empty memory: a node with a
**Memory key** answers as in a new conversation and the test is not
remembered. The AI Agent calls no tools there. The dry-runs that AI agents
use to check a flow never call a model.

## Conversation memory

LLM Prompt and AI Agent remember a conversation when **Memory key** is set.
The key is a template, so each chat gets its own history:
`telegram:{{payload.body.message.chat.id}}`.

- Before the prompt, the node sends the last **Memory length** messages of
  that conversation (default 20; a question and its answer are two). After
  a successful answer it adds the prompt and the answer. A failed call adds
  nothing.
- Only text is remembered: no images, no tool calls.
- Limits: 200 messages per conversation (each cut at 32 KiB), 5 000
  conversations; when a new one would pass that, the one unused for longest
  is dropped.
- **Forget Conversation** with the same key template clears one, for
  example on a `/reset` message. **AI memory → Conversations** lists them,
  shows them as plain text and clears them.
- A longer memory sends more tokens on every message. With Anthropic the
  repeated part is cached by the provider, but it still counts towards the
  daily limit.
- A key with an empty or `..` part (`a//b`, `a/../b`) or starting with `/`
  works in flows but cannot be opened or cleared in AI memory; avoid such
  keys.

## Knowledge base: answers from your documents

The knowledge base lets a model answer from your own texts (manuals, notes,
procedures) instead of from what it learned. It works in two steps:

1. **Add to Knowledge Base** turns each text into an *embedding* (a list of
   numbers that captures its meaning) with an embedding model and stores
   both in a **collection** in PiWorker's database.
2. **Search Knowledge Base** embeds a question the same way and returns the
   stored texts closest in meaning, with a `context` text to put in a
   prompt.

Embeddings need an OpenAI-compatible connection: OpenAI
(`text-embedding-3-small`) or Ollama (`nomic-embed-text`). The answer can
come from any connection, Claude included.

- Every document of a collection must use the same embedding model; the
  search node uses the collection's model when you leave **Model** empty.
  To change models, delete the collection under **AI memory** and add the
  documents again.
- A document holds at most 32 KiB of text. Set **Chunk size** (for example
  `1000`) to split long texts into overlapping pieces; each piece is a
  document `<id>#<n>`.
- The same **Document ID** again replaces the document and all its pieces,
  so re-adding an updated manual does not leave old passages behind. Empty
  gives each message a new document.
- A collection holds at most 20 000 documents. Searching it is done on the
  device and sends nothing but the query to the provider.
- **AI memory → Knowledge base** lists the collections, shows their
  documents a page at a time and deletes documents or whole collections.

The passages found are sent to the answering model as part of the prompt:
with a cloud model they leave the device at that point. See the recipe
[Answer questions from your documents](recipes.md#answer-questions-from-your-documents).

## The AI Agent and its limits

An **AI Agent** gives the model **tools**: flows you published with an
**Agent Tool** trigger and an **Agent Reply** (built as in
[AI agents, step 2](agents.md#step-2-publish-a-flow-as-a-tool)). The node
sends the prompt and the list of tools; when the model asks for a tool, the
node runs that flow with the model's arguments, sends the result back, and
repeats until the model answers without a tool call. The answer is the
result. MCP does not need to be on.

**Tools** lists the tool names the agent may use; the editor suggests the
published ones and warns about any it cannot call. Of those it may only
call tools that:

- are published by a **running** flow other than the agent's own;
- need no confirmation (**Confirmation** `none`): the agent has nobody to
  ask and cannot wait for your approval;
- are not privileged (commands, files, databases, hardware pins), unless
  you turn on **Allow privileged tools**.

On every call, as for Claude over MCP: the arguments must match the tool's
input schema (a mismatch goes back to the model, which can fix its call);
the tool's **Concurrent calls** and **Cooldown** apply; one agent node runs
at most 2 tool calls at once; and the result is cleaned like an agent's:
secrets are removed, and errors of nodes with settings agents may not read
are withheld. A refused or failed call is reported to the model as an
error, so it can try something else.

**Max steps** (default 8) bounds the requests to the model; if the model
still asks for a tool after that many, the message fails with *the agent did
not finish within N steps*. `meta.llm.steps` lists each tool call with its
status (`ok`, `error`, `running` when the tool's own timeout passed, or
`refused`) and duration, never the arguments or results. Each call is a run
of the tool's flow, in that flow's **Activity**; it is not in the
**Agents → Audit** page, which is for agent tokens.

### Prompt injection

Text that reaches the agent from outside (a chat message, an e-mail, a web
page, even a tool's result) can contain instructions written to make the
model misuse its tools. No system prompt prevents this reliably. What the
agent can do is limited by what you allow, so:

- List only the tools the agent needs. Prefer tools that only read.
- Give tools strict input schemas: an `enum` of rooms instead of free text,
  `minimum` and `maximum` on numbers.
- Keep **Allow privileged tools** off. A flow that can run commands or
  write files should not be reachable from untrusted text.
- Keep tools that must never run by surprise (unlock a door, send money)
  out of the list. Giving such a tool **Confirmation** `admin` means no AI
  Agent node can call it, and an assistant over MCP waits for your
  approval.
- Use the tools' **Cooldown** and **Concurrent calls**, and a low **Max
  steps**.
- Let only trusted senders reach the agent, for example with a **Filter** on
  the chat ID before it.

When an AI assistant drafts flows for you, turning on **Allow privileged
tools** or changing an agent node's **Tools** is flagged on the approval
page, and the `low` [agent deploy](agents.md) level refuses both.

## Create a flow with AI

In the editor, **Create with AI** (in the toolbar, and on an empty canvas)
asks a model for a first version of a flow:

1. Choose an LLM connection and, optionally, a model.
2. Describe what the flow should do, up to 4 000 characters: what starts
   it, what it should do with the data, where the result goes. Name the
   devices, connections and secrets you already have.
3. Optionally tick **Change the current flow**: the proposal then replaces
   the nodes on the canvas (your unsaved changes included). Credentials in
   the flow are not sent; the proposal keeps them.
4. **Propose a flow**. It can take up to 3 minutes: PiWorker checks the
   proposal the way a deploy would and, when it finds problems, sends them
   back to the model to fix (twice at most).

The proposal lists its nodes (with their risk), connections, the model's
**notes** (secrets or connections to create, values to check), any problems
still found and the tokens spent. **Apply** (or **Apply changes**) places
it on the canvas, **Add to canvas** adds it beside your nodes. **Undo**
takes it back. If you edit the canvas after asking for a change of the
current flow, the proposal is out of date: the dialog offers **Propose
again** instead of applying it over your edits. Nothing is saved or deployed: review each node's settings,
create the secrets and connections the notes mention, then save and deploy
as usual.

- One proposal is made at a time. Closing the dialog cancels the request.
- The tokens come from the chosen connection's daily limit. The request is
  long (about 11 000 tokens for the list of node types) and is sent again
  for each fix; Anthropic caches the repeated part.
- A capable model gives much better flows. A local model needs a large
  context window (see [Troubleshooting](#troubleshooting)).

## Troubleshooting

| Symptom | What to do |
|---------|------------|
| **Test connection** fails with 401 or 403 | The API key is wrong or lacks access. With a `{{secret.NAME}}` key, check the secret's value. |
| **Test connection** cannot connect to `localhost:11434` or `:1234` | `localhost` is the PiWorker device. Use the address of the computer running Ollama or LM Studio, and make it listen on the network (`OLLAMA_HOST=0.0.0.0`). Keep `/v1` at the end of the base URL. |
| *secret "X" is not defined (referenced by the API key of LLM connection "Y")* | Create the secret under **Secrets** with exactly that name. |
| Deploy refused: *names LLM connection "X", which is not defined* (`undefined_llm_connection`) | Create the connection under **LLM connections** with that name, or choose another in the node. |
| *no model: set one on the node or on the connection* | Set **Model** on the node or a default model on the connection. |
| *the connection's daily token limit is spent* | Wait until midnight (local time), or raise the limit in **LLM connections**. The dialog shows today's usage. |
| *the model declined the request* | The provider refused this content. Rephrase the prompt, or connect the node's **On error** port. Retrying sends the same request. |
| *the answer was cut at maxTokens (N)* | Raise **Max tokens**, or lower **Effort**: thinking counts towards the limit. **LLM Classify** has a fixed limit of 256: if it fails this way, use a model that answers without long thinking. |
| *the model's answer is not valid* | The model did not answer the JSON asked for. Simplify the schema, describe the fields in the prompt, or use a more capable model. Some local servers do not support JSON schemas well; try **Output** `json` without a schema. |
| *not supported by this provider* | Anthropic has no embeddings or transcription. Use an OpenAI or Ollama connection in that node. |
| *collection "X" holds embeddings of "A", not "B"* | Set the node's **Model** to the collection's model, or delete the collection under **AI memory** and add the documents again with the new one. |
| A local model's answers ignore the instructions, or **Create with AI** fails or proposes nonsense with it | The prompt is probably longer than the model's context window and the server cut it without saying so. **Create with AI** needs at least about 16 000 to 32 000 tokens. For Ollama, start it with `OLLAMA_CONTEXT_LENGTH=32768` (or set `num_ctx` in a Modelfile); in LM Studio, raise the context length when loading the model. Long memories and knowledge-base context need room too. |
| *the LLM call did not finish within N seconds* | Raise the node's **Timeout (s)** (up to 600), or use a faster model. A test in the Payload playground stops after 2 minutes whatever the node's timeout. |
| *LLM calls do not run in a dry-run or a node test* | Expected: the checks AI agents run never call a model. Use **Test node** in the Payload playground, or **Run manually** on a deployed flow. |
| The agent never calls a tool | Check the hints under **Tools**: the tool's flow must be running, must not be this flow, must have **Confirmation** `none`, and must not be privileged unless allowed. The model must support tool use. |
| *the agent did not finish within N steps* | Raise **Max steps**, make the tools' descriptions clearer, or give the agent fewer tools. |
| *this agent node already runs 2 call(s), its limit* | Earlier tool calls of this agent are still running (they did not answer within the tool's **Timeout**). The model is told and can try later; give the tool a longer timeout or make its flow faster. |
| **AI memory** cannot open a conversation or delete a document | Its key or ID has an empty or `..` part (`a//b`, `a/../b`), or starts with `/`. Use keys like `telegram:123`. Such a document goes when you delete its collection. |
| Deleting a connection is refused | Running flows use it; the message names them. Stop them first. |
| **Create with AI** says it is busy | Another proposal is being made; wait for it. |
| **Create with AI** stops after 3 minutes | Use a faster model or a shorter description. |
