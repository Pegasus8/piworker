# PiWorker

[![GitHub license](https://img.shields.io/github/license/Pegasus8/piworker)](https://github.com/Pegasus8/piworker/blob/master/LICENSE.md)

A **visual flow-based automation system** for Raspberry Pi and other devices. Create automation workflows by connecting nodes in a visual editor, similar to Node-RED but built with Go for performance and Vue 3 for a modern UI.

## Features

- **Visual Flow Editor** - Drag-and-drop interface to create automation workflows
- **Node-Based** - Connect triggers, processing nodes, and actions
- **Local execution** - Flows and storage run on your device; external-service nodes send data to the services you configure
- **Extensible** - Easy to add new node types
- **Lightweight** - Single binary with embedded frontend, optimized for Raspberry Pi
- **Fast** - Go backend with concurrent message processing

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

Open `http://<device-address>:8080`. Get the first administrator setup code with
`sudo journalctl -u piworker -n 50 --no-pager`, then complete setup in the browser.
The service also starts after a reboot. Re-running `install` updates a managed
installation; reinstalling after `uninstall` reuses the retained data.

You can select a release explicitly with `sudo piworkerctl update vX.Y.Z`
(replace `vX.Y.Z` with an actual compatible release tag). Updates also replace
the manager with the version included in the verified release archive.
Before the first compatible release, contributors can run the lifecycle tests
with `make test-installer`; running the installer still requires published assets.

| Item | Location / command |
|------|--------------------|
| Configuration | `/etc/piworker/piworker.toml` |
| Accounts, flows, secrets and variables | `/var/lib/piworker/piworker.db` |
| Update backups (private, retained until you remove them) | `/var/backups/piworker/` |
| Logs / status | `sudo journalctl -u piworker` / `systemctl status piworker` |
| Restart after changing configuration | `sudo systemctl restart piworker` |

Updates download and validate before stopping the service. They back up its
entire data directory and configuration, replace the executable, and check the
HTTP health endpoint. A failed update restores the previous binary and data;
updates briefly interrupt running flows. Backups cannot undo actions already
sent to external devices or services. Ensure there is enough disk space for a
full data copy. If automatic recovery cannot stop/start the service, the command
reports the backup location for manual recovery.

The managed service runs as the dedicated `piworker` user and always uses the
database path above. Grant that user the device-group permissions needed for
GPIO/I2C hardware. If you change the HTTP address, provide the corresponding
local health URL when installing/updating, for example
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
from the shell or a local, git-ignored `.env` file. Existing installations keep
their accounts and password hashes; old JWT sessions require one new login.
Shell exports do not configure the native systemd service: use its TOML file or
an explicitly configured systemd environment. Browser setup needs no bootstrap
credentials. Compatibility with databases from the legacy 2020 release has not
been validated; keep a full backup and test migration on a copy first.

This README describes the revival branch, including changes not yet released.
Build from source or use `make docker-build` to try this implementation. Published
binaries/images may still contain the previous implementation; check their release notes.

### Option 1: Docker (published releases)

```bash
# Production - single container with everything included
docker run -d \
  --name piworker \
  --restart unless-stopped \
  -p 8080:8080 \
  -v piworker-data:/app/data \
  -e PIWORKER_ADMIN_USER -e PIWORKER_ADMIN_PASS \
  ghcr.io/pegasus8/piworker:latest

# Open http://localhost:8080
```

### Option 2: Pre-built Binary (published releases)

Download the archive for your CPU and `SHA256SUMS` from the same compatible
[release](https://github.com/Pegasus8/piworker/releases). For example, on Linux ARM64:

```bash
# Use amd64 or arm instead for the other supported CPUs.
awk '$2 == "./piworker-linux-arm64.tar.gz" || $2 == "piworker-linux-arm64.tar.gz"' SHA256SUMS | sha256sum -c - &&
tar -xzf piworker-linux-arm64.tar.gz &&
./piworker-linux-arm64
# Open http://localhost:8080
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
| **Go** | 1.27.1+ | Backend compilation | [go.dev/dl](https://go.dev/dl/) |
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
go version    # go1.27.1+
bun --version # 1.3.14
```

A C compiler is required: SQLite uses CGO. Install Xcode Command Line Tools
on macOS or `build-essential` on Debian/Ubuntu. Bun is needed only to build the
frontend; release binaries include it already.

#### Quick Install (Linux/Raspberry Pi)

```bash
# Install Go
wget https://go.dev/dl/go1.27.1.linux-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.27.1.linux-arm64.tar.gz
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
go run . -addr 127.0.0.1:8080 -debug -auth=false
# API running at http://localhost:8080
```

**Terminal 2 - Frontend:**
```bash
cd web
bun install --frozen-lockfile
bun run dev
# UI running at http://localhost:3000
```

Open http://localhost:3000 - the frontend proxies API calls to the backend.

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

- **Backend**: http://localhost:8080 (Go with air hot-reload)
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
  -p 8080:8080 \
  -v piworker-data:/app/data \
  -e PIWORKER_ADMIN_USER -e PIWORKER_ADMIN_PASS \
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
ExecStart=/home/pi/piworker -addr :8080 -db /home/pi/piworker.db
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
| **Webhook** | `trigger-webhook` | Fire on an incoming HTTP request to `/api/webhooks/<path>` |
| **RSS / Atom** | `trigger-rss` | Poll a feed for new entries |
| **MQTT Subscribe** | `trigger-mqtt` | Receive messages from an MQTT broker |
| **GPIO Edge** | `trigger-gpio` | React to Raspberry Pi pin changes |
| **DS18B20 Temperature** | `trigger-ds18b20` | Poll a 1-Wire temperature sensor |
| **System Metric** | `trigger-sysmetric` | Fire when CPU / memory / temperature crosses a threshold |

### Processing
| Node | Type | Description |
|------|------|-------------|
| **Delay** | `process-delay` | Delay messages by a specified duration |
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
| **Command** | `action-command` | Execute shell commands (inputs are shell-escaped) |
| **HTTP Request** | `action-http` | Call an HTTP API (SSRF-guarded, size-capped) |
| **Notify** | `action-notify` | Send to Discord / Slack / ntfy |
| **Telegram** | `action-telegram` | Send a message via a Telegram bot |
| **Email** | `action-email` | Send an email via SMTP |
| **Database** | `action-database` | Execute SQLite, PostgreSQL or MySQL queries |
| **MQTT Publish** | `action-mqtt` | Publish to an MQTT broker |
| **GPIO Write** | `action-gpio` | Set a Raspberry Pi output pin |
| **I2C Read** | `action-i2c` | Read from an I2C device |
| **Write File** | `action-file` | Write or append the message to a file |

> Each node ships an in-app documentation card and a configuration schema, so the
> editor renders its settings form automatically.

---

## Configuration

Configuration is layered, in order of increasing precedence:

**command-line flags > environment variables > TOML config file > built-in defaults**

### Config File (TOML)

PiWorker loads `piworker.toml` from the working directory if present (or pass
`-config <path>`). See [`piworker.example.toml`](piworker.example.toml):

```toml
addr = ":8080"
db = "piworker.db"
auth = true
session_secure = false # Secure cookies only; does not enable an HTTPS listener
cors_origins = ["http://localhost:3000", "http://localhost:8080"]
```

### Command Line Flags

```bash
./piworker [flags]

Flags:
  -version              Print the build version and exit (also accepts --version)
  -config string        Path to TOML config file (default: piworker.toml if present)
  -addr string          HTTP server address (default ":8080")
  -db string            SQLite database path (default "piworker.db")
  -debug                Enable debug logging
  -auth                 Enable session authentication (default true)
  -session-secure        Require HTTPS for session cookies (default false)
  -setup-code           Generate a new setup code and exit (only before setup)
  -recover-account name Generate a password recovery code and exit
  -cors-origins string  Comma-separated allowed CORS origins
  -admin-user string    Bootstrap admin username (or env PIWORKER_ADMIN_USER)
  -admin-pass string    Bootstrap admin password (or env PIWORKER_ADMIN_PASS)
```

### Editing and running flows

Connections use the input/output ports declared by each node, including the
**True** and **False** branches of Switch. Choose ports on the canvas or in
**Connections**; saving and reopening preserves that choice. Invalid connections
from older definitions are flagged for reconnection before deployment.

**Deploy** starts the saved flow and schedules it for restoration when PiWorker
restarts. **Stop** ends execution and removes that restoration intent. The list
switch controls the same lifecycle; there is no separate flow-level “enabled”
setting. Stop a running flow before saving edits. Failed startup restoration is
shown as **Failed**.

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
only names. In compatible credential fields, autocomplete inserts
`{{secret.NAME}}`. Supported fields are Telegram's bot token, Notify's URL,
Database's DSN, and the username/password fields of Email, MQTT Publish and MQTT
Subscribe. These references resolve when the node is created: stop and redeploy
an existing flow after rotating a secret. HTTP Request headers do not currently
resolve secret placeholders. General templates and expressions cannot read the
secret store; missing credential references currently resolve to an empty string.

Secrets are stored in SQLite without application-level encryption at rest. They
are excluded from flow exports when referenced by name; literal credentials
entered directly in a node configuration remain part of that configuration.
Variables are ordinary data, visible in their manager and usable in payloads and
Debug; use the separate secret store for credentials.

### Environment Variables

```bash
export PIWORKER_ADDR=:8080
export PIWORKER_DB=/var/lib/piworker/data.db
export PIWORKER_DEBUG=true
export PIWORKER_AUTH=true
export PIWORKER_SESSION_SECURE=false # true when the browser connects via HTTPS
export PIWORKER_CORS_ORIGINS=http://localhost:3000,http://localhost:8080
export PIWORKER_ADMIN_USER=admin
export PIWORKER_ADMIN_PASS=...
```

### Sessions and account recovery

The browser uses an HttpOnly, SameSite=Strict cookie; session secrets are never
stored in JavaScript/localStorage. SQLite stores only token hashes. Sessions
survive restarts, expire after 7 days of inactivity or 30 days total, and are
limited to 20 per account. Signing out revokes the current session. Changing the
password from **Account** or recovering it invalidates every session for that
account, including live event streams (on the next event or heartbeat).

### HTTP and future HTTPS support

PiWorker currently starts an **HTTP server**. The UI displays a persistent warning
on HTTP pages, including setup, login and the editor. Passwords and sessions can
be intercepted even on a local network. Use a trusted network; this warning does
not encrypt the connection. It is hidden when the browser page uses HTTPS.

PiWorker does **not** currently provide a TLS listener, certificate generation,
automatic HTTP-to-HTTPS redirects, or a certificate installation assistant.
HTTPS requires an externally configured reverse proxy today. Set
`session_secure = true` (or `PIWORKER_SESSION_SECURE=true`) when the browser uses
that HTTPS endpoint. This setting only marks cookies `Secure`; it does not
configure TLS. Enabling it on a LAN HTTP endpoint prevents normal session use.
Forwarded headers do not automatically enable or downgrade cookie security.

A tested HTTPS deployment guide and assisted certificate setup are future work:
[Plan assisted HTTPS setup for local installations](https://github.com/Pegasus8/piworker/issues/287).
See [TODO.md](TODO.md) for the remaining scope.

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
Login/setup/recovery/password attempts are rate limited. Legacy Bearer JWTs are
no longer accepted; old JWT configuration values are ignored for compatibility.

### Webhooks

Deploy a flow with a **Webhook** trigger and external services can start it by
calling `http://<host>/api/webhooks/<path>` (or HTTPS through a configured proxy).
These endpoints use their own optional token authentication, independently of
the administrator session. Set a token on the node to
require `?token=...` or an `X-Webhook-Token` header.

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
The initial scope is one administrator per installation; existing accounts are
preserved during migration.

### Flows

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/flows` | List all flows |
| POST | `/api/flows` | Create a new flow |
| GET | `/api/flows/{id}` | Get a flow by ID |
| PUT | `/api/flows/{id}` | Update a flow |
| DELETE | `/api/flows/{id}` | Delete a flow |
| POST | `/api/flows/{id}/deploy` | Deploy (start) a flow |
| POST | `/api/flows/{id}/stop` | Stop a running flow |

### Node Types

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/node-types` | List available node types |

### Health

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | HTTP 200 when healthy; HTTP 503 when storage is closed |

---

## Tech Stack

### Backend
- **Go 1.27.1+** - High performance, great concurrency
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
├── internal/
│   ├── api/                # REST API handlers
│   ├── flow/               # Flow engine (runtime, router, models)
│   ├── node/               # Node system
│   │   ├── builtin/        # Built-in node implementations
│   │   └── ...
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
Review the binaries before publishing the draft. Publishing a release triggers
GHCR image publication for amd64, arm64, and ARMv7; stable releases also update
`latest`. Make the GHCR package public for anonymous pulls. The release tag must
point to a commit containing both workflows. Publish the draft manually in GitHub;
publishing it from another workflow with `GITHUB_TOKEN` does not trigger the image
workflow. See the [release checklist](docs/release-checklist.md) for the complete
sequence and GitHub event references. Creating or editing these files locally
does not publish anything.

Dependabot monitors Go modules, the Bun lockfile in `web/`, and GitHub Actions.

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Follow [AGENTS.md](AGENTS.md): write a failing behavior test, implement, then refactor
4. Run relevant checks: `make test` for Go, `make frontend-test frontend-test-e2e` for frontend behavior; use `make test-race` for concurrency changes
5. Commit: `git commit -m "Add my feature"`
6. Push: `git push origin feature/my-feature`
7. Open a Pull Request

---

## License

[AGPL-3.0](LICENSE.md)

> **Disclaimer**: I am not responsible for the misuse that may be given to this software. Use it at your own risk.
