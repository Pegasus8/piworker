# Recipes

Complete flows to copy and adapt. Each one lists the nodes in order, with the
settings that matter; anything not mentioned keeps its default. Connect the
nodes from left to right unless a recipe says otherwise.

Build them the way [Getting started](getting-started.md#4-build-your-first-flow)
shows: add the nodes in order (each one is connected after the selected one),
fill in their settings, **Save**, **Deploy**.

- [Tell me when the Pi overheats](#tell-me-when-the-pi-overheats)
- [React to a webhook only when it matters](#react-to-a-webhook-only-when-it-matters)
- [Fetch something every morning and send it to Telegram](#fetch-something-every-morning-and-send-it-to-telegram)
- [Log a temperature sensor to a file](#log-a-temperature-sensor-to-a-file)
- [Switch a pin from a button](#switch-a-pin-from-a-button)
- [Keep a threshold in a variable](#keep-a-threshold-in-a-variable)
- [Avoid a flood of notifications](#avoid-a-flood-of-notifications)

With a language model (see [AI in flows](ai.md) first: these need an LLM
connection):

- [A Telegram chat bot with memory](#a-telegram-chat-bot-with-memory)
- [Route support e-mails by topic](#route-support-e-mails-by-topic)
- [An agent that switches the lights](#an-agent-that-switches-the-lights)
- [Answer questions from your documents](#answer-questions-from-your-documents)

## Tell me when the Pi overheats

Sends a push notification to your phone through [ntfy](https://ntfy.sh) when
the CPU passes 70 °C.

| # | Node | Settings |
|---|------|----------|
| 1 | **System Metric** | Metric `temperature` · Comparison `above` · Threshold `70` · Poll Interval `30` |
| 2 | **Template** | `Pi is at {{payload.value}} °C` |
| 3 | **Notify** | Format `ntfy` · Webhook URL `{{secret.ntfy_url}}` · Title `PiWorker` |

Before deploying, add a secret named `ntfy_url` under **Secrets** with the
value `https://ntfy.sh/<a-topic-name-only-you-know>`, and subscribe to the
same topic in the ntfy app.

System Metric fires **once** each time the value crosses the threshold, not on
every reading, so you get one notification per overheating episode. For
Discord or Slack, change the format and store that service's webhook URL in
the secret instead.

## React to a webhook only when it matters

Another service or device calls a URL on your Pi; the flow acts only on some
calls.

| # | Node | Settings |
|---|------|----------|
| 1 | **Webhook** | Path `temperature` · Secret Token `{{secret.hook_token}}` |
| 2 | **Switch (If/Else)** | Condition `payload.body.temperature > 30` |
| 3 | **Template** (from the Switch's **True** port) | `Hot: {{payload.body.temperature}} °C in {{payload.body.room}}` |
| 4 | **Notify** | as in the previous recipe |

Add the secret `hook_token` first (any long random text). Then call it:

```bash
curl -X POST "http://raspberrypi.local:8080/webhooks/temperature?token=YOUR_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"temperature": 31.5, "room": "office"}'
```

- The JSON you send arrives in `payload.body`; query parameters in
  `payload.query`.
- Port 8080 is plain HTTP, for devices that cannot check PiWorker's
  certificate; the token then travels unencrypted. Callers that can use HTTPS
  should call `https://<device>:8443/webhooks/temperature`.
- `404` means no deployed flow has that path; `401` means the token is missing
  or wrong.
- Connect the Switch's **False** port to something else if the other case
  also needs handling, or leave it unconnected to ignore it.

## Fetch something every morning and send it to Telegram

| # | Node | Settings |
|---|------|----------|
| 1 | **Cron Schedule** | Cron Expression `0 8 * * *` (every day at 8:00) |
| 2 | **HTTP Request** | Method `GET` · URL of a JSON API, for example `https://api.example.com/today` |
| 3 | **Template** | `Today: {{payload.summary}}` (use the fields your API returns) |
| 4 | **Telegram** | Bot Token `{{secret.telegram_token}}` · Chat ID your chat's ID |

After HTTP Request, the payload is the response: a JSON answer becomes an
object whose fields you reach as `payload.field`. To see what the API returns,
put a **Debug** node after HTTP Request first and trigger the flow with a
**Manual Inject** instead of waiting for the morning.

Cron expressions have five fields: minute, hour, day of month, month, day of
week. `*/15 * * * *` is every 15 minutes; `0 9 * * 1` is Mondays at 9:00.

Requests to addresses inside your own network (`192.168.…`, `localhost`) are
refused unless you turn on **Allow Private Addresses** in the HTTP Request
node.

## Log a temperature sensor to a file

Needs a DS18B20 sensor and 1-Wire enabled on the Pi. The DS18B20 node is
[experimental](troubleshooting.md#a-gpio-i2c-or-sensor-node-fails).

| # | Node | Settings |
|---|------|----------|
| 1 | **DS18B20 Temperature** | Poll interval `60` |
| 2 | **Template** | `{{timestamp}},{{payload.celsius}}` |
| 3 | **Write File** | File Path `/var/lib/piworker/temperature.csv` · Mode `append` |

Each reading adds a line such as `2026-11-11T08:00:00Z,21.4`. The file must be
in a directory the PiWorker service user can write to.

## Switch a pin from a button

A button on GPIO17 turns a relay on GPIO27 on while it is pressed. The GPIO
nodes are [experimental](troubleshooting.md#a-gpio-i2c-or-sensor-node-fails).

| # | Node | Settings |
|---|------|----------|
| 1 | **GPIO Edge** | Pin `GPIO17` · Edge `both` · Pull resistor `up` |
| 2 | **Transform** | `not payload.level` (`payload.level` is `true` when the pin reads high; with a pull-up, a pressed button reads low) |
| 3 | **GPIO Write** | Pin `GPIO27` · Value `payload` |

GPIO Write with the value `payload` sets the pin high when the payload is
true and low when it is false. The service user needs permission to use GPIO:
see [Troubleshooting](troubleshooting.md#a-gpio-i2c-or-sensor-node-fails).

## Keep a threshold in a variable

Change a limit without editing the flow.

1. Under **Variables**, create the global variable `threshold` with the JSON
   value `25`.
2. In any flow, use it in a **Filter**:

   ```
   payload.temperature > (vars["threshold"] ?? 25)
   ```

   `?? 25` is the value used while the variable does not exist.
3. Changing the variable takes effect on the next message. No redeploy.

To remember a value **from** a flow, add **Set Variable** (Variable Name
`lastTemperature`, Value Expression `payload.temperature`); read it elsewhere
as `vars["lastTemperature"]` or `{{vars.lastTemperature}}`.

## Avoid a flood of notifications

Put a **Rate Limit** node before the action: Limit `1`, Window `600000`
(10 minutes, in milliseconds) lets one message through per ten minutes and
drops the rest. To collect messages and send them together instead, use
**Aggregate** (Batch size `10`): it waits for ten messages and passes them on
as one list.

## A Telegram chat bot with memory

A bot you chat with on Telegram that remembers the conversation. It answers
only you: anyone can find a bot and write to it, and each message spends
tokens.

You need a bot token from [@BotFather](https://t.me/BotFather) (store it
as the secret `telegram_token`), a secret `telegram_hook` with a long random
text, an LLM connection named `claude` (any connection works), and your
chat ID (below).

| # | Node | Settings |
|---|------|----------|
| 1 | **Webhook** | Path `telegram-bot` · Method Filter `POST` · Secret Token `{{secret.telegram_hook}}` |
| 2 | **Filter** | `payload.body.message != nil && payload.body.message.text != nil && payload.body.message.chat.id == 123456789` (your chat ID) |
| 3 | **Switch (If/Else)** | `payload.body.message.text == "/reset"` |
| 4 | **Forget Conversation** (from the Switch's **True** port) | Memory key `telegram:{{payload.body.message.chat.id}}` |
| 5 | **Telegram** (after 4) | Bot Token `{{secret.telegram_token}}` · Chat ID your chat ID · Message `Starting over.` |
| 6 | **LLM Prompt** (from the Switch's **False** port) | Connection `claude` · System prompt `You are a helpful assistant in a Telegram chat. Answer briefly, in the language of the message.` · Prompt `{{payload.body.message.text}}` · Memory key `telegram:{{payload.body.message.chat.id}}` · Result field `answer` |
| 7 | **Telegram** (after 6) | Bot Token `{{secret.telegram_token}}` · Chat ID your chat ID · Message `{{payload.answer}}` |

Telegram delivers messages to a webhook only at a public HTTPS address with
a certificate it trusts. Put a reverse proxy or a tunnel with a public
certificate in front of PiWorker that forwards only the `/webhooks/` path,
then tell Telegram the address (with your bot token and the webhook token):

```bash
curl "https://api.telegram.org/bot<BOT_TOKEN>/setWebhook" \
  --data-urlencode "url=https://bot.example.com/webhooks/telegram-bot?token=<WEBHOOK_TOKEN>" \
  --data-urlencode 'allowed_updates=["message"]'
```

- To find your chat ID, deploy the flow first with a **Debug** node after
  the Webhook, send the bot a message and read `payload.body.message.chat.id`
  in **Activity → Debug**.
- Each chat has its own memory, because the key contains the chat ID.
  `/reset` clears it. **AI memory** (in the navigation, under Settings)
  shows the conversations.
- **Memory length** (default 20 messages) is what the model sees of the
  past; a longer memory spends more tokens on every message.
- Connect the LLM Prompt's **On error** port to a Telegram node with
  `Sorry, that did not work: {{payload.message}}` to hear about a spent
  daily limit or a provider error.

## Route support e-mails by topic

Sends each support e-mail to the team it is about. It assumes your mail
provider or helpdesk posts each new e-mail to a webhook as JSON with
`from`, `subject` and `text` (adapt the field names to yours).

| # | Node | Settings |
|---|------|----------|
| 1 | **Webhook** | Path `support-mail` · Secret Token `{{secret.mail_hook}}` |
| 2 | **LLM Classify** | Connection `claude` · Model a small, fast model · Categories `billing`, `technical`, `sales` · Instructions `billing: invoices, payments, refunds. technical: errors, setup, how-to questions. sales: prices, quotes, new orders.` · Input `{{payload.body.subject}} {{payload.body.text}}` |
| 3 | **Email** (from **billing**) | To `billing@example.com` · Subject `[billing] {{payload.body.subject}}` · Body `From {{payload.body.from}}: {{payload.body.text}}` |
| 4 | **Email** (from **technical**) | as 3, to `support@example.com` |
| 5 | **Email** (from **sales**) | as 3, to `sales@example.com` |
| 6 | **Email** (from **other**) | as 3, to `office@example.com` |
| 7 | **Email** (from the Classify node's **On error**) | To `office@example.com` · Subject `[unsorted] {{payload.input.body.subject}}` · Body `Not sorted ({{payload.message}}). From {{payload.input.body.from}}: {{payload.input.body.text}}` |

Fill in each Email's SMTP settings (password as `{{secret.smtp_password}}`).

- Each category is an output port of LLM Classify, plus **other** for
  e-mails that fit none. The message leaves unchanged, so the Email nodes
  use the original fields.
- Connecting **On error** to the general inbox means no e-mail is lost when
  the provider is down or the daily limit is spent. The failed message
  carries the e-mail under `payload.input` and the error in
  `payload.message`.
- To send doubtful cases to a person, put a **Switch** on
  `meta.llm.confidence >= 0.7` after a category port: **True** goes to the
  team, **False** to the general inbox.
- The subject and text of each e-mail are sent to the connection's
  provider. With a local model (Ollama) they stay on your network.

## An agent that switches the lights

Tell the Telegram bot of the first recipe "turn off the kitchen light" and
an AI Agent does it, through a tool flow that can only switch the lights
you list. It needs the Zigbee setup of the [Zigbee guide](zigbee.md), with
lights named `living_room_light`, `kitchen_light` and `bedroom_light` in
Zigbee2MQTT and a broker connection `home`.

First the tool, as its own flow. Deploy it before the agent's flow:

| # | Node | Settings |
|---|------|----------|
| 1 | **Agent Tool** | Tool name `lights_set` · Description `Turns a light of the house on or off. Use it when the user asks to switch a light.` · Input schema below · Cooldown `2000` |
| 2 | **Zigbee Set** | Connection `home` · Device `{{payload.room}}_light` · Command `set` · Property `state` · Value `{{payload.state}}` |
| 3 | **Agent Reply** | leave as it is |

```json
{
  "type": "object",
  "required": ["room", "state"],
  "additionalProperties": false,
  "properties": {
    "room": {"type": "string", "enum": ["living_room", "kitchen", "bedroom"]},
    "state": {"type": "string", "enum": ["ON", "OFF"]}
  }
}
```

Then, in the chat bot's flow, replace the **LLM Prompt** (node 6) with an
**AI Agent**:

| Setting | Value |
|---------|-------|
| Connection | `claude` (the model must support tool use) |
| System prompt | `You control the lights of the house with your tools. The rooms are living_room, kitchen and bedroom. Answer in one short sentence.` |
| Prompt | `{{payload.body.message.text}}` |
| Tools | `lights_set` |
| Max steps | `4` |
| Memory key | `telegram:{{payload.body.message.chat.id}}` |
| Result field | `answer` |

- The tool's `enum`s are the real limit: whatever a message says, the agent
  can only switch these three lights on or off. The Filter on your chat ID
  keeps everyone else out.
- The tool's **Confirmation** stays `none` (an agent node cannot wait for a
  confirmation) and it is not privileged, so **Allow privileged tools**
  stays off.
- Each call is a run of the tool's flow in its **Activity**; the agent's
  `meta.llm.steps` lists the calls it made.
- MCP does not need to be on. If it is, the same tool can also be granted
  to Claude Desktop (see [AI agents](agents.md)).

## Answer questions from your documents

Store your manuals or notes in PiWorker's knowledge base, then ask the chat
bot of the first recipe about them. You need an embedding model: an Ollama
connection `ollama` with `nomic-embed-text` pulled (`ollama pull
nomic-embed-text`), or an OpenAI connection with `text-embedding-3-small`.

A flow that stores documents:

| # | Node | Settings |
|---|------|----------|
| 1 | **Webhook** | Path `kb-add` · Secret Token `{{secret.kb_token}}` |
| 2 | **Add to Knowledge Base** | Connection `ollama` · Model `nomic-embed-text` · Collection `manuals` · Text `{{payload.body.title}}: {{payload.body.text}}` · Document ID `{{payload.body.id}}` · Chunk size `1000` · Metadata `source` = `{{payload.body.title}}` |

Add a document (up to 1 MiB per request; add `--cacert piworker-ca.crt`,
from **Trust this device**, if your computer does not trust PiWorker yet):

```bash
curl -X POST "https://raspberrypi.local:8443/webhooks/kb-add?token=YOUR_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"id": "boiler", "title": "Boiler manual", "text": "The boiler is serviced every October. ..."}'
```

Then, in the chat bot's flow, put two nodes between the Switch's **False**
port and the Telegram reply, instead of the LLM Prompt:

| # | Node | Settings |
|---|------|----------|
| 1 | **Search Knowledge Base** | Connection `ollama` · Collection `manuals` · Query `{{payload.body.message.text}}` · Results `4` · Result field `kb` |
| 2 | **LLM Prompt** | Connection `claude` · System prompt `Answer only from these excerpts of our manuals. If they do not contain the answer, say you do not know.` followed by a blank line and `{{payload.kb.context}}` · Prompt `{{payload.body.message.text}}` · Result field `answer` |

- Sending the same `id` again replaces the document and all its chunks, so
  updating a manual leaves no old passages behind.
- **Minimum score** on the search drops weak matches; good values depend on
  the embedding model (try `0.3` and look at the scores in
  `payload.kb.results` with a **Debug** node).
- The search runs on the device and only the question is embedded. The
  excerpts found go to the answering model in its prompt: with Ollama for
  both connections nothing leaves your network.
- **AI memory → Knowledge base** lists the documents and deletes them.
