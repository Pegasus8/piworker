# ADR-0033: Update checks and requested updates

**Status:** Proposed
**Date:** 2026-11-13
**Deciders:** maintainer
**Design:** [docs/design/backups-and-updates.md](../design/backups-and-updates.md)

## Context

Issue #129 asks for automatic updates. `piworkerctl update` already
updates a managed installation safely (backup, health check, rollback), but
only when someone runs it as root. The server runs as the unprivileged
`piworker` user and cannot replace its binary or restart its service.
Releases are checked against `SHA256SUMS` from the same release, which
catches a damaged download but trusts whoever can publish a release.

## Options considered

### Option A: the server updates itself (AdGuard Home style)

**Cons:** the service user must be able to write its own binary, so a
compromised process can persist; the rollback logic would be written a
second time in Go; it does not apply to Docker.

### Option B: a root timer runs `piworkerctl update` on its own schedule

**Cons:** policy and status live outside the editor; the admin cannot see
or trigger it from the browser.

### Option C: the server decides when, root executes a narrow request

| Dimension | Assessment |
|---|---|
| Complexity | Medium: a request file, a systemd path unit, a result file, a policy |
| Security | The server can only ask for "the latest stable release, not older"; the manager validates everything |
| Pi resources | One HTTPS request a day |
| Maintenance | Reuses the tested update and rollback of `piworkerctl` |

## Decision

Option C, without release signatures for now:

- **Update check** in every installation: once a day, the latest stable
  release from GitHub; the editor shows it. `[updates] check = false`
  disables it.
- **No signatures (yet):** a release is trusted as it is today: downloaded
  over HTTPS from the repository's releases and checked against its
  `SHA256SUMS`. A manual signing step per release was judged too much
  friction for now; the limits below (opt-in, stable only, a waiting
  period) reduce the exposure instead.
- **Requests:** the editor's "Update now" or the automatic policy writes
  `update-request` in the data directory; `piworker-update.path` (installed
  by `piworkerctl install`) runs `piworkerctl update --requested` as root,
  which accepts only the latest stable release not older than the
  installed one, and writes `update-result` for the editor.
- **Policy:** `off` by default (check only); `auto` installs a release that
  is stable, at least 3 days old and, while 0.x, in the same minor
  version, in a nightly window. A rolled-back version is skipped until a
  newer one exists.
- Docker and manual installations get the notice and the command only.

## Consequences

- Easier: staying current without a shell; updates visible in the editor.
- Harder: flows are interrupted briefly by automatic updates. Whoever
  controls the repository's releases (a compromised maintainer account)
  can reach installations with `auto` after the waiting period.
- Revisit when: signing can be automated without a manual step per
  release, or PiWorker has enough users for that exposure to matter; a
  release channel other than stable is wanted; Docker users ask for in-app
  updates.

## Action items

1. [ ] Update check and notice.
2. [ ] Request/result files, path unit, policy and Settings page.
3. [ ] README, operations guide and user guide.
