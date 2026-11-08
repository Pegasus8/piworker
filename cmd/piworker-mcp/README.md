# piworker-mcp — Claude Desktop bridge

`piworker-mcp` lets an MCP client that only speaks **stdio**, such as Claude
Desktop, use PiWorker's `/mcp` endpoint. Claude Desktop starts it as a local
MCP server; it relays every JSON-RPC message to PiWorker over HTTPS with your
agent token and writes PiWorker's answers back. Tools, prompts and anything
else PiWorker publishes come through unchanged.

Clients that support remote HTTP MCP servers with custom headers, such as
Claude Code, do not need it: they connect to `https://<pi>:8443/mcp` directly
(see `docs/design/mcp.md` §12).

It is a single static binary (pure Go, no C library) for macOS, Windows and
Linux. Design: [`docs/design/mcp.md`](../../docs/design/mcp.md) §12.

## 1. Before you start

On PiWorker:

1. Enable MCP: `enabled = true` in the `[mcp]` section of `piworker.toml`,
   then restart PiWorker.
2. In the editor, open **Agents**, create a token (it is shown once) and grant
   it the tools it may call.
3. Note the CA fingerprint under **Settings → Trust this device**
   (`SHA256:AB:CD:…`). It is also printed when PiWorker starts.

The editor's new-token dialog shows the commands and configuration below,
already filled in for your PiWorker.

## 2. Install the bridge

Download the binary for your computer from the release (or build it, see
below) and check it against `SHA256SUMS`:

| Computer | File |
|---|---|
| Mac with Apple silicon | `piworker-mcp-darwin-arm64` |
| Mac with Intel | `piworker-mcp-darwin-amd64` |
| Windows (x64) | `piworker-mcp-windows-amd64.exe` |
| Linux x86-64 / ARM64 / ARMv7 | `piworker-mcp-linux-amd64` / `-linux-arm64` / `-linux-armv7` |

**macOS / Linux**

```bash
f=piworker-mcp-darwin-arm64        # the file you downloaded
grep "$f\$" SHA256SUMS | shasum -a 256 -c -   # Linux: sha256sum -c -
# macOS only: the binary is not notarized, so remove the download quarantine.
xattr -d com.apple.quarantine "$f" 2>/dev/null || true
sudo mkdir -p /usr/local/bin
sudo install -m 755 "$f" /usr/local/bin/piworker-mcp
/usr/local/bin/piworker-mcp --version
```

**Windows (PowerShell)**

```powershell
(Get-FileHash .\piworker-mcp-windows-amd64.exe -Algorithm SHA256).Hash   # compare with SHA256SUMS
$dir = "$env:LOCALAPPDATA\Programs\piworker-mcp"
New-Item -ItemType Directory -Force $dir | Out-Null
Move-Item .\piworker-mcp-windows-amd64.exe "$dir\piworker-mcp.exe"
& "$dir\piworker-mcp.exe" --version
```

## 3. Save the token in a private file

The token is never given on the command line (other users can read process
arguments). It is read from a file that only you can read.

**macOS / Linux** — the command the editor shows:

```bash
(umask 077 && printf '%s\n' 'pwk_…' > ~/.piworker-token)
```

The bridge refuses a token file that group or others can access, and tells
you to run `chmod 600 <file>`.

**Windows (PowerShell)** — write it as plain text and remove everyone else's
access:

```powershell
$token = "$env:USERPROFILE\.piworker-token"
Set-Content -Path $token -Value 'pwk_…' -Encoding ascii
icacls $token /inheritance:r /grant:r "${env:USERNAME}:(R,W)"
icacls $token   # should list only you
```

`/inheritance:r` drops the permissions inherited from your profile folder and
`/grant:r` leaves read and write for you alone. The bridge refuses a token file
that accounts other than you, SYSTEM and Administrators can read or change
(for example `Users` or `Everyone`, typical outside your profile folder), or
that another user owns, and shows this `icacls` command. Do not use `Out-File`
or `>` in Windows PowerShell 5.1: they write UTF-16, which the bridge refuses.

## 4. Configure Claude Desktop

