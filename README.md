# PiWorker

[![GitHub license](https://img.shields.io/github/license/Pegasus8/piworker)](https://github.com/Pegasus8/piworker/blob/master/LICENSE.md)

A **visual flow-based automation system** for Raspberry Pi and other devices. Create automation workflows by connecting nodes in a visual editor, similar to Node-RED but built with Go for performance and Vue 3 for a modern UI.

## Features

- **Visual Flow Editor** - Drag-and-drop interface to create automation workflows
- **Node-Based** - Connect triggers, processing nodes, and actions
- **Local execution** - Flows and storage run on your device; external-service nodes send data to the services you configure
- **Extensible** - Easy to add new node types
- **Lightweight** - Single binary with embedded frontend, optimized for Raspberry Pi
- **HTTPS by default** - Local certificate authority generated on first start, with automatic certificate renewal
- **AI agents (MCP)** - Publish flows as typed tools that Claude Code, Claude Desktop and other MCP clients can call, with scoped tokens, approvals and an audit log; agents can draft flows for you to review, and deploy only if you opt in
- **Version history** - Every save of a flow is a numbered version, with one-click rollback to the version you last approved
- **Fast** - Go backend with concurrent message processing

## Documentation

New to PiWorker? The **[user guide](docs/guide/README.md)** takes you from
installation to a running flow without reading this whole page:

| Guide | What it covers |
|-------|----------------|
| [Getting started](docs/guide/getting-started.md) | Install, open the editor, create your account and build your first flow |
| [Concepts](docs/guide/concepts.md) | Messages, templates and expressions, deploy and stop, variables, secrets, versions |
| [Recipes](docs/guide/recipes.md) | Complete flows to copy: notifications, webhooks, schedules, sensors, GPIO |
| [Node reference](docs/guide/nodes.md) | Every node with its ports and settings |
| [AI agents](docs/guide/agents.md) | Publish a flow as a tool and connect Claude, step by step |
| [Troubleshooting](docs/guide/troubleshooting.md) | Symptoms and fixes |

This README is the reference: installation options, configuration, security
model, API and development. Decisions are recorded in [docs/adr](docs/adr/README.md).

![The PiWorker editor with a running flow](docs/guide/images/editor-running.png)

## Quick Start

For maintenance and recovery, see the [operations guide](docs/operations.md).
For automated checks on a Pi, see the [Pi validation script](docs/raspberry-pi-validation.md#automated-checks-recommended-first).
See [release notes](CHANGELOG.md) for compatibility limits and the
[release checklist](docs/release-checklist.md) for publication readiness.

### Install, update and uninstall (Linux / Raspberry Pi)

The native installer sets up PiWorker as an automatically started systemd service.
It downloads a pre-built binary and verifies its SHA256 checksum; Go, Bun and
Docker are not needed. Supported systems: Linux with systemd and glibc 2.35+
(for example Raspberry Pi OS Bookworm), on ARM64, ARMv7 or x86_64.
Install `curl` and CA certificates first if your OS does not include them.

**Release availability:** this installer is new on the revival branch. The
following download command becomes available when a release containing
`piworkerctl` is published. Older releases are not compatible with this installer.

```bash
# Install the latest stable release (run in a directory you own)
curl -fL https://github.com/Pegasus8/piworker/releases/latest/download/piworkerctl -o piworkerctl && sudo bash ./piworkerctl install

# Later: update, including a backup of the stopped database
sudo piworkerctl update

# Remove the service and executables, keeping your data and configuration
sudo piworkerctl uninstall
```

The installer prints the HTTPS address (for example `https://raspberrypi.local:8443`)
and the fingerprint of PiWorker's local certificate authority. Trust that CA once
on each device you use (see [HTTPS](#https)); `http://<device-address>:8080`
redirects to HTTPS. Get the first administrator setup code with
`sudo journalctl -u piworker -n 50 --no-pager`, then complete setup in the browser.
The service also starts after a reboot. Re-running `install` updates a managed
installation; reinstalling after `uninstall` reuses the retained data.

You can select a release explicitly with `sudo piworkerctl update vX.Y.Z`
(replace `vX.Y.Z` with an actual compatible release tag). **Prereleases** are
never "latest", so they are always installed by tag:

```bash
curl -fL https://github.com/Pegasus8/piworker/releases/download/vX.Y.Z-beta.1/piworkerctl -o piworkerctl && sudo bash ./piworkerctl install vX.Y.Z-beta.1
```

With Docker, use the image tag of the prerelease instead of `latest`. Updates also replace
the manager with the version included in the verified release archive.
Before the first compatible release, contributors can run the lifecycle tests
with `make test-installer`; running the installer still requires published assets.

| Item | Location / command |
|------|--------------------|
| Configuration | `/etc/piworker/piworker.toml` |
| Accounts, flows, secrets and variables | `/var/lib/piworker/piworker.db` |
| Local CA and server certificate | `/var/lib/piworker/tls/` |
| Update backups (private, include the CA key; retained until you remove them) | `/var/backups/piworker/` |
| Logs / status | `sudo journalctl -u piworker` / `systemctl status piworker` |
| Restart after changing configuration | `sudo systemctl restart piworker` |

Updates download and validate before stopping the service. They back up its
entire data directory and configuration, replace the executable, and check the
health endpoint (over HTTPS, verified against the local CA). A failed update restores the previous binary and data;
updates briefly interrupt running flows. Backups cannot undo actions already
sent to external devices or services. Ensure there is enough disk space for a
full data copy. If automatic recovery cannot stop/start the service, the command
reports the backup location for manual recovery.

The managed service runs as the dedicated `piworker` user and always uses the
database path above. Grant that user the device-group permissions needed for
GPIO/I2C hardware. The health check follows `addr` and `[tls] mode` in the
configuration; to override it, provide a local health URL when installing or
updating, for example
`sudo PIWORKER_HEALTH_URL=http://127.0.0.1:9090/api/health piworkerctl update`.
Manual installations are not overwritten or migrated automatically.
Uninstall retains the service user as well as data/configuration/backups; it
does not delete Docker installations. To erase retained data permanently,
review and remove the three directories listed above after uninstalling.

### First boot: create the administrator

Start PiWorker and open its address in your browser. On a fresh database,
PiWorker prints a **one-use setup code** in its startup terminal (or container
logs). Enter that code, choose your username and password, and the editor opens.
The code expires after 30 minutes; restart PiWorker or run `-setup-code` against
the same database to replace it. Once an account exists, setup is closed.

For unattended installations, you can optionally bootstrap the first account:

```bash
export PIWORKER_ADMIN_USER=admin
export PIWORKER_ADMIN_PASS='replace-with-a-unique-password'
```

These variables never overwrite an existing account. Docker Compose reads them
from the shell or a local, git-ignored `.env` file. Shell exports do not
configure the native systemd service: use its TOML file or an explicitly
configured systemd environment. Browser setup needs no bootstrap credentials.

**Coming from the 2020 release (`v0.1.0-alpha.1`)?** Migration is not
supported: the revival is a new program with a different data model, and it
does not read the old tasks or database. Install it separately and recreate
your automations as flows.

This README describes the revival branch, including changes not yet released.
Build from source or use `make docker-build` to try this implementation. Published
binaries/images may still contain the previous implementation; check their release notes.

### Option 1: Docker (published releases)

```bash
# Production - single container with everything included
docker run -d \
  --name piworker \
  --restart unless-stopped \
  -p 8443:8443 -p 8080:8080 \
  -v piworker-data:/app/data \
  -e PIWORKER_TLS_HOSTS=raspberrypi.local,192.168.1.50 \
  -e PIWORKER_ADMIN_USER -e PIWORKER_ADMIN_PASS \
  -e PIWORKER_ALLOWED_HOSTS="$(hostname),$(hostname).local" \
  ghcr.io/pegasus8/piworker:latest

# Open https://localhost:8443 (http://localhost:8080 redirects there)
```

A container cannot see the host's name or LAN address, so list the names and
addresses browsers use in `PIWORKER_TLS_HOSTS`. Publish both ports unchanged.
The CA and certificate live in the data volume (`tls/`); the CA fingerprint is in
`docker logs piworker`.

### Option 2: Pre-built Binary (published releases)

Download the archive for your CPU and `SHA256SUMS` from the same compatible
[release](https://github.com/Pegasus8/piworker/releases). For example, on Linux ARM64:

```bash
# Use amd64 or arm instead for the other supported CPUs.
awk '$2 == "./piworker-linux-arm64.tar.gz" || $2 == "piworker-linux-arm64.tar.gz"' SHA256SUMS | sha256sum -c - &&
tar -xzf piworker-linux-arm64.tar.gz &&
./piworker-linux-arm64
# Open https://localhost:8443 (the terminal shows the CA fingerprint)
```

Run from a dedicated directory: manual installations store `piworker.db` in the
working directory by default. These archive names apply to the new revival releases;
the legacy 2020 release uses its own packaging.

### Option 3: Build from Source

```bash
# Clone the repository
git clone --branch revival https://github.com/Pegasus8/piworker.git
cd piworker

# Install locked frontend dependencies and build with embedded frontend
make frontend-install build-release

# Run
./build/piworker
```

---

## Development Setup

### Prerequisites

| Tool | Version | Purpose | Installation |
|------|---------|---------|--------------|
| **Go** | 1.27.3+ | Backend compilation | [go.dev/dl](https://go.dev/dl/) |
| **Bun** | 1.3.14 | Frontend runtime & package manager | `curl -fsSL https://bun.sh/install \| bash` |
| **Make** | any | Build automation | Pre-installed on macOS/Linux |
| **Docker** | BuildKit + Compose v2 | Optional, containerized dev | [docker.com](https://docker.com) |
| **golangci-lint** | latest | Optional, for `make lint` | `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` |

#### Quick Install (macOS)

```bash
# Install Go
brew install go

# Install Bun
curl -fsSL https://bun.sh/install | bash

# Verify installations
go version    # go1.27.3+
bun --version # 1.3.14
```

A C compiler is required: SQLite uses CGO. Install Xcode Command Line Tools
on macOS or `build-essential` on Debian/Ubuntu. Bun is needed only to build the
frontend; release binaries include it already.

#### Quick Install (Linux/Raspberry Pi)

```bash
# Install Go
wget https://go.dev/dl/go1.27.3.linux-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.27.3.linux-arm64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Install Bun
curl -fsSL https://bun.sh/install | bash

# Verify
go version && bun --version
```

### Local Development (Recommended)

Run backend and frontend separately. Vite reloads frontend changes automatically;
restart the Go command after backend changes:

**Terminal 1 - Backend:**
```bash
go run . -tls=off -addr 127.0.0.1:8080 -debug -auth=false
# API running at http://localhost:8080 (plain HTTP for the Vite proxy)
```

**Terminal 2 - Frontend:**
```bash
cd web
bun install --frozen-lockfile
bun run dev
# UI running at http://localhost:3000
```

Open http://localhost:3000 - the frontend proxies API calls to the backend,
keeping the browser's Host, so the API treats the editor as same-origin and
needs no `cors_origins` entry.

### Docker Development

Use Docker Compose for a fully containerized dev environment:

```bash
# Start both services with hot-reload
make docker-dev

# View logs
make docker-dev-logs

# Stop
make docker-dev-down
```

- **Backend**: http://localhost:8080 (Go with air hot-reload; TLS off for the Vite proxy)
- **Frontend**: http://localhost:3000 (Vite HMR)

---

## Production Deployment

### Docker (Recommended)

```bash
# Build production image
make docker-build

# Run with Docker Compose
make docker-prod

# Or run directly
docker run -d \
  --name piworker \
  -p 8443:8443 -p 8080:8080 \
  -v piworker-data:/app/data \
  -e PIWORKER_TLS_HOSTS=raspberrypi.local \
  -e PIWORKER_ADMIN_USER -e PIWORKER_ADMIN_PASS \
  -e PIWORKER_ALLOWED_HOSTS="$(hostname),$(hostname).local" \
  --restart unless-stopped \
  piworker:latest
```

### Raspberry Pi

For cross-compilation on Debian/Ubuntu, install the C toolchains first:

```bash
sudo apt-get install gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf
make frontend-install

# Build for ARM
make build-arm64  # For RPi 4, 64-bit
make build-arm    # For RPi 3 or 32-bit

# Copy to Pi and connect
scp build/piworker-linux-arm64 pi@raspberrypi:~/piworker
# On the Pi, run PiWorker and use the setup code from its terminal.
ssh pi@raspberrypi
```

These targets embed the frontend and explicitly enable CGO. `build-arm` targets
ARMv7 (`GOARM=7`). Override `LINUX_CC`, `ARM_CC`, or `ARM64_CC` to select a
cross-compiler; when building on the matching Linux device, use e.g.
`make build-arm64 ARM64_CC=cc`. Cross-compiling from macOS requires a Linux C
cross-toolchain too; alternatively use the multi-platform Docker build:

```bash
docker buildx build --platform linux/arm64 --load -t piworker:latest .
```

Use Linux with glibc 2.35+ for CI-built binaries, or the Docker image (which
includes its runtime libraries). Real GPIO, I2C and 1-Wire nodes additionally
require the corresponding buses/drivers and device permissions on the Pi.

### Systemd Service (manual alternative)

The native installer above creates and manages its own service. The following
example is only for manually installed binaries.

Start the service and read its setup code with `journalctl -u piworker`.
Existing accounts remain available without bootstrap credentials.

Create `/etc/systemd/system/piworker.service`:

```ini
[Unit]
Description=PiWorker Automation System
After=network.target

[Service]
Type=simple
User=pi
WorkingDirectory=/home/pi
ExecStart=/home/pi/piworker -db /home/pi/piworker.db
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable piworker
sudo systemctl start piworker
```

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Visual Flow Editor                    │
│                 (Vue 3 + Vue Flow + Tailwind)            │
└─────────────────────────────────────────────────────────┘
                              │
                         REST API
                              │
┌─────────────────────────────────────────────────────────┐
│                      Flow Engine                         │
│          (Goroutines + Channels for message passing)     │
└─────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
    ┌──────────┐       ┌──────────┐       ┌──────────┐
    │ Triggers │       │Processing│       │ Actions  │
    │  Nodes   │       │  Nodes   │       │  Nodes   │
    └──────────┘       └──────────┘       └──────────┘
```

### How It Works

1. **Triggers** start a flow (interval timer, cron schedule, HTTP webhook)
2. **Messages** flow through connected nodes via Go channels
3. **Processing nodes** transform or route messages
4. **Action nodes** perform side effects (log, execute commands, HTTP requests)

---

## Node Types

### Triggers (Input)
| Node | Type | Description |
|------|------|-------------|
| **Interval Timer** | `trigger-interval` | Fire at regular time intervals |
| **Cron Schedule** | `trigger-cron` | Fire on a cron schedule (e.g., `0 9 * * *`) |
| **Manual Inject** | `trigger-manual` | Fire on demand from the UI or API |
| **Webhook** | `trigger-webhook` | Fire on an incoming HTTP request to `/webhooks/<path>` |
| **RSS Feed** | `trigger-rss` | Poll a feed for new entries |
| **MQTT Subscribe** | `trigger-mqtt` | Receive messages from an MQTT broker |
| **Zigbee Device** | `trigger-zigbee` | React to a Zigbee device's change or availability ([Zigbee guide](docs/guide/zigbee.md)) |
| **Sun** | `trigger-sun` | Fire at sunrise, sunset, dawn or dusk (computed locally) |
| **GPIO Edge** | `trigger-gpio` | React to Raspberry Pi pin changes (experimental) |
| **DS18B20 Temperature** | `trigger-ds18b20` | Poll a 1-Wire temperature sensor (experimental) |
| **System Metric** | `trigger-sysmetric` | Fire when CPU / memory / temperature crosses a threshold |
| **Agent Tool** | `trigger-tool` | Publish the flow as a typed tool that AI agents call over MCP |

### Processing
| Node | Type | Description |
|------|------|-------------|
| **Delay** | `process-delay` | Delay messages by a specified duration |
| **Timer** | `process-timer` | Fire once no message came for a while (retriggerable) |
| **Zigbee Get** | `process-zigbee-get` | Read a Zigbee device's current value |
| **Debug** | `process-debug` | Inspect messages flowing through the flow |
| **Transform** | `process-transform` | Reshape the payload with a sandboxed expression |
| **Filter** | `process-filter` | Pass a message through only when a condition is true |
| **Switch (If/Else)** | `process-switch` | Route a message to a `true` / `false` output |
| **Template** | `process-template` | Render a text template from the message |
| **Parse** | `process-parse` | Parse JSON or CSV into structured data |
| **Aggregate** | `process-aggregate` | Batch a configured number of messages |
| **Rate Limit** | `process-ratelimit` | Throttle messages with a sliding window |
| **Join** | `process-join` | Merge messages by key or source |
| **Set Variable** | `set-var` | Store a value into a named variable |
| **Get Variable** | `get-var` | Read a named variable into the payload |

### Actions (Output)
| Node | Type | Description |
|------|------|-------------|
| **Log** | `action-log` | Log messages with a configurable level |
| **Execute Command** | `action-command` | Execute shell commands (inputs are passed as literal arguments; values starting with `-` need `allowDashArgs`) |
| **HTTP Request** | `action-http` | Call an HTTP API (SSRF-guarded, size-capped) |
| **Notify** | `action-notify` | Send to Discord / Slack / ntfy |
| **Telegram** | `action-telegram` | Send a message via a Telegram bot |
| **Email** | `action-email` | Send an email via SMTP |
| **Database** | `action-database` | Execute SQLite, PostgreSQL or MySQL queries |
| **MQTT Publish** | `action-mqtt` | Publish to an MQTT broker |
| **Zigbee Set** | `action-zigbee` | Turn a Zigbee device on/off or set one of its properties |
| **GPIO Write** | `action-gpio` | Set a Raspberry Pi output pin (experimental) |
| **I2C Read** | `action-i2c` | Read from an I2C device (experimental) |
| **Write File** | `action-file` | Write or append the message to a file |
| **Agent Reply** | `action-reply` | Answer the agent tool call that started the run |

> **Experimental hardware nodes.** GPIO Edge, GPIO Write, I2C Read and DS18B20
> Temperature are covered by automated tests but have not been validated on
> real hardware for this release. Test them with your own wiring before
> relying on them, and please report what you find.

> Each node ships an in-app documentation card and a configuration schema, so the
> editor renders its settings form automatically. The settings of every node are
> listed in the [node reference](docs/guide/nodes.md), which is generated from
> the same schemas.

Each node type also declares a **risk**: `privileged` for nodes that act on
the host (Execute Command, Write File, Database, GPIO Write, I2C Read),
`external` for nodes that send data elsewhere (HTTP Request, MQTT Publish,
Zigbee Set, Zigbee Get, Email, Telegram, Notify) and `safe` for the rest. The
risk is listed in the node reference, reported to agents by
`pw_list_node_types`, returned by `/api/node-types` (`risk`) and shown in
the editor: the node list marks external and privileged types, and the
settings panel shows the level with its meaning (and, when a setting such as
a templated URL or a RiskSwitch can raise it, says so). Schema properties may be marked
`sensitive` (credentials, never echoed) and `agentVisible` (harmless values
that agents may read); numeric properties state their `unit`.

**Execute Command** refuses a placeholder value that starts with `-` (for example
`-rf` or `--output=/etc/passwd` arriving in a payload) when the placeholder
begins its argument, because the program would read it as an option. Behind
literal text in the same argument (`"temp={{payload}}"`, `--value={{payload}}`,
`/tmp/{{payload}}`) such values, negative numbers included, are allowed. If a
command really expects them as whole arguments, end its options with `--`
before the placeholders and turn on **allowDashArgs**, a deliberate opt-in to
that risk.

### Agent tools

A flow that starts with an **Agent Tool** (`trigger-tool`) and answers with an
**Agent Reply** (`action-reply`) is a *tool* that AI agents call through
PiWorker's MCP endpoint ([design](docs/design/mcp.md), ADR-0009, ADR-0013).
Agents supply data only, never configuration:

- **Arguments** are described by the node's input schema, a closed subset of
  JSON Schema checked at deploy: an object root, closed objects (at most three
  levels), one type per property (`string`, `integer`, `number`, `boolean`,
  arrays of those with `maxItems`, objects), anchored patterns (`^…$`),
  bounded strings (256 characters unless `maxLength` says otherwise). `$ref`,
  `anyOf`/`oneOf`/`allOf`, conditionals and unknown keywords are rejected with
  the JSON pointer of the problem. Every call is validated before the flow
  starts, and the run's payload is exactly the validated arguments.
- **Replies**: the first Agent Reply of a run becomes the call's result (or its
  error in `error` mode). The call waits up to `timeoutMs` (default 10 s, at
  most 30 s); a later reply is fetched afterwards.
- **Names** are unique across running flows (`pw_` is reserved): deploying a
  second flow with a taken tool name answers `409`; a tool without a
  connection or with an invalid schema answers `400`.
- **Confirmation**: `none`, `client` (the MCP client asks its user; not a
  security control) or `admin` (each call waits for approval in the editor).
  `maxConcurrent` (when unset: 1, or 4 for read-only tools) and `cooldownMs`
  limit how often a tool runs.

Arguments that reach command, file, database, GPIO or I2C nodes must be strict:
strings need an `enum` or a pattern that accepts no value starting with `-`,
with a `..` path segment, or containing `/`, whitespace, a line break, `$`, `;`,
`|` or a backtick anywhere (each can be acknowledged per property with
`x-piworker-allow`), and numbers need `minimum` and `maximum`. A tool that
sets a flow variable also reaches every branch of its flow that reads flow
variables, so those branches count towards its risk. Patterns use the
syntax Go and JavaScript share (no `(?i)`, `\z`, `[[:alpha:]]`…), since MCP
clients read them as JavaScript. The editor edits schemas as JSON and shows each
node's documentation.

---

## AI agents (MCP)

PiWorker can be used by AI agents (Claude Code, Claude Desktop and other
[Model Context Protocol](https://modelcontextprotocol.io) clients) through its
`/mcp` endpoint. Agents do not get a tool per node type or raw access to
commands, GPIO or files: **the flows you publish are the tools**
([ADR-0009](docs/adr/0009-mcp-flows-as-tools.md),
[design](docs/design/mcp.md)). An agent can only send the data a tool's input
schema allows; the flow decides what happens with it.

### Enabling it

MCP is off by default. Set `enabled = true` in the `[mcp]` section of
`piworker.toml` (or `PIWORKER_MCP_ENABLED=true`) and restart PiWorker; it needs
`auth = true`. The endpoint is `https://<device>:8443/mcp` (Streamable HTTP,
stateless; protocol versions 2025-06-18, 2025-11-25 and 2026-07-28). With
`[tls] mode = "off"` it only answers clients on the same machine unless
`lan_access = true` (tokens would then cross the network unencrypted). The
**Agents** pages of the editor show whether MCP is on. Agents can draft
flows only with `author_enabled = true` in the same section (see
[Authoring](#authoring-drafts-you-approve)).

### Publishing a flow as a tool

1. Start the flow with an **Agent Tool** node: a tool name (e.g. `garage_open`),
   a description the agent reads, and an input schema (see [Agent tools](#agent-tools)).
2. Connect the nodes that do the work, using the arguments from the payload.
3. End each branch that answers with an **Agent Reply** node (`result` or `error`).
4. Deploy the flow: the tool is published while the flow runs.
5. Under **Agents → New token**, grant the tool to a token and copy the client set-up.

The editor's **Test tool** section of an Agent Tool node calls it the way an
agent would. PiWorker rates each tool `safe`, `external` or `privileged` from
the nodes it can reach (commands, files, databases, GPIO and I2C are
privileged), and tells agents whether a tool is read-only, destructive or
reaches the outside world.

### Tokens and scopes

Each agent gets its own token (`pwk_…`), created in the editor with your
password and shown once. Tokens work only on `/mcp`, never on the editor's
API, and expire after 90 days by default.

- **Scopes**: `call` (call granted tools), `read` (see flows, runs and system
  status; secrets and variables stay hidden), `author` (check, test and draft
  flows for you to review; only with `author_enabled = true`, see
  [Authoring](#authoring-drafts-you-approve)) and `deploy` (deploy and stop
  flows; only while the **Agent deploy** level is not off, see
  [Agent deploy](#agent-deploy)).
- **Grants** name individual tools, or **all tools** for every published tool
  that is not privileged. Calling a privileged tool also needs the token's
  **privileged** flag.
- A grant is bound to the tool's *fingerprint*: its risk, annotations, input
  schema and confirmation mode, and which nodes above `safe` (commands, files,
  HTTP, MQTT, e-mail…) it reaches, by ID and type, not their settings.
  Redeploying a tool with any such change (repointing it to another action
  or adding one counts) suspends its grants until you re-confirm them, and
  its requests waiting for approval run nothing
  ([ADR-0015](docs/adr/0015-tool-identity-binds-grants-and-approvals.md),
  [ADR-0022](docs/adr/0022-low-keeps-tools-and-protected-data-the-admins.md)).
- Revoking a token, or all of them at once, needs no password and takes effect
  immediately. Account recovery and password changes revoke every token
  (a password change can keep them on request).

### Approvals and audit

A tool whose confirmation mode is `admin` does not run when called: the agent
gets `approval_required` and the request waits up to 15 minutes under
**Agents → Approvals**, with the tool, its recorded risk, the arguments (shown
as plain text, hidden characters escaped) and the token. Approving runs
exactly the tool version the agent asked for (a tool changed since runs
nothing) and keeps the result, which the agent fetches with `pw_get_result`.
Every agent call is recorded under **Agents → Audit** for 30 days; calls
refused as busy, cooling down or with invalid arguments are counted per
token.

### Built-in tools and prompts

Besides the flows you grant, a token sees the built-in `pw_*` tools and the
prompts of its scopes, and only while they are on (the author ones with
`author_enabled = true`, the deploy ones while **Agent deploy** is not off):

| Scope | Tools | Prompt |
|---|---|---|
| `call` | `pw_get_result` (the result of a call that answered `running` or `approval_required`) | |
| `read` | `pw_list_flows`, `pw_get_flow`, `pw_list_runs`, `pw_get_run`, `pw_system_status` | `diagnose_flow` |
| `author` | `pw_list_node_types`, `pw_validate_flow`, `pw_save_draft`, `pw_get_draft`, `pw_dry_run`, `pw_test_node` | `author_flow` |
| `deploy` | `pw_deploy_flow`, `pw_stop_flow` | |

Argument and result shapes are in the [design](docs/design/mcp.md) (§8, §10).

### Authoring: drafts you approve

Agents never write a flow directly: with the `author` scope they save
**drafts** (a whole flow, or operations such as adding a node or connecting
two, against the `version` they read; a draft of a version saved since is
refused as a conflict) that you review under **Agents → Drafts**, or that a
token with the `deploy` scope deploys itself within the
[Agent deploy](#agent-deploy) level. The `author` scope alone works on new
flows; changing or testing an existing flow also needs `read`. A token has
at most 20 drafts waiting.

- **What agents may change**: other nodes (add, configure, enable or
  disable, move, remove), connections, and the flow's name and description.
- **What agents may not change**: Agent Tool and Agent Reply nodes (they
  define the tools agents call; moving them is allowed), a webhook trigger's
  path or token (nor add or turn on a webhook trigger), a setting that widens
  what a node may do (`allowPrivate`, `allowDashArgs`) turned on, and the
  legacy flow variables. Such a draft is refused, naming the place.
- **Credentials**: agents never see secret values, credentials typed into
  settings, or settings marked hidden from agents; these read
  `"<redacted>"`, and sending `"<redacted>"` back keeps your value.
  Credential settings an agent writes accept only `{{secret.NAME}}`
  references. A draft with errors a deploy would refuse (except a secret
  you have not defined yet) is not stored.
- **Checking before drafting**: `pw_validate_flow` reports every problem a
  deploy would find, from the same checks. `pw_dry_run` runs a flow once in a
  sandbox: triggers do not start (the agent supplies the payload), every node
  that is not `safe` (HTTP, MQTT, e-mail, notifications, commands, files,
  databases, GPIO, I2C) is a stub that records what it would have done and
  returns the agent's mock, if any, variables are empty unless the agent
  supplies flow variables, and there are no secrets (at most 50 nodes,
  10 s). `pw_test_node` runs one safe processing node (transform, filter,
  switch…) on an input (5 s). One dry-run or node test runs at a time on
  the device.

The **Drafts** tab (with a badge for waiting drafts) lists drafts waiting,
decided or all. A draft's page shows the agent's note, the risk before and
after, the **alerts** (a new destination, host or secret reference, a
credential used for a different request, a changed rate, a risk switch
turned on, a change to what a published tool runs or to a node reading
variables…), the problems found, the changes node by node (credentials
hidden, destinations highlighted), and, when agents changed the flow since
you last approved it, a comparison with that version. **Approve** saves the
draft as a new version (optionally deploying it; a running flow is always
redeployed), which becomes the version you last approved; **Reject** takes
an optional reason the agent can read.

### Agent deploy

The **Agent deploy** level on the **Agents** page (changing it needs your
password) decides whether tokens with the `deploy` scope may deploy and
stop flows. The scope can only be granted while the level is not off.

- **Off** (default): agents only draft; you review and deploy every change.
- **Low-risk flows**: agents may deploy, start and stop flows, and deploy
  their own drafts without your review, when the flow's risk is at most
  `external` (no commands, files, databases, GPIO or I2C) and, compared with
  the version you last approved, it does not raise the flow's risk (a flow
  you approved as safe stays safe), adds no privileged node, changes no
  recipient or destination (nor anything feeding a destination built from
  message data), reaches no new host, adds or moves no secret reference,
  turns on no risk switch, adds no typed credential, uses no credential for
  a different request, changes no rate of a flow that reaches out, changes
  nothing a global variable receives or a tool reads from one, changes
  nothing a published tool runs, and adds or changes no node that reads
  variables, message metadata or what a credential returned. A refusal names
  the kinds of change, never values. An agent may not stop, or replace with
  its draft, a flow it could not deploy.
- **All flows**: agents may deploy any flow, privileged ones included. A
  leaked token or a misled agent could take over the device.

At `low` you still accept that agents change, without review, what approved
flows send to approved destinations, their conditions and processing, and
whether they run, within those limits. For example, an agent may turn an
approved flow's MQTT command `{"state":"LOCK"}` into `{"state":"UNLOCK"}`
(whether or not the broker needs a password), rewrite a message's text, or
change an HTTP body sent with no credential to a device's API (a relay's
RPC). Keep device commands that matter in agent tools with confirmation
mode `admin` (what a tool runs cannot change at `low`, and each call waits
for you) or in HTTP requests that use a credential, or keep the level off. The nodes of a flow an agent deploys at
`low` also run on data the agent cannot read (a trigger's payload, what a
node computes from settings hidden from agents), and which of its nodes run
or fail, which the agent sees in `pw_get_run`, can reveal bits of that data
([ADR-0022](docs/adr/0022-low-keeps-tools-and-protected-data-the-admins.md)).
At `low` and `all` you also accept that an agent whose token also has the
`author` and `read` scopes can confirm guesses of short or predictable
secret values without your approval (see
[Security notes](#security-notes)). Every agent deploy and stop is audited
(**Agents → Audit**); a deploy is also marked **Deployed by agent**, but a
stop leaves no marker on the flow, so a flow an agent stopped shows only in
the audit.

Lowering the level (to `off`, or from `all` to `low`) stops new agent
deploys and undoes none: flows agents deployed keep running, privileged ones
included. Look for **Deployed by agent** and **Changed by agent** on the
flows, then stop them or use **History → Roll back** to return to the
version you last approved.

### Versions, markers and rollback

Every save of a flow's definition is a numbered version: the newest 50 are
kept, plus always the one you last approved. Deploying and stopping are not
versions. The editor sends the version it loaded when it saves or deploys;
if the flow changed since (an approved draft, an agent's deploy, another
tab), PiWorker answers `409` and the editor offers to reload it instead of
overwriting. Flow cards and the editor header show **Deployed by agent**
with the token's name when an agent deployed what runs, and **Changed by
agent** when the current version is not one you approved. **History** (on
each flow card and in the editor) lists the kept versions with who saved
them and which one is deployed, approved or from a draft; **Roll back to
version N** restores the version you last approved as a new version and
redeploys it when the flow runs. Runs record the version they ran, and
agents see the errors only of runs of a version you approved.

### Connecting Claude Code

Save PiWorker's CA, check its fingerprint, add the server with the token,
and start Claude Code trusting the CA:

```bash
curl -fo ~/piworker-ca.crt http://raspberrypi.local:8080/piworker-ca.crt
openssl x509 -in ~/piworker-ca.crt -noout -fingerprint -sha256
# Stop unless it prints the CA fingerprint shown by `piworkerctl install`,
# the PiWorker log or the editor's Trust this device page.
claude mcp add --transport http --scope user piworker https://raspberrypi.local:8443/mcp \
  --header "Authorization: Bearer pwk_…"
NODE_EXTRA_CA_CERTS=~/piworker-ca.crt claude   # for this command only
```

The CA is downloaded over plain HTTP, so the fingerprint check is what makes
it trustworthy. Node.js trusts `NODE_EXTRA_CA_CERTS` for **every** site, so
set it for the `claude` command only (an alias such as
`alias claude='NODE_EXTRA_CA_CERTS=~/piworker-ca.crt claude'` keeps it that
way) and never export it from your shell profile. To avoid trusting the CA
at all, run Claude Code through the piworker-mcp bridge instead, which pins
the CA for PiWorker only:
`claude mcp add --scope user piworker -- /usr/local/bin/piworker-mcp --url https://raspberrypi.local:8443/mcp --token-file ~/.piworker-token --ca-fingerprint SHA256:…`
(see below for the token file).

### Connecting Claude Desktop

Claude Desktop starts local (stdio) servers, so it connects through the
**piworker-mcp** bridge, which pins PiWorker's CA fingerprint and reads the
token from a private file (`--url`, `--token-file`, `--ca-fingerprint`). The
editor shows the exact commands for macOS/Linux and Windows when you create a
token; see [cmd/piworker-mcp/README.md](cmd/piworker-mcp/README.md) for
installing it. In `claude_desktop_config.json`, `"command"` must be the
**full path** of the bridge (Claude Desktop does not use your shell's
`PATH`); `--token-file ~/.piworker-token` may start with `~`. The bridge
accepts plain `http://` only to `127.0.0.1` or `[::1]`: with
`[tls] mode = "off"`, Claude Desktop must run on the PiWorker device itself,
so turn HTTPS on (`mode = "auto"`) to connect from other computers.

### Security notes

- A token is a password: anyone holding it has its access. Give each agent its
  own token with only the tools it needs, and revoke it when in doubt.
- Privileged tools (commands, files, databases, GPIO, I2C) need a token with
  the **privileged** flag; **all tools** never covers them. Arguments that
  reach privileged nodes must be strictly constrained by the schema.
- Agents never manage tokens, settings or approvals, and never read secrets
  or variables. **Agent deploy** is off by default; turning it on needs your
  password and is a risk you accept for every token with the deploy scope
  (see [Agent deploy](#agent-deploy)).
- What agents read hides secret values by comparing text with them, so an
  agent whose own text comes back to it can see whether a guess of a value
  comes back hidden, at any **Agent deploy** level: with the author and
  read scopes, through a draft you approve or, with the deploy scope at
  low or all, a flow it deploys; with the call scope, through a granted
  tool that repeats its arguments (many guesses per call); with the read
  scope, through an error a run records of what it sent (a tool call, a
  webhook, an MQTT message). Agents never see the run errors of a
  version no admin approved. Short or predictable values fall this way:
  use generated secrets of 16 characters or more (the **Secrets** dialog
  marks shorter ones and the **Agents** page counts them). Store
  credentials as secrets rather than typing them into settings, URL
  passwords (`https://user:password@host`) included: typed, they are
  hidden the same way but never checked. Run errors also hide the
  settings agents cannot see, so keep guessable passwords, paths and
  names out of those in flows whose runs agents may read
  ([ADR-0021](docs/adr/0021-agent-views-never-confirm-a-guess.md),
  [ADR-0022](docs/adr/0022-low-keeps-tools-and-protected-data-the-admins.md),
  [ADR-0023](docs/adr/0023-runs-record-the-version-they-ran.md)).
- Client-side confirmations are a convenience, not a control: use
  `confirm: admin` for anything that needs your approval.
- If an agent misbehaves, revoke its token, turn agent deploy off, roll back
  what it changed and read the audit: see
  [Agents: when one misbehaves](docs/operations.md#agents-when-one-misbehaves).

---

## Configuration

Configuration is layered, in order of increasing precedence:

**command-line flags > environment variables > TOML config file > built-in defaults**

### Config File (TOML)

PiWorker loads `piworker.toml` from the working directory if present (or pass
`-config <path>`). See [`piworker.example.toml`](piworker.example.toml):

```toml
addr = ":8443"          # HTTPS (plain HTTP when [tls] mode = "off")
http_addr = ":8080"     # redirects to HTTPS; "" disables it
webhooks_http = true    # serve /webhooks/ over plain HTTP on http_addr
db = "piworker.db"
auth = true
session_secure = false  # only for tls.mode = "off" behind an HTTPS proxy
cors_origins = []       # other origins allowed cross-origin access; empty: none
allowed_hosts = []      # extra Host names, see "Host names" below

[tls]
mode = "auto"           # auto | custom | off
hosts = []              # extra certificate names or addresses
cert_file = ""          # custom mode
key_file = ""           # custom mode

[mcp]                   # AI agents, see "AI agents (MCP)"
enabled = false         # mount /mcp (needs auth = true)
lan_access = false      # with tls.mode = "off": accept non-loopback clients
max_inflight = 4        # agent calls running at once (1-64)
max_result_bytes = 16384
author_enabled = false  # allow the author scope
```

### Command Line Flags

```bash
./piworker [flags]

Flags:
  -version              Print the build version and exit (also accepts --version)
  -config string        Path to TOML config file (default: piworker.toml if present)
  -addr string          Listen address: HTTPS, or HTTP with -tls=off (default ":8443")
  -http-addr string     HTTP listener that redirects to HTTPS; "" disables (default ":8080")
  -tls string           TLS mode: auto, custom or off (default "auto")
  -tls-regenerate-ca    Replace the local CA, then exit (devices must trust the new one)
  -db string            SQLite database path (default "piworker.db")
  -debug                Enable debug logging
  -auth                 Enable session authentication (default true)
  -session-secure        Mark session cookies Secure behind an HTTPS proxy (default false)
  -setup-code           Generate a new setup code and exit (only before setup)
  -recover-account name Generate a password recovery code and exit
  -cors-origins string  Comma-separated allowed CORS origins
  -allowed-hosts string Comma-separated extra Host names (see "Host names")
  -admin-user string    Bootstrap admin username (or env PIWORKER_ADMIN_USER)
  -admin-pass string    Bootstrap admin password (or env PIWORKER_ADMIN_PASS)
```

### Editing and running flows

Connections use the input/output ports declared by each node, including the
**True** and **False** branches of Switch. Choose ports on the canvas or in
**Connections**; saving and reopening preserves that choice. Invalid connections
from older definitions are flagged for reconnection before deployment.

**Deploy** starts the saved flow and schedules it for restoration when PiWorker
restarts. Deploying checks every enabled node's settings against its schema
(types, choices, ranges) and fails with the offending path and the expected
unit, e.g. `nodes[3].config.interval: expected number (milliseconds), got
string "5s"`. Choices match exactly as listed (`temperature`, not
`Temperature`), except where the node itself ignores case, such as the HTTP
method. Clearing a number field in the editor removes the setting, so the node
uses its default. Values of credential fields are never quoted in these errors. A
node definition without an `enabled` field is enabled. **Stop** ends execution and removes that restoration intent. The list
switch controls the same lifecycle; there is no separate flow-level “enabled”
setting. Stop a running flow before saving edits. Failed startup restoration is
shown as **Failed**.

**Upgrading from versions without these checks.** Earlier versions deployed
any settings and let nodes silently clamp or replace odd values: an interval
under 100 ms, a `0` left by a cleared number field, a choice in the wrong case,
a mistyped value. Flows that were running keep running after the upgrade:
restoring them at startup skips the schema check and logs each such setting as
a warning (`Restored flow has a setting that the next deploy will refuse`, with
its path), and the nodes treat those values exactly as before. Fix them before
the next explicit deploy, which refuses them. Restoration still marks a flow
**Failed** for errors that stopped it before too, for an HTTP Request node
that would send a secret to a URL whose scheme or host comes from a
placeholder, and for a Telegram bot token, Notify URL or Database DSN that
names a secret that does not exist (see below). HTTP Request headers stored as plain text by older
editors were always ignored; the editor now shows them with a warning above a
name/value list, where they can be entered again. A Command node now fails a
run whose placeholder value starting with `-` would begin an argument (see
**Command** below).

A deployed flow containing **Manual Inject** offers **Run manually**. Send the
saved node payload or a one-time JSON/text override; downstream actions execute
normally. Overrides do not change the saved node. Manual Inject wraps the value
in `payload.data`, with `payload.count` and `payload.timestamp`, and uses the
saved topic. The injection API treats `null` as “use the configured payload”, so
the manual dialog rejects custom JSON `null` instead of silently changing it.

**Activity → Runs** lists the latest 50 recorded executions, even when the flow
is stopped. Open a run to inspect node identities, durations and errors; refresh
its details to see newly recorded events. History does not store full payloads.
The live **Debug** view is separate and keeps messages only in browser memory.

### User variables and secrets

Open **Variables** from the flow list to manage globals. In a saved flow editor,
choose **This flow** or **Global · all flows** to create, edit or delete values.
The dialog includes **How to use variables** with syntax and scope examples. Choose Text or JSON; JSON preserves numbers, booleans, objects, arrays and
`null`. Global variables are shared by all flows; flow variables are isolated by flow ID.
Both scopes persist in SQLite. Get/Set Variable nodes have an explicit scope
selector, defaulting to global for existing configurations. Failed writes leave the previous value intact
and report an error. Changes are read on subsequent node executions; they do not
restart flows. Saved variable values are not included when exporting or duplicating a flow.
Configure values for the new flow separately. Deleting a flow deletes its scoped
values; stopping or redeploying it preserves them. The legacy `variables` field
in a flow definition remains unused; the dedicated variables API is authoritative.

Use `vars["threshold"]` in Transform, Filter, Switch and Set Variable expressions,
for example `payload.temperature > vars["threshold"]`. Fields rendered as message
templates also support `{{vars.threshold}}`. Missing template variables produce an
empty string; expressions can use defaults, such as `vars["threshold"] ?? 25`.
For flow scope, use `flowVars["threshold"]` in expressions and
`{{flowVars.threshold}}` in templates. The same name can exist in both scopes;
there is no fallback from flow to global. Get Variable retains its configured
default when the key is absent.

Compatible editor fields offer autocomplete, **Insert reference**, and
**Ctrl+Space**. Use arrow keys and Enter/Tab to select, or Escape to dismiss.
Get/Set Variable fields suggest names from the selected scope; expression and
template references distinguish `vars` and `flowVars`. Declared Set Variable
names are also suggested in their scope, even before they have a value.
Playground tests read copies of both scopes; testing Set Variable does not mutate live values.

Open **Secrets** in the flow list to store or replace credentials. Its API lists
only names and marks values shorter than 16 characters (never values or
lengths; see the AI agents [security notes](#security-notes)). In compatible
credential fields, autocomplete inserts `{{secret.NAME}}`. Supported fields are
Telegram's bot token, Notify's URL, Database's DSN, and the username/password
fields of Email, MQTT Publish and MQTT Subscribe. These references resolve when the node uses them (every Telegram
message, Notify request and e-mail; the first Database query and the MQTT
connection), so a rotated secret applies without a redeploy, except to a
Database or MQTT connection that is already open. A flow whose Telegram bot
token, Notify URL or Database DSN names a secret that does not exist is refused
at deploy (and such a node fails if the secret is deleted later); in the
optional Email and MQTT credentials a missing secret resolves to an empty
string. That case, and a missing secret in an HTTP Request URL or header, is
not refused but reported: the deploy answers it as a warning
(`unresolved_secret`), which the editor shows until you dismiss it, and the
startup log repeats it for the flows it restores (ADR-0028).

The Secrets dialog says where each secret is used: every flow node setting
(and whether that flow is deployed or that node disabled) and every broker
connection credential that references it. Deleting a used secret asks first
and says how many places use it. Names referenced somewhere that no secret
has are listed under **Referenced but not defined**, each with a button that
prefills the name for you to define it.

Two places have further rules:

- **HTTP Request**: header values (`Authorization: Bearer {{secret.api_token}}`)
  and the URL's `user:password@` part and query string
  (`?apikey={{secret.api_key}}`, query-encoded). A secret placed in the scheme,
  host, path or fragment fails the deploy, and one that does not exist fails
  the request before anything is sent; `meta.httpURL` and errors show the
  placeholder. The request body never resolves secrets. When the URL or a
  header uses a secret, the URL must start with a literal scheme and host
  (`https://api.example.com/...`), so that message data or a variable such as
  `{{payload.url}}` or `{{flowVars.base}}` cannot choose where the secret goes;
  otherwise the deploy fails with `secrets require a fixed scheme and host`.
  Placeholders after the host stay allowed. A redirect to another host does
  not receive the headers that carry secrets, nor a `Referer` repeating a
  query secret.
- **Webhook** token: deploying fails if the secret does not exist; if it is
  deleted or emptied later, every request is rejected (the check is never
  turned off).

Secrets are only resolved where they are written in a node's configuration,
never inside message data, so a payload cannot extract them. General templates
and expressions cannot read the secret store.

Secrets are stored in SQLite without application-level encryption at rest
(a deliberate decision, [ADR-0030](docs/adr/0030-no-application-encryption-of-secrets-at-rest.md);
the threat review is in [docs/design/secrets-at-rest.md](docs/design/secrets-at-rest.md)).
On a Raspberry Pi any key would have to sit on the same SD card for
PiWorker to restart unattended, so it would not stop someone holding the
card or a copy of the data directory. What protects them instead: PiWorker
creates its database private (mode 0600) and removes other users' access
from an existing one at startup, logging it; secrets leave the device in a
file only through an export you encrypt with a passphrase; and on a device
that might be stolen, use full-disk encryption and keep backups as you would
keep passwords. They
are excluded from flow exports when referenced by name; literal credentials
entered directly in a node configuration remain part of that configuration.
Variables are ordinary data, visible in their manager and usable in payloads and
Debug; use the separate secret store for credentials.

### Broker connections

**Broker connections** (in the navigation, under Settings) name an MQTT
broker once: its URL (`tcp`, `mqtt`, `ssl`, `tls`, `mqtts`, `ws`, `wss`; no
credentials in it), an optional username and password (both may be
`{{secret.NAME}}` references; the password is write-only) and a
Zigbee2MQTT base topic. Zigbee nodes always use one; **MQTT Subscribe** and
**MQTT Publish** take either a `connection` or their own `broker` (with
their own optional credentials), never both (ADR-0026, ADR-0029).

Saving a connection restarts every running flow whose enabled nodes use it,
so they reconnect with the new settings; the answer lists the flows
restarted and any that could not start again (they are marked failed).
Deleting a connection that a running flow uses is refused (409) and names
the flows; a stopped flow that names a missing connection is refused at
deploy (`undefined_connection`). **Test connection** tries settings without
storing them.

### Environment Variables

```bash
export PIWORKER_ADDR=:8443
export PIWORKER_HTTP_ADDR=:8080      # empty disables the redirect listener
export PIWORKER_WEBHOOKS_HTTP=true
export PIWORKER_TLS_MODE=auto        # auto | custom | off
export PIWORKER_TLS_HOSTS=raspberrypi.local,192.168.1.50
export PIWORKER_TLS_CERT_FILE=/etc/piworker/cert.pem  # custom mode
export PIWORKER_TLS_KEY_FILE=/etc/piworker/key.pem    # custom mode
export PIWORKER_DB=/var/lib/piworker/data.db
export PIWORKER_DEBUG=true
export PIWORKER_AUTH=true
export PIWORKER_SESSION_SECURE=false # true only with TLS off behind an HTTPS proxy
export PIWORKER_CORS_ORIGINS=https://dashboard.example  # empty: no other origin
export PIWORKER_ALLOWED_HOSTS=piworker.example.com,backend
export PIWORKER_ADMIN_USER=admin
export PIWORKER_ADMIN_PASS=...
export PIWORKER_MCP_ENABLED=true     # [mcp] enabled
export PIWORKER_MCP_LAN_ACCESS=false # [mcp] lan_access
export PIWORKER_MCP_MAX_INFLIGHT=4
export PIWORKER_MCP_MAX_RESULT_BYTES=16384
export PIWORKER_MCP_AUTHOR_ENABLED=false
```

Boolean variables accept `true/false`, `1/0`, `yes/no` or `on/off`; an empty or
unrecognised value stops startup with an error (it never silently disables
authentication). A config file named with `-config` or `PIWORKER_CONFIG` must
exist; only the implicit `piworker.toml` may be absent. Unknown keys in the TOML
file (typos such as `atuh`) are rejected at startup.

Prometheus metrics are served at `/api/metrics`. When authentication is enabled
they require a signed-in session.

Write requests to `/api/` must come from the editor
even when authentication is disabled: they need the `X-PiWorker-Request: 1`
header, a same-origin or `cors_origins` Origin, and a JSON body.
`cors_origins` is empty by default: the editor is served from the same origin
and needs no CORS. Only the origins it lists get cross-origin (credentialed)
access; an empty list allows no other origin, never any origin.

### Host names (DNS rebinding protection)

The API, the web UI and the MCP endpoint only answer requests whose `Host`
header is a name of this server; others get `403` with a JSON error. This stops
a malicious web page whose DNS name is re-pointed at your Pi from using your
browser to reach PiWorker. Accepted without configuration:

- `localhost`, the machine's hostname and `<hostname>.local` (mDNS),
- the current interface addresses and, in fact, any IP address
  (an attack needs a DNS name),

ignoring the port and letter case. The hostname and addresses are re-read
every 30 seconds. Add any other name you use (a DNS name, a reverse proxy's
name, the Docker host's name) with `allowed_hosts` in the TOML file,
`PIWORKER_ALLOWED_HOSTS` (comma-separated) or `-allowed-hosts`. Webhooks
(`/webhooks/`) and the CA certificate download (`/piworker-ca.crt`) accept any
name, because external devices call them. The production Compose file sets
`PIWORKER_ALLOWED_HOSTS` (`raspberrypi,raspberrypi.local` by default, override
it with your host's name). In development the Vite proxy passes on the Host the
browser used, so the API sees the editor as same-origin; list any name other
than localhost or an address you open the dev server by.

### Sessions and account recovery

The browser uses an HttpOnly, SameSite=Strict cookie; session secrets are never
stored in JavaScript/localStorage. SQLite stores only token hashes. Sessions
survive restarts, expire after 7 days of inactivity or 30 days total, and are
limited to 20 per account. Signing out revokes the current session. Changing the
password from **Account** or recovering it invalidates every session for that
account, including live event streams (on the next event or heartbeat).

### HTTPS

PiWorker serves **HTTPS by default** (`[tls] mode = "auto"`). On first start it
creates a **local certificate authority (CA)** and a server certificate signed
by it, both in the `tls` directory next to the database (`ca.crt`, `ca.key`,
`server.crt`, `server.key`; keys are readable only by the PiWorker user). The
CA is valid for 10 years; the server certificate for 397 days.

| Address | Serves |
|---------|--------|
| `https://<device>:8443` (`addr`) | The editor, `/api`, webhooks |
| `http://<device>:8080` (`http_addr`) | 308 redirect to HTTPS, plus `/piworker-ca.crt` and, with `webhooks_http`, `/webhooks/` |

**Encryption versus trust.** The connection is encrypted from the first visit.
Until a device trusts PiWorker's CA, its browser cannot tell PiWorker from an
impostor on the same network and shows a certificate warning. Trust the CA once
per device: open **Trust this device** (`/trust`, linked from the sign-in page,
the flow list and Account). It offers the CA download (`/piworker-ca.crt`) and
step-by-step instructions to install and remove the trust on macOS, Windows,
Linux, iPhone/iPad, Android and Firefox (which keeps its own store). After that,
a certificate warning means something is wrong.

**Verify the fingerprint** of the CA you download against one obtained on the
device itself, not only against a page loaded over the network:

```bash
sudo piworkerctl install                                    # prints it (native service)
sudo journalctl -u piworker | grep "CA fingerprint"         # startup log
docker logs piworker 2>&1 | grep "CA fingerprint"           # Docker
openssl x509 -in /var/lib/piworker/tls/ca.crt -noout -fingerprint -sha256
```

Signed-in users also see it on the trust page and at `GET /api/tls` (mode, CA
fingerprint, certificate names and expiry).

**Renewal and address changes.** The server certificate renews itself 30 days
before it expires, and within 10 minutes (or on `SIGHUP`) when the device gets a
new address or host name; the running server switches without a restart and
devices that trust the CA notice nothing. The certificate covers `localhost`,
`127.0.0.1`, `::1`, the host name, `<hostname>.local` and every current interface
address. Add other names (for example a DNS name from your router, or the host's
address when running in Docker) to `[tls] hosts` or `PIWORKER_TLS_HOSTS`. The CA
itself is never replaced automatically.

**Command-line clients and Claude Code.** Tools such as curl and Node.js may not
use the system trust. Save the CA (for example
`curl -o ~/piworker-ca.crt http://raspberrypi.local:8080/piworker-ca.crt`), check
its fingerprint, then use `curl --cacert ~/piworker-ca.crt …` or, for Claude Code,
start it with `NODE_EXTRA_CA_CERTS=~/piworker-ca.crt claude`: for that command
only, never exported from your shell profile, since Node.js then trusts the CA
for every site (see [Connecting Claude Code](#connecting-claude-code)).

**What trusting the CA means.** A trusted CA can vouch for any website on that
device. PiWorker stores the CA key only in its data directory (`tls/ca.key`), but
flows run as the PiWorker user: anyone who can create command flows in PiWorker,
or read its data directory or update backups, can copy the key and use it to
impersonate any website to devices that trust the CA. Protect PiWorker accounts,
the device and its backups accordingly, and remove the trust when you stop using
PiWorker. If the key may have been exposed, replace the CA and trust the new one:

```bash
sudo systemctl stop piworker
sudo -u piworker /usr/local/bin/piworker -config /etc/piworker/piworker.toml -db /var/lib/piworker/piworker.db -tls-regenerate-ca
sudo systemctl start piworker
```

Run it as the service user so the new files stay readable by the service.

**Webhooks over HTTP.** Many devices (sensors, microcontrollers) cannot trust a
custom CA, so `webhooks_http = true` (the default) also serves `/webhooks/` on
the plain HTTP port. Their tokens then travel unencrypted; set
`webhooks_http = false` (or `PIWORKER_WEBHOOKS_HTTP=false`) when every caller can
use HTTPS.

**Your own certificate.** Set `mode = "custom"` with `cert_file` and `key_file`
(PEM). PiWorker reloads them on `SIGHUP` (`sudo systemctl kill -s HUP piworker`).
There is no PiWorker CA to download in this mode.

**Behind a reverse proxy.** Set `mode = "off"`: `addr` then serves plain HTTP and
`http_addr` is unused. Set `session_secure = true` when browsers reach the proxy
over HTTPS. Forwarded headers do not change cookie security. With TLS on,
session cookies are always `Secure`.

**Recovery.** If HTTPS does not start or you cannot reach it, run
`sudo piworkerctl tls off` (managed service), start the binary with `-tls=off`,
or set `PIWORKER_TLS_MODE=off` (Docker). PiWorker then serves plain HTTP on
`addr` (port 8443 by default) and the editor shows its unencrypted-connection
warning. `sudo piworkerctl tls on` restores HTTPS with the local CA. A missing
or mismatched `ca.key` stops startup instead of silently creating a new CA;
restore it from a backup or regenerate the CA as shown above.

### Recovering access

If you forget your password, choose **Forgot password?** on the sign-in page.
On the device hosting PiWorker, run this using the **same database as the server**:

```bash
# Native managed service (use the service account to preserve file ownership):
sudo -u piworker /usr/local/bin/piworker -config /etc/piworker/piworker.toml -db /var/lib/piworker/piworker.db -recover-account YOUR_USERNAME
# Manual binary:
./piworker -db /path/to/piworker.db -recover-account YOUR_USERNAME
# Docker (container remains running):
docker exec piworker ./piworker -db /app/data/piworker.db -recover-account YOUR_USERNAME
```

The command prints a one-use code valid for 30 minutes. Enter it in the recovery
form with your username and new password. It does not reset the password until
that code is redeemed. Generating another code replaces the previous one. No
email, public recovery-code endpoint, or additional user registration is involved.

API clients must keep the cookie returned by login and send
`X-PiWorker-Request: 1` on POST/PUT/PATCH/DELETE requests (including login).
Authentication request bodies must contain one UTF-8 JSON value with unique
field names; use the documented lowercase/camelCase field names. Duplicate
fields, invalid UTF-8 and trailing JSON values are rejected.
Browser origins must match the server or an explicitly configured CORS origin.
Login/setup/recovery/password attempts are rate limited. Only cookie sessions are
accepted (no Bearer tokens).

### Webhooks

Deploy a flow with a **Webhook** trigger and external services can start it by
calling `https://<host>:8443/webhooks/<path>`, or `http://<host>:8080/webhooks/<path>`
for devices that cannot trust the local CA (see `webhooks_http` in [HTTPS](#https)).
These endpoints live outside `/api`, so the administrator's session cookie is
never sent to them, and they use their own optional token authentication. Set a token on the node to
require `?token=...` or an `X-Webhook-Token` header; prefer storing it as a
secret and writing `{{secret.NAME}}` in the token field.

---

## Make Commands

```bash
# Development
make run              # Build and run
make test             # Run all tests
make test-verbose     # Run tests with verbose output
make test-race        # Run tests with the race detector
make lint             # Run linter
make hooks-install    # Install the git pre-commit hook (auto-gofmt staged files)

# Frontend
make frontend-install # Install dependencies (bun)
make frontend-dev     # Start dev server
make frontend-test    # Run frontend unit tests
make frontend-test-e2e # Run Playwright tests (install Chromium first)
make frontend-build   # Build for production
make frontend-embed   # Build and embed in Go binary

# Docker
make docker-dev       # Start dev environment
make docker-dev-down  # Stop dev environment
make docker-build     # Build production image
make docker-prod      # Run production container

# Release
make build-release    # Build optimized binary with embedded frontend
make build-arm64      # Build for Raspberry Pi 4
make build-arm        # Build for Raspberry Pi 3
```

---

## API Reference

### Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/auth/status` | Whether auth is enabled and first setup is required |
| POST | `/api/auth/setup` | Create the first administrator using a local one-use code |
| POST | `/api/auth/login` | Start a cookie session |
| GET | `/api/auth/session` | Read the current session |
| POST | `/api/auth/logout` | Revoke the current session |
| POST | `/api/auth/password` | Change password and revoke all account sessions |
| POST | `/api/auth/recover` | Redeem a local recovery code and revoke account sessions |

There is no public registration endpoint, user-management UI, or role system.
The initial scope is one administrator per installation; updates keep existing
accounts.

### Flows

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/flows` | List all flows |
| POST | `/api/flows` | Create a new flow |
| GET | `/api/flows/{id}` | Get a flow by ID |
| PUT | `/api/flows/{id}` | Update a flow; with the loaded `version`, `409` (`currentVersion`) when another was saved since |
| DELETE | `/api/flows/{id}` | Delete a flow |
| POST | `/api/flows/{id}/deploy` | Deploy (start) a flow; optional `{"version": N}`, checked like a save. Answers `{"flow", "running", "warnings"}`: `warnings` lists what it deployed with but you should know, such as `unresolved_secret` (Lint's warning issues; empty when none) |
| POST | `/api/flows/{id}/stop` | Stop a running flow |
| PATCH | `/api/flows/{id}/toggle` | Turn a flow on or off: `{"enabled": bool, "version": N}`; turning it on with a stale `version` is `409` (`currentVersion`) and deploys nothing; turning it on answers `warnings` as deploy does |
| GET | `/api/flows/{id}/versions` | Version history (`limit`), newest first |
| POST | `/api/flows/{id}/rollback` | Restore the version last approved as a new version (`409` when there is none to restore) |
| POST | `/api/flows/{id}/duplicate` | Create a stopped copy of a flow |
| GET | `/api/flows/{id}/export` | Download a flow definition (no variables or secret values) |
| POST | `/api/flows/import` | Create a flow from an exported definition |
| POST | `/api/flows/{id}/inject/{nodeId}` | Fire a Manual Inject node of a running flow; optional `{"payload": …}` override |

### Runs and live events

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/flows/{id}/runs` | Recorded executions, newest first (`limit`, 50 by default) |
| GET | `/api/flows/{id}/runs/{runId}/events` | Node events, durations and errors of one run (no payloads) |
| GET | `/api/flows/{id}/events` | Live event stream (Server-Sent Events) used by the editor's Activity panel |

### Variables and secrets

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/variables` | List global variables |
| PUT, DELETE | `/api/variables/{name}` | Set (`{"value": …}`) or delete a global variable |
| GET | `/api/flows/{id}/variables` | List the variables of one flow |
| PUT, DELETE | `/api/flows/{id}/variables/{name}` | Set or delete a flow variable |
| GET | `/api/secrets` | `{"secrets": [{"name", "short", "uses"}], "undefined": [{"name", "uses"}], "shortBelow": 16}`: names only, never values or lengths; `short` marks a value under `shortBelow` characters, which an agent could confirm a guess of. `uses` lists where each secret is referenced (`flowId`, `flowName`, `flowRunning`, `nodeId`, `nodeType`, `nodeDisabled`, `setting`, or `connection` and `setting`); `undefined` lists the referenced names no secret has |
| PUT, DELETE | `/api/secrets/{name}` | Set (`{"value": "…"}`) or delete a secret |

### Broker connections and Zigbee

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/connections` | `{"connections": [{"name", "broker", "username", "hasPassword", "baseTopic"}]}`, never the password |
| PUT | `/api/connections/{name}` | Create or replace (`{"broker", "username", "password", "baseTopic"}`; no `password` keeps the stored one, `""` clears it). Restarts the running flows that use it: `{"restarted": [{"id", "name"}], "failed": [{"id", "name", "error"}]}` |
| DELETE | `/api/connections/{name}` | Delete; `409` with `data.flows` while a running flow uses it |
| POST | `/api/connections/{name}/test` | Connect with the settings sent (the stored password when none is sent), store nothing: `{"ok", "error"}` |
| GET | `/api/zigbee`, `/api/zigbee/{name}` | Zigbee2MQTT networks: bridge state, devices and their last state |
| POST | `/api/zigbee/{name}/permit-join` | Allow devices to join for a while |

### Node Types

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/node-types` | List available node types (`category` filters by `input`, `processing` or `output`); each carries its `risk` (`safe`, `external`, `privileged`) |
| GET | `/api/node-types/{type}` | One node type with its documentation and configuration schema |
| POST | `/api/nodes/{type}/test` | Run one processing node once on a sample message (the editor's payload playground); nothing is deployed |

### Health and metrics

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | HTTP 200 when healthy; HTTP 503 when storage is closed |
| GET | `/api/metrics` | Prometheus metrics (needs a session when authentication is on) |

### HTTPS

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/piworker-ca.crt` | The local CA certificate (PEM); public, also on the HTTP port; 404 without a local CA |
| GET | `/api/tls` | TLS mode, CA fingerprint, certificate names and expiry (authenticated) |

### Agent access (editor session only)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET, POST | `/api/agent/tokens` | List tokens; create one (password step-up, plaintext shown once) |
| DELETE | `/api/agent/tokens/{id}` | Revoke a token |
| POST | `/api/agent/tokens/revoke-all` | Revoke every token at once; returns how many were active |
| POST | `/api/agent/tokens/{id}/grants` | Add, re-confirm (password), suspend or remove a grant |
| GET | `/api/agent/tools` | Published tools, for grant pickers |
| GET | `/api/agent/approvals` | Approval queue (`status`, `limit`) |
| GET | `/api/agent/approvals/pending-count` | Pending approvals and drafts (does not extend the session) |
| POST | `/api/agent/approvals/{id}/approve`, `/reject` | Decide; approving runs the call and returns its result |
| GET | `/api/agent/drafts` | Agents' drafts (`status` pending, decided or all; `limit`, `beforeId` paging) |
| GET | `/api/agent/drafts/{id}` | A draft with its issues, alerts and diffs, recomputed against the current version |
| POST | `/api/agent/drafts/{id}/approve`, `/reject` | Approve (`{"deploy": bool}`; saves a new version) or reject (optional `reason`) |
| GET | `/api/agent/audit` | Agent calls, newest first (filters, `beforeId` paging) |
| GET | `/api/agent/audit/rejections` | Rejected calls per token and reason (`tokenId`, `since`, `until`) |
| GET, PUT | `/api/agent/settings` | Agent deploy level (PUT needs the password), MCP status |
| POST | `/api/flows/{id}/call/{nodeId}` | Call a published tool as the editor (Test tool) |

---

## Tech Stack

### Backend
- **Go 1.27.3+** - High performance, great concurrency
- **gorilla/mux** - HTTP router
- **SQLite** - Local database (no external dependencies)
- **zerolog** - Structured JSON logging
- **go:embed** - Frontend embedded in binary

### Frontend
- **Vue 3** - Reactive UI framework
- **Vue Flow** - Visual flow editor canvas
- **Tailwind CSS** - Utility-first styling
- **shadcn-vue** - Accessible UI components
- **Pinia** - State management
- **Vite** - Dev server and bundler (required for Vue SFC support)
- **Bun** - JavaScript runtime and package manager (faster than Node.js/npm)

---

## Project Structure

```
piworker/
├── main.go                 # Application entry point
├── Dockerfile              # Production multi-stage build
├── Dockerfile.dev          # Development with hot-reload
├── docker-compose.yml      # Dev environment
├── docker-compose.prod.yml # Production environment
├── Makefile                # Build automation
│
├── cmd/piworker-mcp/       # Claude Desktop stdio bridge to /mcp
├── scripts/                # piworkerctl installer and validation scripts
├── docs/                   # User guide, operations, ADRs and design
│
├── internal/
│   ├── api/                # REST API handlers
│   ├── flow/               # Flow engine (runtime, router, models, lint, diff, dry-run)
│   ├── node/               # Node system
│   │   ├── builtin/        # Built-in node implementations
│   │   └── ...
│   ├── service/            # Agent tool calls, reads, drafts and deploy policy
│   ├── mcpserver/          # The /mcp endpoint
│   ├── agentauth/          # Agent token verification
│   ├── toolschema/         # Closed JSON Schema subset of tool inputs
│   ├── tlsca/              # Local CA and server certificate
│   ├── config/             # Defaults, TOML and environment configuration
│   ├── vars/, secrets/     # Variable and secret stores
│   ├── webhook/            # The /webhooks/ hub
│   ├── events/, metrics/   # Run events and Prometheus metrics
│   ├── storage/            # SQLite persistence
│   ├── types/              # Shared types (Message, Port, etc.)
│   └── webui/              # Embedded frontend assets
│
└── web/                    # Vue 3 frontend
    ├── src/
    │   ├── components/     # UI components
    │   ├── views/          # Page views
    │   ├── stores/         # Pinia stores
    │   └── ...
    └── ...
```

---

## Validation and releases

Builds accept `VERSION`, for example `make build-release VERSION=vX.Y.Z`.
CI injects the tag (or commit SHA for branch builds); Docker publication passes
the release tag as the `VERSION` build argument. `piworker -version` prints that
value without loading configuration or opening the database. Direct `go build`
and Docker builds without an explicit version identify themselves as `dev`.

CI runs on pull requests and pushes to `master`, `main`, and `revival`. It checks
the installer lifecycle with isolated OS/download fixtures and ShellCheck,
Go formatting, `go vet`, race-enabled tests, frontend unit/browser tests and typechecking/build, Linux
CGO builds for amd64/arm64/ARMv7, and multi-platform Docker builds. Linux archives
and SHA256 checksums are available as workflow artifacts.

Pushing a `v*` tag runs the same checks and prepares a **draft GitHub release**.
A tag with a hyphen (`v1.0.0-beta.1`) is drafted as a **prerelease**: it does
not become GitHub's latest release and does not move the image's `latest` tag.
Review the binaries before publishing the draft. Publishing a release triggers
GHCR image publication for amd64, arm64, and ARMv7; stable releases also update
`latest`. Make the GHCR package public for anonymous pulls. The release tag must
point to a commit containing both workflows. Publish the draft manually in GitHub;
publishing it from another workflow with `GITHUB_TOKEN` does not trigger the image
workflow. See the [release checklist](docs/release-checklist.md) for the complete
sequence and GitHub event references. Creating or editing these files locally
does not publish anything.

Dependabot monitors Go modules, the Bun lockfile in `web/`, and GitHub Actions.

## Security

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Follow [AGENTS.md](AGENTS.md): write a failing behavior test, implement, then refactor
4. Update the documentation with the change: the [user guide](docs/guide/README.md) for anything users see; after adding a node or changing a setting, regenerate the node reference with `go test -run TestNodeReferenceDocIsCurrent ./internal/node/builtin -update`
5. Run relevant checks: `make test` for Go, `make frontend-test frontend-test-e2e` for frontend behavior; use `make test-race` for concurrency changes
6. Commit: `git commit -m "Add my feature"`
7. Push: `git push origin feature/my-feature`
8. Open a Pull Request

---

## License

[AGPL-3.0](LICENSE.md)

> **Disclaimer**: I am not responsible for the misuse that may be given to this software. Use it at your own risk.
