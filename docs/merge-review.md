# Revival → master review — 2026-10-23

Baseline: master...83534cf, 69 commits, 374 changed paths. The change replaces the
old core/webui implementation with internal/ and web/, plus native lifecycle and
release tooling. This review covers the full PR scope by area; generated bundles
and removed legacy files were assessed as replacements, not individually audited
as new application logic. It is not a guarantee that every defect was found.

## Open blocking finding

**[P1] Quoted command placeholders permit shell substitution.**
`internal/node/builtin/action_command.go`, `expandVariables` and `shellEscape`:
values are wrapped in single quotes without considering the surrounding command
syntax. For a saved command such as `printf '%s\n' "{{payload}}"`, a payload
`$(printf PIWORKER_REVIEW)` produces a double-quoted shell word containing
`'$(printf PIWORKER_REVIEW)'`. The single quote characters are literal inside the
double quotes, so command substitution still executes. A harmless local shell
reproduction returned `'PIWORKER_REVIEW'`, rather than the literal payload.

Impact: a flow author can reasonably quote a placeholder expecting literal data;
untrusted webhook/MQTT inputs reaching that command can then execute commands as
the service account. Existing unquoted-placeholder escaping tests do not establish
safety in quoted contexts. Before merge, add a failing regression covering actual
execution with quoted payload/topic/meta placeholders and implement a safe binding
or validation strategy, including quote/backtick/command-substitution contexts.
The finding is deliberately open; this PR must remain a draft until resolved.

## Areas reviewed

- Startup/configuration precedence, first account bootstrap, session middleware,
  CSRF/origin checks, rate limits, hashed session tokens, expiry and recovery.
- API route access, flow edit/deploy/stop/delete coordination, definition validation,
  persistent variables/secrets, single SQLite connection and transaction behavior.
- Runtime routing, cancellation, per-node queues/concurrency, observer/event hub
  locking and stream/session lifecycle; existing race/regression suite is a gate.
- Nodes with host/network effects (command/file/HTTP/webhook); these intentionally
  run with the service account's capabilities. Webhooks are public unless their
  optional token is configured. They are not an administrative API sandbox.
- Frontend cookie-based session restoration, API failures, editor state, embedded
  assets and Markdown docs. Markdown currently originates in compiled node
  definitions; adding externally supplied documentation would require sanitizing
  the marked/v-html path before treating it as untrusted input.
- Installer checksum/preflight, stopped-data backups, rollback, retained data and
  service ownership; package-based and real Pi results are recorded separately.
- CI/release/Docker workflows, artifact naming, version injection and Dependabot
  paths. Tag publication remains a separate operation from merging this PR.

## Compatibility and remaining gates

- Legacy 2020 databases/flow definitions are not promised automatic migration;
  keep the changelog notice and original data backups.
- HTTP transport and plaintext-at-rest secret storage remain documented limits.
- Refreshed dependency audit has one unresolved braces development-tool advisory;
  see dependency-audit.md. Old default-branch alert totals do not describe revival.
- Require green CI on the PR head, including Go race tests, browser tests, all
  Linux archives and the Docker multi-platform build. A successful older SHA is
  not evidence for this dependency refresh.
- Pi hardware/browser followups and public release/GHCR download checks remain
  release validation work; real local-package installation/reboot already passed.

No merge, tag, release publication or default-branch rename is authorized here.

## Validation of the refresh

- `go test -race ./...` and `go vet ./...`: passed with Go 1.27.3/CGO.
- Frontend unit tests: 5 passed, 20 assertions; typecheck/production build passed.
- Playwright: all 34 passed, including live backend setup/recovery and flow tests.
  An initial attempt failed at the Xcode Git shim; rerunning with the installed
  Command Line Tools Git/compiler/SDK succeeded without changing tests.
- Embedded UI regenerated from the updated dependency lockfile.
- Go dependency analysis: 0 reachable/imported-package findings; frontend retains
  the explicitly documented braces development-tool advisory.
- Remote CI results must be read against the latest pushed SHA. This document's
  local evidence does not certify a pending remote multi-platform Docker build.
