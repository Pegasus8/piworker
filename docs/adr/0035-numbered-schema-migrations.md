# ADR-0035: Numbered schema migrations recorded in the database

**Status:** Proposed
**Date:** 2026-11-15
**Deciders:** maintainer

## Context

The schema was built at every start from `CREATE TABLE IF NOT EXISTS`
statements and "does this column exist?" checks spread over eight files. The
database did not say which schema it had, so a binary could not tell a
database it understands from one written by a newer release; the backup
manifest carried a hand-maintained `SchemaVersion` constant that nothing tied
to the tables (ADR-0032). Every future change would add another conditional
that runs forever. Installations update in place on an SD card and restore
backups across versions, so both directions matter.

## Options considered

### Option A: Keep idempotent checks

**Pros:** no mechanism. **Cons:** cannot express a change that is not
"add if missing" (a rewrite of rows, a dropped column); cannot refuse a newer
database; cost grows with every release.

### Option B: A migration library (golang-migrate, goose)

**Pros:** known tool, down-migrations. **Cons:** a dependency and SQL files
to embed for something that needs forty lines; down-migrations are not
wanted (going back is restoring a backup).

### Option C: An ordered list in code and `PRAGMA user_version`

| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | A newer database is refused instead of being half-read |
| Pi resources | One PRAGMA read at start; no per-start schema work |
| Maintenance | One append per schema change |

## Decision

`internal/storage/migrations.go` holds `migrations`, an ordered list; the
position of a migration (from 1) is the schema version it produces and
`SchemaVersion` is their number. Opening a store reads `PRAGMA
user_version`, applies each pending migration in its own transaction
together with the new version, and refuses a database with a higher version
(`ErrSchemaTooNew`) without touching it.

Migration 1, the baseline, is every table as of this release, accounts
included. It stays idempotent, because it is also how databases written
before versions existed (version 0) are upgraded. Later migrations are
appended; a released migration is never edited or reordered. There are no
down-migrations: going back is restoring a backup made before the update.

A backup's manifest records the `user_version` of its database, and restore
validation reads the database's own version, so a backup from a newer
release is refused before anything is staged.

## Consequences

- Easier: schema changes that rewrite data; knowing what a database is.
- Harder: downgrading the binary after an update that added a migration now
  fails at start with a clear error instead of running against an unknown
  schema (restore the pre-update backup).
- Revisit when: a migration is too slow for a Pi's start timeout.

## Action items

1. [x] `migrate`, the baseline, `ErrSchemaTooNew`.
2. [x] Backups take and check the database's version.
