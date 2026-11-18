# ADR-0031: Export and restore of variables, connections and secrets

**Status:** Accepted
**Date:** 2026-11-13
**Deciders:** maintainer
**Design:** [docs/design/secrets-at-rest.md](../design/secrets-at-rest.md) (why the file is encrypted)

## Context

A flow export names secrets and broker connections but carries none of
their values, and no export carries the global or flow variables. Moving an
installation to a new Pi, or reinstalling, meant typing every variable,
connection and secret again. Secrets are write-only over the API: nothing
lets a signed-in session read their values today. ADR-0030 decided the
database is not encrypted; a file of secrets that can be copied anywhere
must be.

## Options considered

### Option A: document copying the database

**Pros:** nothing to build. **Cons:** all or nothing (flows, accounts,
audit included), only between compatible versions, and the copy is the
plaintext database.

### Option B: a stores file with secret values sealed by a passphrase

| Dimension | Assessment |
|---|---|
| Complexity | Medium: a package, two routes, a dialog |
| Security | Secret values leave only sealed (scrypt + AES-256-GCM) and only after the account password; the readable part has no values or passwords |
| Pi resources | One scrypt derivation (32 MiB, under a second on a Pi 3) per export or import with secrets |
| Maintenance | A versioned file format |

### Option C: the same file with secret values in plaintext

**Cons:** the first API to read every secret in one request, into a file
that ends up in downloads folders and cloud drives.

## Decision

Option B.

- **Format** (`internal/storesbackup`, `format: "piworker-stores"`,
  `version: 1`): readable `variables` (global), `flowVariables` (flow ID,
  flow name, variables of each flow that has any), `connections` (without
  passwords) and `secrets` (names). With a passphrase, `sealed` holds the
  secret values *and the connections with their passwords* (so editing the
  readable part cannot send a password to another broker), and the readable
  `connections` list is empty. The passphrase has at least 12 characters;
  scrypt N=2^15, r=8, p=1, a random salt and nonce, AES-256-GCM with the
  format as associated data. A file asking for heavier scrypt parameters
  than about twice what PiWorker writes (beyond N=2^16, r=8, p=1: 64 MiB,
  for a 1 GiB Pi 3) is refused.
- **Export:** `POST /api/stores/export {passphrase?, password?}` answers the
  file as an attachment. With a passphrase it requires the account password
  (the existing step-up, 403 when wrong) and is refused while
  authentication is disabled; without one it carries no secret value or
  password. Logged (whether values were included and how many secrets).
- **Import:** `POST /api/stores/import {bundle, passphrase?, overwrite?}`
  (up to 8 MiB). Everything is validated before anything is written; a wrong
  passphrase or another kind of file is a 400. Order: secrets, connections,
  global variables, flow variables (matched by ID, else by a flow name only
  one flow has; others are reported). Existing values are kept unless
  `overwrite`; one identical to the file's is kept and restarts nothing. A
  connection from a file without passwords keeps the
  password it already has here, or is created without one and reported.
  The connections it adds or replaces restart the running flows that use
  them (ADR-0029). The answer is counts per kind plus what was not restored;
  it never echoes a value. No step-up: writing secrets needs none today
  (`PUT /api/secrets/{name}`).
- MCP exposes none of it.

## Consequences

- Easier: moving or rebuilding an installation; keeping a copy of the
  configuration that a flow export lacks.
- Harder: a lost passphrase makes the sealed part unreadable (by design).
  The file's readable part (variables, connection hosts) is not
  authenticated: it is data the admin chooses to import.
- Revisit when: automatic backups (issue #128) should reuse this format, or
  accounts and agent tokens should travel too.

## Action items

1. [x] `internal/storesbackup` (format, seal, restore).
2. [x] REST routes with step-up; editor dialog "Export and restore".
3. [x] Documentation: README, user guide.