Open Claude Desktop's **Settings → Developer → Edit Config**, or the file
directly:

| OS | `claude_desktop_config.json` |
|---|---|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |
| Linux (community builds) | `~/.config/Claude/claude_desktop_config.json` |

Add PiWorker under `mcpServers` (keep any servers already there) and restart
Claude Desktop. Use the **full path** of the binary: Claude Desktop does not
start servers through your shell, so your `PATH` may not apply. `--token-file`
may start with `~`, which the bridge expands to your home directory.

**macOS**

```json
{
  "mcpServers": {
    "piworker": {
      "command": "/usr/local/bin/piworker-mcp",
      "args": [
        "--url", "https://raspberrypi.local:8443/mcp",
        "--token-file", "/Users/you/.piworker-token",
        "--ca-fingerprint", "SHA256:AB:CD:…"
      ]
    }
  }
}
```

**Linux**

```json
{
  "mcpServers": {
    "piworker": {
      "command": "/usr/local/bin/piworker-mcp",
      "args": [
        "--url", "https://raspberrypi.local:8443/mcp",
        "--token-file", "/home/you/.piworker-token",
        "--ca-fingerprint", "SHA256:AB:CD:…"
      ]
    }
  }
}
```

**Windows** (backslashes doubled in JSON)

```json
{
  "mcpServers": {
    "piworker": {
      "command": "C:\\Users\\you\\AppData\\Local\\Programs\\piworker-mcp\\piworker-mcp.exe",
      "args": [
        "--url", "https://raspberrypi.local:8443/mcp",
        "--token-file", "C:\\Users\\you\\.piworker-token",
        "--ca-fingerprint", "SHA256:AB:CD:…"
      ]
    }
  }
}
```

The editor's snippet has the same arguments, with `piworker-mcp` as the
command and `/path/to/piworker-token` as the token file: replace both with full
paths. If `raspberrypi.local` does not resolve on your computer, use the Pi's
IP address; PiWorker's certificate covers its interface addresses.

## Flags

| Flag | Meaning |
|---|---|
| `--url URL` | PiWorker's MCP endpoint, `https://HOST:8443/mcp`. Required. Plain `http://` is only accepted to `127.0.0.1` or `[::1]` (PiWorker on the same computer with `tls.mode = "off"`); credentials, queries and fragments are refused. |
| `--token-file PATH` | File holding the agent token (`pwk_…`). Required. Whitespace and a UTF-8 BOM are ignored. On macOS and Linux it must not be accessible to group or others; on Windows only you, SYSTEM and Administrators may read or change it. |
| `--ca-fingerprint FP` | Trust only the CA with this SHA-256 fingerprint. Accepts `SHA256:AB:CD:…` as PiWorker shows it, in any case, with or without the prefix and colons. |
| `--ca-file PATH` | Trust only the CA certificate(s) in this PEM or DER file, e.g. the `piworker-ca.crt` from **Trust this device**, or your own CA with `tls.mode = "custom"`. With `--ca-fingerprint` too, only the certificate with that fingerprint. |
| `--timeout DURATION` | Give up on a request after this long (default `2m0s`). |
| `--version`, `--help` | Print the version or the help and exit. |

Without `--ca-fingerprint` and `--ca-file` the system's trusted CAs are used
(for a certificate from a public CA, e.g. behind a reverse proxy).

