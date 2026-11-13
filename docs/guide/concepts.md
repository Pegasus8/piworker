# Concepts

Everything you need to read a flow and write your own. If you have not built a
flow yet, do [Getting started](getting-started.md) first.

- [Flows, nodes and connections](#flows-nodes-and-connections)
- [Messages](#messages)
- [Templates and expressions](#templates-and-expressions)
- [Saving, deploying and stopping](#saving-deploying-and-stopping)
- [Seeing what happened](#seeing-what-happened)
- [Variables](#variables)
- [Secrets](#secrets)
- [Broker connections](#broker-connections)
- [Versions and history](#versions-and-history)

## Flows, nodes and connections

A **flow** is one automation. It is made of **nodes** joined by
**connections**:

| Kind | Colour | What it does | Examples |
|------|--------|--------------|----------|
| **Trigger** | orange | Starts the flow when something happens. It has no input. | Interval Timer, Cron Schedule, Webhook, GPIO Edge |
| **Processing** | blue | Changes, checks or routes the message. | Template, Transform, Filter, Switch |
| **Action** | purple | Does something with the message. | Notify, HTTP Request, Email, GPIO Write |

A node has **ports**: dots on its left edge receive messages, dots on its right
edge send them. A connection goes from an output port to an input port. Things
worth knowing:

- An output can feed **several** nodes. Each one gets its own copy of the
  message, so the branches do not interfere.
- A flow can have **several triggers**.
- Most nodes have one output. **Switch (If/Else)** has two, **True** and
  **False**: connect each to the branch that should run in that case.
- Actions have an output too, so you can chain them: after **HTTP Request**
  the message carries the response.

The [node reference](nodes.md) lists every node, and the **?** button in a
node's settings opens its documentation with examples.

Every node type also has a **risk** level, shown in the editor:

| Risk | Meaning | Examples |
|------|---------|----------|
| **Safe** | Only computes, reads local state or logs. | Template, Transform, Debug |
| **External** | Sends data to, or receives it from, other services. | HTTP Request, MQTT Publish, Email, Notify |
| **Privileged** | Acts on the device itself. | Execute Command, Write File, Database, GPIO Write |

The node list marks External and Privileged nodes, and a node's settings
panel shows its level with what it means. A few settings can raise a node to
Privileged, for example an HTTP Request whose address is taken from the
message, or an option that allows private network addresses; the panel says
so when the node has such a setting. The same levels decide what
[AI agents](agents.md) may deploy on their own.

## Messages

What travels along a connection is a **message**. It has these parts:

| Part | What it holds | Example |
|------|---------------|---------|
| `payload` | The data itself. Text, a number, or an object with fields. | `{"temperature": 22.5, "room": "office"}` |
| `topic` | A short label set by the trigger. | `interval`, `webhook`, `manual` |
| `meta` | Extra information nodes add along the way. | `meta.httpStatus` |
| `id`, `timestamp` | A unique ID and the time the message was created. | |

Each trigger documents the payload it produces. For example Interval Timer
sends `{"count": 1, "timestamp": …}`, and Webhook puts the request's JSON body
in `payload.body`. When you are unsure what a message looks like, connect a
**Debug** node and look at **Activity → Debug**.

Most nodes replace the payload with their result: after a **Template** the
payload is the rendered text, after a **Transform** it is whatever the
expression returned.

## Templates and expressions

There are two ways to use message data in a setting, and each setting accepts
one of them. The setting's help text says which.

### Templates: fill in the blanks

Used in **text** settings: a Template node, a notification message, an e-mail
subject, a URL, a command. Write normal text and put values in double braces:

```
Temperature is {{payload.temperature}} °C in {{payload.room}}
```

| Placeholder | Becomes |
|-------------|---------|
| `{{payload}}` | The whole payload |
| `{{payload.room}}` | One field; use dots to go deeper (`{{payload.body.user.name}}`) |
| `{{topic}}`, `{{id}}`, `{{timestamp}}` | The message's topic, ID and time |
| `{{meta.key}}` | A metadata value |
| `{{vars.name}}`, `{{flowVars.name}}` | A [variable](#variables) |
| `{{secret.NAME}}` | A [secret](#secrets), only in credential settings |

A placeholder that has no value becomes empty text.

### Expressions: calculate or decide

Used in **Transform**, **Filter**, **Switch** and **Set Variable**. An
expression is a small formula. No braces:

```
payload.temperature > 30
```

| You want to… | Write |
|--------------|-------|
| Compare | `payload.temperature > 30`, `payload.status == "open"` |
| Combine conditions | `payload.temperature > 30 and payload.room == "office"` |
| Calculate | `payload.celsius * 9 / 5 + 32` |
| Choose between two values | `payload.level > 0 ? "on" : "off"` |
| Join text | `payload.first + " " + payload.last` |
| Build a new payload | `{"room": payload.room, "hot": payload.temperature > 30}` |
| Use a default when a value is missing | `vars["threshold"] ?? 25` |
| Work with text | `upper(payload.name)`, `contains(payload.email, "@example.com")` |
| Work with lists | `len(payload.items)`, `filter(payload.items, {.price > 100})`, `map(payload.items, {.name})`, `sum(payload.values)` |

In an expression you can use `payload`, `topic`, `meta`, `id`, `timestamp`,
`vars` and `flowVars`. Expressions cannot read files, call the network or see
secrets, and one that runs too long is stopped.

**Filter or Switch?** Both take a condition. Filter lets the message through
when it is true and drops it otherwise. Switch never drops: it sends the
message out of **True** or **False**, for when both cases need handling.

**Test before you deploy.** The settings of every processing node have a
**Payload playground**: paste a sample message, choose **Test node**, and see
the output or the error.

## Saving, deploying and stopping

| Button | What it does |
|--------|--------------|
| **Save** | Stores your changes. Nothing runs yet. |
| **Deploy** | Starts the saved flow. It keeps running after you close the browser and is started again when PiWorker restarts. |
| **Stop** | Stops it and keeps it stopped. |

To change a running flow: **Stop**, edit, **Save**, **Deploy**.

Deploy checks every enabled node first. If a setting is wrong it does not
start anything and tells you where, for example
`nodes[3].config.interval: expected number (milliseconds), got string "5s"`.

**Run manually.** A deployed flow that contains a **Manual Inject** trigger
shows **Run manually**: it sends one message on demand, with the saved payload
or one you type. It is the easiest way to test the rest of a flow without
waiting for the real trigger.

## Seeing what happened

The **Activity** panel at the bottom of the editor has two tabs:

- **Runs**: the latest 50 executions, also for a stopped flow. Open a run to
  see which nodes ran, how long each took and the error of one that failed.
  Message contents are not stored here.
- **Debug**: the messages reaching your **Debug** nodes, live. They exist only
  in your browser while the page is open.

On the canvas, a node that just ran shows how long it took.

## Variables

A variable is a named value that outlives a message: a threshold, the last
reading, a counter. Variables are kept across restarts.

| Scope | Shared with | In expressions | In templates |
|-------|-------------|----------------|--------------|
| **Global** | every flow | `vars["threshold"]` | `{{vars.threshold}}` |
| **This flow** | only this flow | `flowVars["threshold"]` | `{{flowVars.threshold}}` |

- Create and edit them under **Variables** (in the navigation, under
  Settings, for globals; **Flow variables** in the editor for both scopes), or
  from a flow with the **Set Variable** node.
- Read them in expressions and templates as shown above, or with the **Get
  Variable** node, which puts the value in the payload.
- The same name may exist in both scopes. They are separate values: a flow
  variable that is missing does not fall back to the global one.
- Fields that accept references offer them with **Insert reference** or
  **Ctrl+Space**.

Variables are plain data, visible to anyone signed in. Do not put passwords
in them.

## Secrets

Passwords, tokens and webhook URLs that contain a token belong under
**Secrets** (in the navigation, under Settings). You give each one a name; after saving, the
value is never shown again, only replaced.

Use a secret by writing `{{secret.NAME}}` in a credential setting: the
Telegram bot token, the Notify URL, the Database connection string, the
username and password of Email and MQTT, the Webhook token, and HTTP Request
headers (`Authorization: Bearer {{secret.api_token}}`) or URL query
(`?apikey={{secret.api_key}}`).

- Rotating a secret needs no redeploy; the new value is used the next time
  the node runs.
- If a setting names a secret that does not exist, a required credential
  (Telegram bot token, Notify URL, Database connection string, Webhook token)
  stops the deploy. An optional one (Email and MQTT username or password) and
  an HTTP Request URL or header still deploy, but the editor shows a
  **warning** after deploying: the node would get an empty value there, or
  the request would fail. Define the secret and the warning goes away on the
  next deploy.
- The **Secrets** dialog shows where each secret is used (flow, node,
  setting, or broker connection, and whether it is deployed). Check it before
  deleting or renaming a secret; deleting one that is used asks first. Names
  that are used but not defined appear under **Referenced but not defined**,
  with a button to define each one.
- Exporting a flow exports the name, never the value.
- Message data cannot read secrets: they are only resolved where you wrote
  them in the settings.
- Secrets are stored in PiWorker's database without extra encryption (on a
  Pi the key would have to sit on the same card). PiWorker keeps the
  database file private to its own user; protect the device and its backups
  (full-disk encryption if it could be stolen). The reasons are in
  [the review](../design/secrets-at-rest.md).

More detail, including the exact HTTP Request rules, is in the
[README](../../README.md#user-variables-and-secrets).

## Broker connections

MQTT and Zigbee nodes reach an MQTT broker. Instead of typing the broker's
address and password into every node, define it once under **Broker
connections** (in the navigation, under Settings) and choose it in the
node's **Connection** setting:

- **MQTT Subscribe** and **MQTT Publish** take either a **Connection** or a
  **Broker** address with their own username and password, not both.
  Zigbee nodes always use a connection.
- Saving a change to a connection **restarts the running flows that use
  it**, so the new address or password applies at once. The message after
  saving lists them, and any that could not start again (they show as
  failed; open them to see why).
- A connection that a running flow uses cannot be deleted; stop those flows
  first. A stopped flow that names a missing connection is refused when you
  deploy it.
- A connection's username and password may be `{{secret.NAME}}`
  references; the [Secrets](#secrets) dialog lists the connections that use
  each secret.

## Versions and history

Every **Save** creates a numbered version of the flow. **History** (in the
editor and on each flow card) lists them with who saved each one and which is
deployed. If two browser tabs edit the same flow, the second save is refused
and offers to reload, so nothing is silently overwritten.

History matters most when [AI agents](agents.md) may change your flows:
**Roll back** restores the version you last approved.

## Copying flows around

On a flow card, the **⋯** menu can **duplicate** a flow or **export** it as a file, and
**Import** in the flow list loads such a file. Variables and secret values are
not part of an export: create them again on the other installation.
