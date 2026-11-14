# ADR-0032: Automatic backups and whole-state restore

**Status:** Accepted
**Date:** 2026-11-13
**Deciders:** maintainer
**Design:** [docs/design/backups-and-updates.md](../design/backups-and-updates.md)

## Context

Issue #128 asks for automatic backups. Today the only copies are the ones
`piworkerctl update` makes before an update (never removed) and the
partial stores export of ADR-0031. The usual failure of a Raspberry Pi is
its SD card, and the usual mistake of a user is breaking or deleting
something; neither has a recovery path short of a manual copy of the data
directory with the service stopped. ADR-0030 asks that copies leaving the
device be encrypted.

## Options considered

### Option A: incremental, deduplicated backups (restic/borg style)

| Dimension | Assessment |
|---|---|
| Complexity | High: chunk store, index, pruning |
| Security | Same as B once encrypted |
| Pi resources | Lowest transfer for large data, but PiWorker's state is a few MB |
| Maintenance | A repository format to keep compatible; a broken chunk loses every later backup |

### Option B: full snapshots with retention

| Dimension | Assessment |
|---|---|
| Complexity | Medium: `VACUUM INTO`, a tar.gz with a manifest, a scheduler, retention |
| Security | Local copies as private as the database; remote copies age-encrypted with a stored passphrase |
| Pi resources | Seconds and a few MB per day without history |
| Maintenance | One versioned archive format; every file restores on its own |

### Option C: leave it to the OS (cron + rsync, documented)

**Cons:** no consistent SQLite snapshot without stopping the service, no
status in the editor, no restore path for a fresh install, nothing for
Docker users.

## Decision

Option B, in the server (so it works for managed, Docker and manual
installations alike):

- **Content:** `manifest.json`, a `VACUUM INTO` snapshot of the database
  without history (`flow_runs`, `node_events`, `mcp_audit`,
  `mcp_rejections`, `auth_sessions`, `auth_codes` emptied in the copy),
  `tls/`, and the configuration file when there is one.
- **Schedule:** daily at a configurable time with a random delay, skipped
  when the content did not change; "back up now"; a backup before every
  restore.
- **Retention:** per destination, newest 7 daily, 4 weekly, 6 monthly by
  default; only archives PiWorker wrote are deleted.
- **Destinations:** a local directory (default `backups/` next to the
  database, typically moved to a USB drive) and S3-compatible storage. No
  other protocol for now.
- **Encryption:** local copies are not encrypted (ADR-0030). Remote copies
  are always age-encrypted with a passphrase (scrypt) the admin sets once;
  it is stored in the backup settings so backups run unattended, never
  returned by the API and not usable from flows.
- **Restore** restores the whole state as it was, running flows included,
  and revokes every session. It is validated completely, staged next to the
  database, and applied at the next startup before the database is opened
  (the server exits with a dedicated code to be restarted). Entry points:
  the editor (password step-up), the first-run setup page (setup code), and
  `piworker -restore` / `piworkerctl restore` with the service stopped. A
  backup from a newer schema than the binary is refused. The editor restore
  does not write the configuration file; `piworkerctl restore` does.
- **Dependencies:** `filippo.io/age` (encryption) and an S3 client
  (`github.com/minio/minio-go/v7`).
- `piworkerctl update` keeps its stopped full copy as the rollback, now
  limited to the newest 3.
- **Downloads** are age-encrypted with a passphrase chosen at download
  time, need the account password and are refused while authentication is
  disabled (as ADR-0031's export with secret values). **Uploads** to
  restore travel as base64 inside JSON (the request guard admits only JSON;
  up to 64 MiB). The setup-page restore checks the setup code without
  consuming it and uses it up only once the backup was accepted.
- `/api/health` answers the process's random instance, so the editor can
  tell that PiWorker restarted after a restore however quick the restart.
- MCP exposes none of it.

## Consequences

- Easier: recovering from a dead SD card or a mistake; moving to new
  hardware with devices still trusting the CA.
- Harder: a lost passphrase makes remote copies unreadable (by design);
  backups do not contain run history or the agent audit; a restore needs a
  restart and briefly interrupts flows; restored running flows act at once.
- Revisit when: users ask for another destination (WebDAV, SFTP), or the
  state grows enough that full snapshots cost real transfer.

## Action items

1. [x] Snapshot, archive, local destination, schedule, retention, Backups page.
2. [x] Staged restore: editor, `-restore`, `piworkerctl restore`.
3. [x] Restore from the first-run setup page (and from an uploaded file in the editor; encrypted downloads).
4. [x] S3-compatible destination with age encryption.
5. [x] Retention for `piworkerctl update` copies.
6. [x] README, operations guide and user guide.
