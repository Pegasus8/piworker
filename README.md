# PiWorker

[![GitHub license](https://img.shields.io/github/license/Pegasus8/piworker)](https://github.com/Pegasus8/piworker/blob/master/LICENSE.md)

A **visual flow-based automation system** for Raspberry Pi and other devices. Create automation workflows by connecting nodes in a visual editor, similar to Node-RED but built with Go for performance and Vue 3 for a modern UI.

## Features

- **Visual Flow Editor** - Drag-and-drop interface to create automation workflows
- **Node-Based** - Connect triggers, processing nodes, and actions
- **Privacy-First** - Everything runs locally on your device
- **Extensible** - Easy to add new node types
- **Lightweight** - Single binary with embedded frontend, optimized for Raspberry Pi
- **Fast** - Go backend with concurrent message processing

## Quick Start

### First boot: create the administrator

Before starting a binary or container with a new database, export:

```bash
export PIWORKER_ADMIN_USER=admin
export PIWORKER_ADMIN_PASS='replace-with-a-unique-password'
```

Use a unique password of at least 8 characters. These credentials create the
first user only; they do not reset existing accounts. Sign in with them when
opening the editor. You can unset the variables after the first successful
start. Docker Compose also reads them from a local, git-ignored `.env` file.
Authentication stays enabled by default; without credentials a fresh installation
exits with an explanation instead of starting an unprotected server.

Pre-built images and binaries below apply to the published revival release.
Until it is published, build this branch from source or use `make docker-build`.

### Option 1: Docker (Recommended)

```bash
# Production - single container with everything included
docker run -d \
  --name piworker \
  -p 8080:8080 \
  -v piworker-data:/app/data \
  -e PIWORKER_ADMIN_USER -e PIWORKER_ADMIN_PASS \
  ghcr.io/pegasus8/piworker:latest

# Open http://localhost:8080
```

### Option 2: Pre-built Binary

Download the latest release from [Releases](https://github.com/Pegasus8/piworker/releases) and run:

```bash
./piworker
# Open http://localhost:8080
```

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
| **Go** | 1.25+ | Backend compilation | [go.dev/dl](https://go.dev/dl/) |
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
go version    # go1.25+
bun --version # 1.3.14
```

A C compiler is required: SQLite uses CGO. Install Xcode Command Line Tools
on macOS or `build-essential` on Debian/Ubuntu. Bun is needed only to build the
frontend; release binaries include it already.

#### Quick Install (Linux/Raspberry Pi)

```bash
# Install Go
wget https://go.dev/dl/go1.25.0.linux-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.25.0.linux-arm64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Install Bun
curl -fsSL https://bun.sh/install | bash

# Verify
go version && bun --version
```

### Local Development (Recommended)

Run backend and frontend separately for hot-reload:

**Terminal 1 - Backend:**
```bash
go run main.go -debug -auth=false
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
# On the Pi, set PIWORKER_ADMIN_USER and PIWORKER_ADMIN_PASS before first boot.
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

### Systemd Service

Bootstrap the administrator once by running the binary as `pi` with the
credentials above and `-db /home/pi/piworker.db`, then stop it. The service reuses
that database without needing bootstrap credentials.

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
jwt_secret = ""        # empty: a secret is generated and persisted across restarts
cors_origins = ["http://localhost:3000", "http://localhost:8080"]
```

### Command Line Flags

```bash
./piworker [flags]

Flags:
  -config string        Path to TOML config file (default: piworker.toml if present)
  -addr string          HTTP server address (default ":8080")
  -db string            SQLite database path (default "piworker.db")
  -debug                Enable debug logging
  -auth                 Enable JWT authentication (default true)
  -jwt-secret string    JWT secret (auto-generated and persisted if empty)
  -cors-origins string  Comma-separated allowed CORS origins
  -admin-user string    Bootstrap admin username (or env PIWORKER_ADMIN_USER)
  -admin-pass string    Bootstrap admin password (or env PIWORKER_ADMIN_PASS)
```

### Environment Variables

```bash
export PIWORKER_ADDR=:8080
export PIWORKER_DB=/var/lib/piworker/data.db
export PIWORKER_DEBUG=true
export PIWORKER_AUTH=true
export PIWORKER_JWT_SECRET=...        # optional; otherwise auto-generated + persisted
export PIWORKER_CORS_ORIGINS=https://app.example.com
export PIWORKER_ADMIN_USER=admin
export PIWORKER_ADMIN_PASS=...
```

### Webhooks

Deploy a flow with a **Webhook** trigger and external services can start it by
calling `http(s)://<host>/api/webhooks/<path>`. Set a token on the node to
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
| GET | `/api/health` | Health check |

---

## Tech Stack

### Backend
- **Go 1.25+** - High performance, great concurrency
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

CI runs on pull requests and pushes to `master`, `main`, and `revival`. It checks
Go formatting, `go vet`, race-enabled tests, frontend typechecking/build, Linux
CGO builds for amd64/arm64/ARMv7, and multi-platform Docker builds. Linux archives
and SHA256 checksums are available as workflow artifacts.

Pushing a `v*` tag runs the same checks and prepares a **draft GitHub release**.
Review the binaries before publishing the draft. Publishing a release triggers
GHCR image publication for amd64, arm64, and ARMv7; stable releases also update
`latest`. Make the GHCR package public for anonymous pulls. Install these
workflows on the default branch before the first release. Creating or editing
these files locally does not publish anything.

Dependabot monitors Go modules, the Bun lockfile in `web/`, and GitHub Actions.

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes
4. Run tests: `make test`
5. Commit: `git commit -m "Add my feature"`
6. Push: `git push origin feature/my-feature`
7. Open a Pull Request

---

## License

[AGPL-3.0](LICENSE.md)

> **Disclaimer**: I am not responsible for the misuse that may be given to this software. Use it at your own risk.
