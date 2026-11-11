# AI agents

PiWorker can be used by AI assistants such as Claude Code and Claude Desktop
(any client that speaks the [Model Context Protocol](https://modelcontextprotocol.io),
MCP). This page takes you from nothing to an assistant calling one of your
flows, then explains the optional, more powerful modes.

The idea is simple: **an agent can only use the flows you publish as tools.**
It sends the data a tool asks for; the flow decides what happens. Agents get
no access to commands, files, pins or your secrets.

- [What an agent can do](#what-an-agent-can-do)
- [Step 1: turn MCP on](#step-1-turn-mcp-on)
- [Step 2: publish a flow as a tool](#step-2-publish-a-flow-as-a-tool)
- [Step 3: create a token](#step-3-create-a-token)
- [Step 4: connect your assistant](#step-4-connect-your-assistant)
- [Tools that need your approval](#tools-that-need-your-approval)
- [Letting agents look, draft and deploy](#letting-agents-look-draft-and-deploy)
- [Staying safe](#staying-safe)

## What an agent can do

You decide per token, by ticking **scopes** when you create it:

| Scope | The agent can… | Off by default? |
|-------|----------------|-----------------|
| `call` | Call the tools you granted to that token. | Needs MCP on |
| `read` | See your flows, their runs and the system status. Never secrets or variables. | Needs MCP on |
| `author` | Check and test flows, and write **drafts** that you approve or reject. | Needs `author_enabled` |
| `deploy` | Deploy and stop flows itself. | Needs the **Agent deploy** level above Off |

Start with `call` only. It is all you need for "Claude, convert this" or
"Claude, open the garage".

## Step 1: turn MCP on

MCP is off until you enable it. Edit PiWorker's configuration file
(`/etc/piworker/piworker.toml` for a `piworkerctl` installation) and add:

```toml
[mcp]
enabled = true
```

Restart PiWorker (`sudo systemctl restart piworker`). With Docker, set the
environment variable `PIWORKER_MCP_ENABLED=true` instead. The **Agents** page
of the editor shows whether MCP is on.

MCP needs sign-in to be enabled (the default) and is served over HTTPS at
`https://<device>:8443/mcp`.

## Step 2: publish a flow as a tool

A tool is a flow that starts with an **Agent Tool** node and ends with an
**Agent Reply** node. This example converts a temperature; it touches nothing
outside PiWorker, so it is a safe first tool.

| # | Node | Settings |
|---|------|----------|
| 1 | **Agent Tool** | Tool name `convert_temperature` · Description `Converts a temperature from Celsius to Fahrenheit. Use it when the user asks for Fahrenheit.` · Input schema below |
| 2 | **Transform** | `{"fahrenheit": payload.celsius * 9 / 5 + 32}` |
| 3 | **Agent Reply** | leave as it is |

Input schema:

```json
{
  "type": "object",
  "required": ["celsius"],
  "properties": {
    "celsius": {
      "type": "number",
      "minimum": -90,
      "maximum": 60,
      "description": "Temperature in °C"
    }
  }
}
```

- The **description** is what the assistant reads to decide when to use the
  tool. Say what it does and when to use it.
- The **input schema** lists the arguments and their limits. PiWorker checks
  every call against it before the flow starts, and the arguments arrive as
  the payload (`payload.celsius`).
- The payload reaching **Agent Reply** is the answer the assistant receives.

**Save** and **Deploy**: the tool exists while the flow runs. Open the Agent
Tool node again and use its **Test tool** section to call it yourself, the way
an agent would.

Real tools follow the same shape with real actions in the middle: Agent Tool →
GPIO Write → Agent Reply to open a door, Agent Tool → HTTP Request → Agent
Reply to query a device. PiWorker rates each tool `safe`, `external` or
`privileged` from the nodes it reaches and shows the rating when you grant it.

## Step 3: create a token

Each assistant gets its own token, which works like a password.

1. Open **Agents** and choose **New token**.
2. Name it after the assistant (`claude-laptop`), tick the `call` scope and
   grant it the `convert_temperature` tool.
3. Confirm with your password. The token (`pwk_…`) is shown **once**, together
   with ready-to-copy commands for the next step.

Tokens expire after 90 days by default and only work for MCP, never for
signing in to the editor.

## Step 4: connect your assistant

The page that shows your new token gives you the exact commands for your
installation. In short:

**Claude Code** (on a computer on the same network):

```bash
curl -fo ~/piworker-ca.crt http://raspberrypi.local:8080/piworker-ca.crt
openssl x509 -in ~/piworker-ca.crt -noout -fingerprint -sha256   # must match the fingerprint PiWorker shows
claude mcp add --transport http --scope user piworker https://raspberrypi.local:8443/mcp \
  --header "Authorization: Bearer pwk_…"
NODE_EXTRA_CA_CERTS=~/piworker-ca.crt claude
```

Then ask: *"Use piworker to convert 20 °C to Fahrenheit."*

**Claude Desktop** connects through a small helper program, `piworker-mcp`,
which you download from the same release as PiWorker. See
[its instructions](../../cmd/piworker-mcp/README.md).

Details and the reasons behind each command are in the README:
[Connecting Claude Code](../../README.md#connecting-claude-code),
[Connecting Claude Desktop](../../README.md#connecting-claude-desktop).

## Tools that need your approval

For anything you would not want to happen without you (a door, a heater, a
command), set the Agent Tool's **Confirmation** to `admin`. A call then does
not run: it waits under **Agents → Approvals**, showing the tool, the
arguments and which token asked. You approve or reject it; nothing runs
until you do, and requests expire after 15 minutes.

`client` confirmation only asks the assistant's own app to check with its
user. It is a convenience, not a safeguard.

Everything agents do is listed under **Agents → Audit** for 30 days.

## Letting agents look, draft and deploy

These are optional and off by default.

**Read.** A token with `read` lets the assistant see your flows and why a run
failed, which makes it good at diagnosing problems. Secret values and
variables stay hidden.

**Author (drafts).** Add `author_enabled = true` to the `[mcp]` section and
give a token the `author` scope. The assistant can then propose a new flow or
a change to an existing one. It never edits a flow directly: its proposal
appears under **Agents → Drafts**, where you see exactly what would change,
the risks PiWorker noticed, and **Approve** or **Reject**. Agents can try
their draft in a sandbox first, where nothing real is sent or switched.

**Deploy.** The **Agent deploy** level on the **Agents** page lets tokens with
the `deploy` scope put changes live without your review:

| Level | Meaning |
|-------|---------|
| **Off** (default) | Agents only draft. You approve and deploy everything. |
| **Low-risk flows** | Agents may deploy flows that do not touch the device itself and do not send data anywhere new. |
| **All flows** | Agents may deploy anything. A leaked token could take over the device. |

Read [Agent deploy](../../README.md#agent-deploy) before raising it: even at
*Low-risk flows* an agent can change what your approved flows send to the
destinations you approved.

Flows an agent changed or deployed are marked **Changed by agent** and
**Deployed by agent**, and **History → Roll back** returns a flow to the
version you last approved.

## Staying safe

- Give each assistant its own token, with only the tools it needs.
- Use `admin` confirmation for tools with real-world effects.
- Keep **Agent deploy** off unless you have a reason.
- Use long, generated secrets (16 characters or more).
- If an assistant does something unexpected: **Agents → Revoke all**, then
  follow [Agents: when one misbehaves](../operations.md#agents-when-one-misbehaves).

The full rules are in the README's
[AI agents](../../README.md#ai-agents-mcp) section and its
[security notes](../../README.md#security-notes).