A token pasted into any flag (a value with `pwk_` followed by a token id, such
as the token itself where the token file's path should be) is refused before
anything else, with a message that does not repeat it.

## How the server is verified

Every connection that carries the token is verified for the host name in
`--url` against **one** trust anchor: the pinned CA, or the system CAs when
nothing is pinned. A pinned CA replaces the system CAs; it is never added to
them.

With `--ca-fingerprint` alone the bridge first needs the CA certificate itself.
PiWorker presents only its server certificate, so before the first request the
bridge looks for the CA in the certificate chain the server presents and, when
it is not there, downloads PiWorker's public `/piworker-ca.crt` from the same
host and port. Neither step sends the token or trusts the server: only a
certificate whose SHA-256 fingerprint equals the pin is used. When the server
presents a root certificate that is not the pinned CA, the bridge stops before
sending any request. Then every request is verified normally against that CA.

So a device impersonating your Pi on the network (mDNS or ARP spoofing) gets
neither the token nor your agent's requests, and cannot feed the agent tool
descriptions or results.

## Behaviour

- **stdout carries only MCP messages**, one JSON-RPC message per line.
  Diagnostics go to stderr, which Claude Desktop keeps in its MCP log
  (`~/Library/Logs/Claude/mcp-server-piworker.log` on macOS,
  `%APPDATA%\Claude\logs\mcp-server-piworker.log` on Windows). The token and
  request arguments are never logged.
- **Protocol versions.** The bridge is a relay: it does not negotiate. It sends
  `MCP-Protocol-Version` from the `initialize` answer, or from each request's
  `_meta` (2026-07-28 and later, which also get `Mcp-Method` and `Mcp-Name`),
  and reads JSON and event-stream answers alike. It does not hold a session
  (PiWorker's endpoint is stateless) and does not open the optional GET stream.
- **Concurrency.** Up to 8 requests are sent at once and 64 may wait; answers
  are written as they arrive, each with its request's id. Beyond 64 waiting,
  requests get an error at once.
- **Cancellation.** `notifications/cancelled` aborts the matching HTTP request
  and drops its answer. It is not forwarded: a stateless server could not
  match it. With protocol version 2026-07-28 or later, aborting the request
  cancels the call on PiWorker. With earlier versions (Claude Desktop
  negotiates a 2025 version today) the call keeps running on PiWorker until
  it ends or reaches its own timeout, and only its answer is lost: a tool call
  you cancel may still have its effects.
- **No retries.** A tool call may have side effects, so nothing is ever sent
  twice. Redirects are not followed and HTTP proxies are not used (PiWorker is
  on your local network).
- **Limits.** Messages from the client up to 4 MiB, answers up to 16 MiB.
  JSON-RPC batches are refused.
- **Exit.** When the client closes stdin, requests still running get 10 s to
  finish, then the bridge exits with status 0. Invalid flags exit with 2,
  a broken stdout with 1.

## Errors

Failures come back to the client as JSON-RPC errors with code `-32000` and,
for HTTP errors, `data.httpStatus`:

| Situation | What the bridge says |
|---|---|
| HTTP 401 | The token is wrong, expired or revoked: create a new one under Agents. |
| HTTP 403 | PiWorker refused the request (its reason is quoted): check the token's scopes and grants, and that the `--url` host is in `allowed_hosts`. |
| HTTP 404 | No MCP endpoint at `--url`: check the URL and `[mcp] enabled = true`. |
| HTTP 429 | The token's rate limit was reached; `data.retryAfter` holds the seconds from `Retry-After`. |
| HTTP 5xx | PiWorker failed to handle the request. |
| Wrong fingerprint | PiWorker's CA does not match `--ca-fingerprint` (both fingerprints shown); the token was not sent. |
| Wrong host name | The certificate is not valid for the `--url` host (the names it covers are listed); add the name to `tls.hosts`. |
| Untrusted certificate | Pass `--ca-fingerprint` or `--ca-file`. |
| Timeout / unreachable | PiWorker did not answer within `--timeout` / cannot be reached at host:port. |

A JSON-RPC error PiWorker's MCP layer sends with another HTTP status (for
example during protocol version negotiation) is passed through unchanged.

## Build and test

```bash
make build-mcp-bridge       # build/piworker-mcp for this computer
make build-mcp-bridge-all   # build/piworker-mcp-{linux-amd64,linux-arm64,linux-armv7,darwin-amd64,darwin-arm64,windows-amd64.exe}
make test-mcp-bridge        # go test -v ./cmd/piworker-mcp/...
```

Builds use `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.version=…"`.
The bridge must stay pure Go: `TestBridgeIsPureGo` fails if it imports
anything that needs cgo, such as PiWorker's storage. Its tests run a real
go-sdk MCP server (stateless, JSON and event-stream answers, protocol versions
2025-06-18, 2025-11-25 and 2026-07-28) behind a bearer check over TLS, and a
real go-sdk client over the stdio transport.
