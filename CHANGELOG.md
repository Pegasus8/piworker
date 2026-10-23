# Release notes

## Unreleased — revival

This is the pending first release of the revived implementation, not a published
version. The version number and release date have not been selected.

### Included

- Visual flow editor, node configuration and playground, manual execution,
  deployment/restoration on restart, execution history and live debugging.
- Persistent global and flow variables, plus a separate named credential store.
- One-use administrator setup and recovery codes, server-side cookie sessions
  and account password changes.
- Embedded frontend in a single Go binary, Linux AMD64/ARM64/ARMv7 archives and
  a multi-platform Docker build/publication workflow.
- `piworkerctl install`, `update` and `uninstall` for Linux/systemd. Updates verify
  checksums and back up data; uninstall retains accounts, flows and configuration.
- `piworker -version` reports the build version without starting the server.
- Health checks return HTTP 503 when storage is closed. Resizing the flow editor
  keeps nodes reachable on smaller screens.

### Security correction

- Command-node payload/topic/metadata values are passed as literal arguments,
  including in quoted placeholders; data is never recursively expanded into code.
  Unsupported shell expansion contexts fail validation before execution.
- Command output reports the configured template instead of inlining message values.
  Positional shell arguments are reserved for bindings; templates using set/shift
  or explicit eval/interpreter re-execution must be reviewed.

### Compatibility and limits

- Native releases require systemd and glibc 2.35+ for the managed installer.
  Standalone binaries do not require systemd. ARMv6 and native macOS/Windows
  release binaries are not provided. Docker is the alternative on those desktop OSes.
- The old `v0.1.0-alpha.1` release from 2020 is not compatible with the installer.
  Its database migration has not been validated. Test with a copy and retain the
  original binary and data; no automatic migration guarantee is made.
- Existing accounts from the revival session-authentication migration retain
  their password hashes. Legacy JWT sessions must sign in again; Bearer JWT API
  authentication is no longer supported.
- The server listens over HTTP. HTTPS needs an external reverse proxy;
  `session_secure` only controls the cookie's Secure flag.
- Secrets have no application-level encryption at rest. Flow exports do not
  include the separate variable or secret stores. Credentials entered literally
  in a node can appear in its exported configuration.
- Updates interrupt flows. Restoring local data cannot undo commands or requests
  already sent to devices and external services.

See the [README](README.md) for setup, the [operations guide](docs/operations.md)
for recovery, and [TODO](TODO.md) for explicitly deferred features.
