# Node reference

<!-- Generated from the node registry. Do not edit by hand: run
     go test -run TestNodeReferenceDocIsCurrent ./internal/node/builtin -update -->

Every node PiWorker ships, with its ports and settings. The editor shows the
same information, plus examples, behind the **?** button of each node's
configuration dialog. New to PiWorker? Start with
[Getting started](getting-started.md) and [Concepts](concepts.md).

**Risk** says what a node can affect: `safe` nodes only compute, read local
state or log; `external` nodes send data to other services; `privileged`
nodes act on the device itself (commands, files, databases, hardware). It
decides how [AI agents](agents.md) may use a flow.

## Triggers

A trigger starts a flow: it has no input and emits a message when something happens.

| Node | What it does |
|------|--------------|
| [Agent Tool](#trigger-tool) | Publish this flow as a tool that AI agents can call over MCP |
| [Cron Schedule](#trigger-cron) | Triggers based on a cron schedule expression |
| [DS18B20 Temperature](#trigger-ds18b20) | Poll a 1-Wire DS18B20 temperature sensor (experimental) |
| [GPIO Edge](#trigger-gpio) | Trigger when a GPIO input pin changes level (experimental) |
| [Interval Timer](#trigger-interval) | Triggers at regular time intervals |
| [MQTT Subscribe](#trigger-mqtt) | Trigger when a message arrives on an MQTT topic |
| [Manual Inject](#trigger-manual) | Trigger flow manually with a button click |
| [RSS Feed](#trigger-rss) | Trigger when an RSS/Atom feed gets a new item |
| [Sun](#trigger-sun) | Trigger at sunrise, sunset, dawn or dusk |
| [System Metric](#trigger-sysmetric) | Triggers when CPU, memory, or temperature crosses a threshold |
| [Webhook](#trigger-webhook) | Triggers when an HTTP request hits its webhook URL |
| [Zigbee Device](#trigger-zigbee) | Trigger when a Zigbee device reports a change or goes offline |

<a id="trigger-tool"></a>

### Agent Tool

Publish this flow as a tool that AI agents can call over MCP.

- **Type:** `trigger-tool`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Concurrent calls | `maxConcurrent` | integer (count) | – | Maximum number of calls of this tool running at once (count, 1-16); when empty: 1, or 4 for read-only tools |
| Confirmation | `confirm` | `none`, `client`, `admin` | `none` | none: runs when called; client: MCP clients ask their user first (not enforced); admin: every call waits for your approval in the editor |
| Cooldown | `cooldownMs` | integer (milliseconds) | `0` | Minimum time between the end of a call and the next one, in milliseconds (0-3600000) |
| Description (required) | `description` | string | – | What the tool does and when to use it (at least 20 characters); agents read it to decide when to call the tool |
| Idempotent | `idempotent` | boolean | `false` | Calling the tool again with the same arguments has no additional effect (advertised to agents as idempotentHint) |
| Input schema (required) | `inputSchema` | json | `{"additionalProperties":false,"properties":{},"type":"object"}` | JSON Schema of the arguments (closed subset: object root, typed properties, anchored patterns, arrays with maxItems); see the documentation |
| Output schema | `outputSchema` | json | – | Optional JSON Schema of the reply payload (type object) |
| Timeout | `timeoutMs` | integer (milliseconds) | `10000` | How long a call waits for the reply, in milliseconds (1000-30000); after that the call reports running and the result can be fetched later |
| Title | `title` | string | – | Short human-readable name shown by MCP clients (optional) |
| Tool name (required) | `toolName` | string | – | Name agents call the tool by: 1-128 letters, digits, _ . or -, unique across running flows, not starting with pw_ |

<a id="trigger-cron"></a>

### Cron Schedule

Triggers based on a cron schedule expression.

- **Type:** `trigger-cron`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Cron Expression (required) | `expression` | string | `*/5 * * * *` | Standard cron expression (minute hour day month weekday) |

<a id="trigger-ds18b20"></a>

### DS18B20 Temperature

Poll a 1-Wire DS18B20 temperature sensor (experimental).

- **Type:** `trigger-ds18b20`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Device ID | `deviceId` | string | – | 1-Wire device id (28-xxxx). Empty auto-discovers the first DS18B20. |
| Poll interval (s) (required) | `intervalSec` | number (seconds) | `10` | How often to read the sensor, in seconds |

<a id="trigger-gpio"></a>

### GPIO Edge

Trigger when a GPIO input pin changes level (experimental).

- **Type:** `trigger-gpio`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Edge (required) | `edge` | `rising`, `falling`, `both` | `both` | Which edge to trigger on |
| Pin (required) | `pin` | string | – | GPIO pin name, e.g. GPIO17 |
| Pull resistor | `pull` | `none`, `up`, `down` | `none` | Internal pull resistor |

<a id="trigger-interval"></a>

### Interval Timer

Triggers at regular time intervals.

- **Type:** `trigger-interval`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Interval (ms) (required) | `interval` | number (milliseconds) | `1000` | Time between triggers, in milliseconds (minimum 100) |

<a id="trigger-mqtt"></a>

### MQTT Subscribe

Trigger when a message arrives on an MQTT topic.

- **Type:** `trigger-mqtt`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Broker | `broker` | string | – | Broker URL, e.g. tcp://host:1883, when no connection is chosen |
| Client ID | `clientId` | string | – | Own MQTT client id; leave empty to share a connection with the other MQTT nodes on this broker |
| Connection | `connection` | string | – | Broker connection defined under Connections (its URL and credentials are used); leave empty to set a broker below |
| Password | `password` | string | – | Optional, with a broker URL only; use {{secret.NAME}} |
| QoS | `qos` | number | `0` | 0, 1 or 2 |
| Topic (required) | `topic` | string | – | Topic filter to subscribe to (supports +/# wildcards) |
| Username | `username` | string | – | Optional, with a broker URL only ({{secret.NAME}} supported) |

<a id="trigger-manual"></a>

### Manual Inject

Trigger flow manually with a button click.

- **Type:** `trigger-manual`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Payload | `payload` | string | `{}` | Default payload to send when triggered (JSON or text) |
| Payload Type | `payloadType` | `json`, `text` | `json` | Type of the payload |
| Topic | `topic` | string | `manual` | Message topic for routing |

<a id="trigger-rss"></a>

### RSS Feed

Trigger when an RSS/Atom feed gets a new item.

- **Type:** `trigger-rss`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Allow private hosts | `allowPrivate` | boolean | `false` | Permit fetching from private/loopback addresses (SSRF risk) |
| Feed URL (required) | `url` | string | – | RSS or Atom feed URL to poll |
| Poll interval (s) | `intervalSec` | number (seconds) | `300` | How often to poll the feed, in seconds (minimum 10) |

<a id="trigger-sun"></a>

### Sun

Trigger at sunrise, sunset, dawn or dusk.

- **Type:** `trigger-sun`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Event | `event` | `sunrise`, `sunset`, `dawn`, `dusk` | `sunset` | sunrise, sunset, or civil dawn / dusk (sun 6° below the horizon) |
| Latitude (required) | `latitude` | number (degrees) | – | Of the place, in degrees (south negative), e.g. -34.6 |
| Longitude (required) | `longitude` | number (degrees) | – | Of the place, in degrees (west negative), e.g. -58.4 |
| Offset (min) | `offset` | number (minutes) | `0` | Shift from the event, in minutes: negative before, positive after |

<a id="trigger-sysmetric"></a>

### System Metric

Triggers when CPU, memory, or temperature crosses a threshold.

- **Type:** `trigger-sysmetric`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Comparison | `comparison` | `above`, `below` | `above` | Fire when the value is above or below the threshold |
| Metric (required) | `metric` | `cpu`, `memory`, `temperature` | `cpu` | System metric to watch |
| Poll Interval (s) | `interval` | number (seconds) | `10` | How often to sample the metric, in seconds |
| Threshold (required) | `threshold` | number | `80` | CPU load, memory % (0-100), or temperature in °C |

<a id="trigger-webhook"></a>

### Webhook

Triggers when an HTTP request hits its webhook URL.

- **Type:** `trigger-webhook`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Method Filter | `method` | string | – | Only accept this HTTP method; leave empty to accept any |
| Path (required) | `path` | string | – | URL path segment under /webhooks/ (e.g. 'github-push') |
| Secret Token | `token` | string | – | Optional shared secret, required as ?token=... or the X-Webhook-Token header ({{secret.NAME}} supported) |

<a id="trigger-zigbee"></a>

### Zigbee Device

Trigger when a Zigbee device reports a change or goes offline.

- **Type:** `trigger-zigbee`
- **Risk:** `safe`
- **Inputs:** none
- **Outputs:** `output`, `availability`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Connection (required) | `connection` | string | – | Broker connection to Zigbee2MQTT (defined under Connections) |
| Device (required) | `device` | string | – | Zigbee2MQTT friendly name of the device or group |
| Minimum change | `minChange` | number | `0` | Numbers only: fire when the value moved at least this much (in the property's own unit) since it last fired; 0 for any change |
| Mode | `mode` | `change`, `every` | `change` | change: only when the value (or state) changes; every: on every report |
| Property | `property` | string | – | Only this property (temperature, humidity, state, power, action…); empty for the whole state |

## Processing

Processing nodes change, route or hold the messages that pass through them.

| Node | What it does |
|------|--------------|
| [AI Agent](#process-llm-agent) | A language model that calls your flow tools until it can answer |
| [Aggregate](#process-aggregate) | Collect N messages and emit them together as one array |
| [Debug](#process-debug) | Inspects and logs messages for debugging |
| [Delay](#process-delay) | Delays messages by a specified time |
| [Filter](#process-filter) | Pass a message through only when a condition is true |
| [Get Variable](#get-var) | Read a named variable into the payload |
| [Join](#process-join) | Correlate and merge messages by a key or by source node |
| [LLM Classify](#process-llm-classify) | Route a message by the category a language model picks |
| [LLM Prompt](#process-llm) | Ask a language model and pass on its answer (text or JSON) |
| [Parse](#process-parse) | Parse a JSON or CSV string payload into structured data |
| [Rate Limit](#process-ratelimit) | Pass at most N messages per time window, dropping the rest |
| [Search Knowledge Base](#process-knowledge-search) | Find the stored documents closest in meaning to a query |
| [Set Variable](#set-var) | Store a value into a named variable |
| [Switch (If/Else)](#process-switch) | Route a message to one of two outputs based on a condition |
| [Template](#process-template) | Render a text template from the message into a string |
| [Timer](#process-timer) | Fire once no message came for a while (retriggerable) |
| [Transcribe Audio](#process-transcribe) | Turn speech in an audio message into text |
| [Transform](#process-transform) | Transform payload using expressions |
| [Zigbee Get](#process-zigbee-get) | Read a Zigbee device's current value |

<a id="process-llm-agent"></a>

### AI Agent

A language model that calls your flow tools until it can answer.

- **Type:** `process-llm-agent`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Allow privileged tools | `allowPrivileged` | boolean | `false` | Let the agent call tools that run commands, write files or touch hardware |
| Connection (required) | `connection` | string | – | LLM connection to use (defined under LLM connections) |
| Effort | `effort` | `(empty)`, `low`, `medium`, `high`, `xhigh`, `max` | – | How much the model thinks before answering, only for models that support it: others may reject the request (empty: the model's default) |
| Max steps | `maxSteps` | integer (steps) | `8` | Most requests to the model before the agent gives up, in steps (1 to 32) |
| Max tokens | `maxTokens` | integer (tokens) | `4096` | Longest answer the model may give, in tokens (1 to 64000); a longer answer is cut |
| Memory key | `memoryKey` | string | – | Conversation to remember, e.g. telegram:{{payload.body.message.chat.id}} (supports {{templates}}; empty: no memory) |
| Memory length | `memoryTurns` | integer (messages) | `20` | How many past messages of the conversation to keep and send, in messages (2 to 200) |
| Model | `model` | string | – | Model to ask; it must support tool use (empty: the connection's default model) |
| Prompt (required) | `prompt` | string | `{{payload}}` | The request, built from the message (supports {{templates}}) |
| Result field | `resultField` | string | – | Payload field to store the result in, e.g. answer or ai.summary (empty: the result replaces the payload) |
| System prompt | `system` | string | – | Instructions for the agent: its role, what its tools are for, its rules (supports {{templates}}) |
| Timeout (s) | `timeout` | integer (seconds) | `300` | Longest time the whole call may take, in seconds (1 to 600) |
| Tools | `tools` | array | – | Flow tools the agent may call, by tool name (at most 64; tools that ask for confirmation are never called) |

<a id="process-aggregate"></a>

### Aggregate

Collect N messages and emit them together as one array.

- **Type:** `process-aggregate`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Batch size (required) | `count` | number | `10` | Number of messages to collect before emitting them as one array |

<a id="process-debug"></a>

### Debug

Inspects and logs messages for debugging.

- **Type:** `process-debug`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Debug Name | `name` | string | `Debug` | Identifier shown in logs to distinguish multiple debug nodes |
| Log Metadata | `logMeta` | boolean | `true` | Include message metadata in logs |
| Log Payload | `logPayload` | boolean | `true` | Include the message payload in logs |
| Pass Through | `passThrough` | boolean | `true` | Forward the message to the next node (disable to stop flow here) |

<a id="process-delay"></a>

### Delay

Delays messages by a specified time.

- **Type:** `process-delay`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Delay (ms) (required) | `delay` | number (milliseconds) | `1000` | Time to delay, in milliseconds (0-300000, i.e. at most 5 minutes) |

<a id="process-filter"></a>

### Filter

Pass a message through only when a condition is true.

- **Type:** `process-filter`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Condition (required) | `condition` | string | `true` | Boolean expression; the message passes only when true (e.g. payload.value > 10) |

<a id="get-var"></a>

### Get Variable

Read a named variable into the payload.

- **Type:** `get-var`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Default | `default` | string | – | Value used when the variable is not set |
| Variable Name (required) | `key` | string | – | Name of the variable to read |
| Variable scope | `scope` | `global`, `flow` | `global` | Global is shared by all flows; Flow is isolated to this saved flow. |

<a id="process-join"></a>

### Join

Correlate and merge messages by a key or by source node.

- **Type:** `process-join`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Correlation key (required) | `key` | string | `meta.correlationID` | Expression for the grouping key (e.g. payload.orderId or meta.correlationID) |
| Count (key mode) | `count` | number | `2` | Emit an array once this many messages share a key |
| Max pending keys | `maxKeys` | number | `1000` | Maximum incomplete groups kept; the oldest is dropped beyond this |
| Mode (required) | `mode` | `key`, `source` | `key` | key: collect N by a key; source: one per source node |
| Sources (source mode) | `sources` | string | – | Comma-separated source node IDs to await before emitting an object |
| Timeout (ms) | `timeoutMs` | number (milliseconds) | `300000` | Drop an incomplete group this long after its first message, in milliseconds |

<a id="process-llm-classify"></a>

### LLM Classify

Route a message by the category a language model picks.

- **Type:** `process-llm-classify`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `other`, then one per item of `categories`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Categories (required) | `categories` | array | – | The categories, one output port each (1 to 20 names of up to 64 characters; "other" is always there) |
| Connection (required) | `connection` | string | – | LLM connection to use (defined under LLM connections) |
| Input | `input` | string | `{{payload}}` | The text to classify, built from the message (supports {{templates}}) |
| Instructions | `instructions` | string | – | Optional guidance: what each category means, examples (supports {{templates}}) |
| Model | `model` | string | – | Model to ask (empty: the connection's default model) |
| Timeout (s) | `timeout` | integer (seconds) | `120` | Longest time the whole call may take, in seconds (1 to 600) |

<a id="process-llm"></a>

### LLM Prompt

Ask a language model and pass on its answer (text or JSON).

- **Type:** `process-llm`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Connection (required) | `connection` | string | – | LLM connection to use (defined under LLM connections) |
| Effort | `effort` | `(empty)`, `low`, `medium`, `high`, `xhigh`, `max` | – | How much the model thinks before answering, only for models that support it: others may reject the request (empty: the model's default) |
| Images | `images` | string | – | Message path of the images to send, e.g. payload.image: base64, a data URL, an http(s) URL or a list of them (empty: none) |
| JSON schema | `schema` | json | – | Optional JSON Schema the answer must match (output json), e.g. {"type": "object", "properties": {...}} |
| Max tokens | `maxTokens` | integer (tokens) | `1024` | Longest answer the model may give, in tokens (1 to 64000); a longer answer is cut |
| Memory key | `memoryKey` | string | – | Conversation to remember, e.g. telegram:{{payload.body.message.chat.id}} (supports {{templates}}; empty: no memory) |
| Memory length | `memoryTurns` | integer (messages) | `20` | How many past messages of the conversation to keep and send, in messages (2 to 200) |
| Model | `model` | string | – | Model to ask (empty: the connection's default model) |
| Output | `output` | `text`, `json` | `text` | text: the answer as text; json: the answer parsed as a JSON object |
| Prompt (required) | `prompt` | string | `{{payload}}` | What to ask, built from the message (supports {{templates}}) |
| Result field | `resultField` | string | – | Payload field to store the result in, e.g. answer or ai.summary (empty: the result replaces the payload) |
| System prompt | `system` | string | – | Instructions for the model: its role, tone and rules (supports {{templates}}) |
| Timeout (s) | `timeout` | integer (seconds) | `120` | Longest time the whole call may take, in seconds (1 to 600) |

<a id="process-parse"></a>

### Parse

Parse a JSON or CSV string payload into structured data.

- **Type:** `process-parse`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| CSV delimiter | `delimiter` | string | `,` | Field delimiter for CSV (default ,) |
| CSV has header row | `header` | boolean | `true` | When parsing CSV, treat the first row as column names (produces objects) |
| Format (required) | `format` | `json`, `csv` | `json` | Input text format to parse |

<a id="process-ratelimit"></a>

### Rate Limit

Pass at most N messages per time window, dropping the rest.

- **Type:** `process-ratelimit`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Limit (required) | `limit` | number | `1` | Maximum messages allowed per window |
| Window (ms) (required) | `windowMs` | number (milliseconds) | `1000` | Sliding window length, in milliseconds |

<a id="process-knowledge-search"></a>

### Search Knowledge Base

Find the stored documents closest in meaning to a query.

- **Type:** `process-knowledge-search`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Collection (required) | `collection` | string | – | Collection to search, e.g. manuals |
| Connection (required) | `connection` | string | – | LLM connection to use (defined under LLM connections) |
| Minimum score | `minScore` | number | `0` | Leave out documents less similar than this, from 0 (keep all) to 1 (identical) |
| Model | `model` | string | – | Embedding model of the collection (empty: the model the collection was built with) |
| Query (required) | `query` | string | `{{payload}}` | What to look for, built from the message (supports {{templates}}) |
| Result field | `resultField` | string | – | Payload field to store the result in, e.g. answer or ai.summary (empty: the result replaces the payload) |
| Results | `topK` | integer (documents) | `4` | Most documents to return, best first, in documents (1 to 50) |
| Timeout (s) | `timeout` | integer (seconds) | `120` | Longest time the whole call may take, in seconds (1 to 600) |

<a id="set-var"></a>

### Set Variable

Store a value into a named variable.

- **Type:** `set-var`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Value Expression | `value` | string | `payload` | Expression evaluated to compute the stored value (e.g. payload.count) |
| Variable Name (required) | `key` | string | – | Name of the variable to store |
| Variable scope | `scope` | `global`, `flow` | `global` | Global is shared by all flows; Flow is isolated to this saved flow. |

<a id="process-switch"></a>

### Switch (If/Else)

Route a message to one of two outputs based on a condition.

- **Type:** `process-switch`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `true`, `false`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Condition (required) | `condition` | string | `true` | Boolean expression; true routes to the True port, false to the False port |

<a id="process-template"></a>

### Template

Render a text template from the message into a string.

- **Type:** `process-template`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Template (required) | `template` | string | – | Text with {{payload.field}}, {{topic}}, {{meta.key}}, {{id}}, {{timestamp}} placeholders |

<a id="process-timer"></a>

### Timer

Fire once no message came for a while (retriggerable).

- **Type:** `process-timer`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Duration | `duration` | number | `5` | How long without a new message before it fires (in the unit below; at most 24 hours) |
| Restart on each message | `restart` | boolean | `true` | Each message starts the countdown again; off: a running timer is not extended |
| Unit | `durationUnit` | `seconds`, `minutes`, `hours` | `minutes` | Unit of the duration |

<a id="process-transcribe"></a>

### Transcribe Audio

Turn speech in an audio message into text.

- **Type:** `process-transcribe`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Audio (required) | `audio` | string | `payload` | Message path of the audio, e.g. payload or payload.voice: base64, a data URL, bytes or an http(s) URL (at most 25 MiB) |
| Connection (required) | `connection` | string | – | LLM connection to use (defined under LLM connections) |
| Language | `language` | string | – | Language spoken, as an ISO-639-1 code such as en or es (empty: detected); naming it improves accuracy |
| Model | `model` | string | – | Transcription model, e.g. whisper-1 (empty: the connection's default model, else whisper-1) |
| Result field | `resultField` | string | – | Payload field to store the result in, e.g. answer or ai.summary (empty: the result replaces the payload) |
| Timeout (s) | `timeout` | integer (seconds) | `120` | Longest time the whole call may take, in seconds (1 to 600) |

<a id="process-transform"></a>

### Transform

Transform payload using expressions.

- **Type:** `process-transform`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Expression (required) | `expression` | string | `payload` | Expression to evaluate (e.g., payload.price * 1.21, upper(payload.name)) |

<a id="process-zigbee-get"></a>

### Zigbee Get

Read a Zigbee device's current value.

- **Type:** `process-zigbee-get`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Connection (required) | `connection` | string | – | Broker connection to Zigbee2MQTT (defined under Connections) |
| Device (required) | `device` | string | – | Zigbee2MQTT friendly name of the device or group |
| Property | `property` | string | `state` | The property to read (state, power, energy…) |
| Timeout (s) | `timeout` | number (seconds) | `5` | How long to wait for the answer, in seconds |

## Actions

Actions do something with a message: notify, call a service, write, switch a pin.

| Node | What it does |
|------|--------------|
| [Add to Knowledge Base](#action-knowledge-add) | Store text in the local knowledge base for later searches |
| [Agent Reply](#action-reply) | Answer the agent tool call that started this run |
| [Database](#action-database) | Run a SQL query or statement against SQLite, Postgres or MySQL |
| [Email](#action-email) | Send an email via SMTP |
| [Execute Command](#action-command) | Executes a shell command |
| [Forget Conversation](#action-llm-forget) | Clear a conversation the LLM nodes remember |
| [GPIO Write](#action-gpio) | Set a GPIO output pin high or low (experimental) |
| [HTTP Request](#action-http) | Makes HTTP requests to external APIs |
| [I2C Read](#action-i2c) | Read bytes from an I2C device (experimental) |
| [Log](#action-log) | Logs messages to the console/file |
| [MQTT Publish](#action-mqtt) | Publish a message to an MQTT topic |
| [Notify](#action-notify) | Send a notification to Discord, Slack, or ntfy |
| [Telegram](#action-telegram) | Send a message via a Telegram bot |
| [Write File](#action-file) | Write or append the message content to a file |
| [Zigbee Set](#action-zigbee) | Turn a Zigbee device on or off or set one of its properties |

<a id="action-knowledge-add"></a>

### Add to Knowledge Base

Store text in the local knowledge base for later searches.

- **Type:** `action-knowledge-add`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Chunk size | `chunkSize` | integer (characters) | `0` | Split long texts into chunks of at most this many characters, each one a document (0: one document; else 200 to 8000 characters) |
| Collection (required) | `collection` | string | – | Collection to store the text in, e.g. manuals (created on the first document) |
| Connection (required) | `connection` | string | – | LLM connection to use (defined under LLM connections) |
| Document ID | `documentId` | string | – | ID of the document, e.g. {{payload.id}}: adding the same ID again replaces it; it may not end in '#' and a number, the form of chunk IDs (supports {{templates}}; empty: a new ID per message) |
| Metadata | `metadata` | object | – | Names and values stored with the document and returned by searches, e.g. source = {{topic}} (values support {{templates}}) |
| Model (required) | `model` | string | – | Embedding model, e.g. text-embedding-3-small (OpenAI) or nomic-embed-text (Ollama); every document of a collection uses the same one |
| Text | `text` | string | `{{payload}}` | Text to store, built from the message (supports {{templates}}) |
| Timeout (s) | `timeout` | integer (seconds) | `120` | Longest time the whole call may take, in seconds (1 to 600) |

<a id="action-reply"></a>

### Agent Reply

Answer the agent tool call that started this run.

- **Type:** `action-reply`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** none

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Mode | `mode` | `result`, `error` | `result` | result: the payload is the tool's result; error: the call fails with the payload as its error |

<a id="action-database"></a>

### Database

Run a SQL query or statement against SQLite, Postgres or MySQL.

- **Type:** `action-database`
- **Risk:** `privileged`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| DSN (required) | `dsn` | string | – | Connection string (supports {{secret.NAME}} for credentials) |
| Driver (required) | `driver` | `sqlite`, `postgres`, `mysql`, `sqlite3`, `postgresql` | `sqlite` | Database driver (sqlite3 and postgresql are accepted aliases) |
| Mode | `mode` | `query`, `exec` | `query` | query returns rows; exec returns affected-row counts |
| Parameters | `params` | string | – | Expression returning an array of bind parameters, e.g. [payload.id, payload.name] |
| SQL (required) | `query` | string | – | SQL statement with ? / $1 placeholders for parameters |

<a id="action-email"></a>

### Email

Send an email via SMTP.

- **Type:** `action-email`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Body | `body` | string | `{{payload}}` | Body (supports templates) |
| From (required) | `from` | string | – | Sender address |
| Password | `password` | string | – | SMTP password (optional; use {{secret.NAME}}) |
| SMTP Host (required) | `host` | string | – | SMTP server hostname |
| SMTP Port | `port` | number | `587` | Submission port (587 for STARTTLS, 465 for implicit TLS) |
| Subject | `subject` | string | `PiWorker notification` | Subject (supports templates) |
| TLS | `tls` | `auto`, `implicit`, `starttls` | `auto` | auto: implicit TLS on 465, STARTTLS when offered otherwise; implicit; starttls (required) |
| To (required) | `to` | string | – | Recipient address(es), comma-separated |
| Username | `username` | string | – | SMTP username (optional; {{secret.NAME}} supported) |

<a id="action-command"></a>

### Execute Command

Executes a shell command.

- **Type:** `action-command`
- **Risk:** `privileged`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Allow values starting with '-' | `allowDashArgs` | boolean | `false` | Risky: let a placeholder that begins an argument take a value starting with '-', which most programs read as an option (e.g. -rf). Only enable it when the command ends its options with -- before the placeholders. Not needed behind literal text such as temp={{payload}}. |
| Command (required) | `command` | string | – | Shell command to execute. Use {{payload}}, {{topic}}, {{meta.key}} for variable substitution. |
| Shell | `shell` | string | `/bin/sh` | Path to the shell interpreter |
| Timeout (seconds) | `timeout` | number (seconds) | `30` | Maximum execution time, in seconds (1-3600) |

<a id="action-llm-forget"></a>

### Forget Conversation

Clear a conversation the LLM nodes remember.

- **Type:** `action-llm-forget`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Memory key (required) | `memoryKey` | string | – | Conversation to forget: the memory key of the LLM node, e.g. telegram:{{payload.body.message.chat.id}} (supports {{templates}}) |

<a id="action-gpio"></a>

### GPIO Write

Set a GPIO output pin high or low (experimental).

- **Type:** `action-gpio`
- **Risk:** `privileged`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Pin (required) | `pin` | string | – | GPIO pin name, e.g. GPIO17 |
| Value (required) | `value` | `high`, `low`, `payload` | `high` | high, low, or payload (use the message payload's truthiness) |

<a id="action-http"></a>

### HTTP Request

Makes HTTP requests to external APIs.

- **Type:** `action-http`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Allow Private Addresses | `allowPrivate` | boolean | `false` | Allow requests to loopback/private/link-local/CGNAT and other reserved addresses (needed for LAN/local services). Off by default to prevent SSRF; while off, HTTP(S)_PROXY settings are ignored. |
| Body | `body` | string | – | Request body (supports templates, for POST/PUT/PATCH) |
| Body Type | `bodyType` | `json`, `text`, `form` | `json` | Content type for the request body |
| Headers | `headers` | object | – | HTTP headers (name → value pairs); values support {{secret.NAME}} and message templates |
| Max Response Size (bytes) | `maxResponseSize` | number (bytes) | `10485760` | Maximum response body size to read, in bytes (protects against memory exhaustion) |
| Method | `method` | `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD` | `GET` | HTTP method |
| Output Mode | `outputMode` | `body`, `full`, `status` | `body` | What to return: full response, just body, or just status |
| Retry on Failure | `retryable` | boolean | `false` | Retry transient failures even for non-idempotent methods (POST/PATCH). May cause duplicate requests. |
| Timeout (ms) | `timeout` | number (milliseconds) | `30000` | Request timeout, in milliseconds (1000-60000) |
| URL (required) | `url` | string | – | Request URL (supports {{payload.field}} templates; {{secret.NAME}} only in the user:password@ part and the query string, and only when the URL starts with a literal scheme and host) |

<a id="action-i2c"></a>

### I2C Read

Read bytes from an I2C device (experimental).

- **Type:** `action-i2c`
- **Risk:** `privileged`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Address (required) | `address` | string | – | Device address, e.g. 0x76 |
| Bus | `bus` | string | – | I2C bus name; empty uses the first bus |
| Length (required) | `length` | number (bytes) | `1` | Number of bytes to read |
| Register | `register` | string | – | Optional register to read from, e.g. 0xD0 |

<a id="action-log"></a>

### Log

Logs messages to the console/file.

- **Type:** `action-log`
- **Risk:** `safe`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Log Level | `level` | `debug`, `info`, `warn`, `error` | `info` | The severity level for the log message |
| Message | `message` | string | – | Custom log message (optional) |

<a id="action-mqtt"></a>

### MQTT Publish

Publish a message to an MQTT topic.

- **Type:** `action-mqtt`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Broker | `broker` | string | – | Broker URL, e.g. tcp://host:1883, when no connection is chosen |
| Client ID | `clientId` | string | – | Own MQTT client id; leave empty to share a connection with the other MQTT nodes on this broker |
| Connection | `connection` | string | – | Broker connection defined under Connections (its URL and credentials are used); leave empty to set a broker below |
| Password | `password` | string | – | Optional, with a broker URL only; use {{secret.NAME}} |
| Payload | `payload` | string | `{{payload}}` | Message payload (supports {{templates}}) |
| QoS | `qos` | number | `0` | 0, 1 or 2 |
| Retained | `retained` | boolean | `false` | Set the retained flag |
| Topic (required) | `topic` | string | – | Topic to publish to (supports {{templates}}) |
| Username | `username` | string | – | Optional, with a broker URL only ({{secret.NAME}} supported) |

<a id="action-notify"></a>

### Notify

Send a notification to Discord, Slack, or ntfy.

- **Type:** `action-notify`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Allow Private Addresses | `allowPrivate` | boolean | `false` | Allow notifying loopback/private hosts (off by default to prevent SSRF) |
| Format | `format` | `discord`, `slack`, `ntfy`, `raw` | `discord` | Payload format for the destination service |
| Message | `message` | string | `{{payload}}` | Message text (supports {{payload.field}} templates) |
| Title | `title` | string | – | Optional title (ntfy only) |
| Webhook URL (required) | `url` | string | – | The Discord/Slack/ntfy webhook URL (it embeds a token; use {{secret.NAME}}) |

<a id="action-telegram"></a>

### Telegram

Send a message via a Telegram bot.

- **Type:** `action-telegram`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Bot Token (required) | `botToken` | string | – | Telegram bot token from @BotFather (use {{secret.NAME}}) |
| Chat ID (required) | `chatId` | string | – | Destination chat/channel ID |
| Message | `message` | string | `{{payload}}` | Message text (supports {{payload.field}} templates) |
| Parse Mode | `parseMode` | `(empty)`, `Markdown`, `MarkdownV2`, `HTML` | – | Optional text formatting |

<a id="action-file"></a>

### Write File

Write or append the message content to a file.

- **Type:** `action-file`
- **Risk:** `privileged`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Append Newline | `newline` | boolean | `true` | Append a trailing newline if missing |
| Content | `content` | string | `{{payload}}` | Content to write (supports {{payload.field}} templates) |
| File Path (required) | `path` | string | – | Absolute or relative path of the file to write |
| Mode | `mode` | `append`, `overwrite` | `append` | append to the file or overwrite it |

<a id="action-zigbee"></a>

### Zigbee Set

Turn a Zigbee device on or off or set one of its properties.

- **Type:** `action-zigbee`
- **Risk:** `external`
- **Inputs:** `input`
- **Outputs:** `output`

| Setting | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| Command | `command` | `on`, `off`, `toggle`, `set`, `raw` | `toggle` | on, off, toggle (the state), set (one property) or raw (a JSON object) |
| Connection (required) | `connection` | string | – | Broker connection to Zigbee2MQTT (defined under Connections) |
| Device (required) | `device` | string | – | Zigbee2MQTT friendly name of the device or group (supports {{templates}}) |
| Payload | `payload` | string | – | For raw: a JSON object sent as is (supports {{templates}}) |
| Property | `property` | string | – | For set: the property to set (brightness, color_temp, power_on_behavior…) |
| Value | `value` | string | – | For set: the value (supports {{templates}}; numbers and true/false are sent as such) |

