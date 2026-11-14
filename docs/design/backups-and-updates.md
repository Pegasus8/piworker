# Automatic backups, restore and automatic updates

Status: proposed in [ADR-0032](../adr/0032-automatic-backups-and-whole-state-restore.md)
(backups and restore) and [ADR-0033](../adr/0033-update-checks-and-requested-updates.md)
(update checks and automatic updates). Issues #128 and #129.

## Where we start

- `piworkerctl update` (bash, root) stops the service, copies the whole data
  directory, the binary and `/etc/piworker` to `/var/backups/piworker/<date>`,
  installs the new binary, checks health and rolls back on failure. Those
  copies are never removed: with unattended updates they would fill the SD
  card. The release is checked against `SHA256SUMS` from the same release,
  which catches a broken download but not a release someone else published.
- The stores export (ADR-0031) moves variables, connections and secrets
  between installations. It is not a backup: flows, accounts, versions and
  the CA stay behind.
- The server runs as the unprivileged `piworker` user. It cannot replace its
  own binary or restart its service, and it should stay that way.

## What users need

| Situation | What they expect |
|---|---|
| "I broke a flow / deleted something yesterday" | Pick yesterday's backup in the editor, restore, done |
| "The SD card died" / "I moved to a new Pi" | Fresh install, open the browser, restore from a file or the cloud, everything back as it was (flows running, devices still trusting the CA) |
| "PiWorker does not start any more" | A command that restores with the service stopped |
| "Keep it updated without me" | Opt-in, safe, at night, rolled back on failure, and told what happened |

## Backups

### Content

One archive per backup, `piworker-backup-YYYYMMDD-HHMMSS.tar.gz`:

- `manifest.json`: format (`piworker-backup`, version 1), PiWorker version,
  database schema marker, creation time, host name, the SHA-256 of every
  other entry.
- `piworker.db`: a consistent snapshot taken with `VACUUM INTO` while the
  service runs (no stop, no WAL sidecars, already compacted). In the copy,
  history is removed before it is archived: `flow_runs`, `node_events`,
  `mcp_audit`, `mcp_rejections`, and the short-lived `auth_sessions` and
  `auth_codes`. Flow versions, drafts, approvals, tokens and settings stay:
  they are state, not history.
- `tls/`: the local CA and server certificate, so devices that trust the CA
  keep trusting the restored installation.
- `piworker.toml`: the configuration file, when the server was started with
  one. Informational for the editor restore (see below).

Snapshots are always **full**. A PiWorker database is a few MB without
history; incremental chains save little and add a failure mode (one broken
link loses every later backup). History of backups comes from retention.

### Schedule and retention

- Daily at a configurable local time (default 03:17 plus up to 30 minutes of
  random delay), skipped when nothing changed since the last backup
  (compared by a hash of the archived content, computed after history is
  removed).
- "Back up now" in the editor, and a backup before every restore.
- Retention per destination, grandfather-father-son: the newest 7 daily,
  4 weekly and 6 monthly (configurable; minimum 1). Only files whose name
  and manifest PiWorker wrote are ever deleted.

### Destinations

1. **Local directory**, default `backups/` next to the database
   (`/var/lib/piworker/backups` for managed installs, the data volume in
   Docker). Configurable, typically a USB drive: the SD card is the part of
   a Pi that fails, and a copy on the same card does not survive it.
   PiWorker never mounts anything; it checks the directory is writable and
   reports it otherwise.
2. **S3-compatible storage** (AWS, Backblaze B2, Cloudflare R2, MinIO,
   Wasabi): endpoint, region, bucket, prefix, access key ID; the secret key
   is stored like the passphrase (below). Path-style addressing optional
   for MinIO.

No other protocol in this iteration.

### Encryption

- Local copies are not encrypted: they have the database's protection (0600
  files in a 0700 directory) and the database next to them is plaintext
  (ADR-0030).
- Remote copies are **always** encrypted with [age](https://age-encryption.org)
  using a passphrase (scrypt recipient, work factor 2^15 so a Pi 3 seals it
  in about a second). With age a user can decrypt a copy with the standard
  `age -d` tool even if PiWorker no longer runs.
- The passphrase is set once (at least 12 characters, as in ADR-0031),
  stored in the database so backups run unattended, and never shown again;
  the editor can check a typed passphrase against it. Storing it adds
  little: whoever reads the database already has everything the copy holds.
  It is **not** a user secret: flows cannot reference it, and it lives in
  the backup settings, not the `secrets` table.
- The editor says plainly: keep the passphrase in your password manager;
  without it the remote copies cannot be restored.

### Status

A Backups page in Settings: last result per destination, size and age,
the list of copies (download, restore, delete), the settings. The shell
shows a warning when the last backup failed or the newest is older than
48 hours while backups are enabled. A failed backup is logged as an error.

## Restore

### Paths

| Where | When | Needs |
|---|---|---|
| Editor, Backups page | Running installation | Account password (step-up); passphrase for a remote copy |
| First-run setup page, "Restore from a backup" | Fresh installation, no account yet | The setup code; the file or the S3 settings and passphrase |
| `piworker -restore FILE` / `sudo piworkerctl restore FILE` | Service does not start | Shell access; service stopped |

### Mechanism: staged, applied at startup

The server never replaces the database it has open.

1. Validate before anything is written: manifest, checksums, archive layout
   (only the known entries, regular files, no paths outside), SQLite
   `PRAGMA integrity_check`, and that the backup is not from a newer schema
   than this binary understands (older is fine; startup migrates it).
2. Take a backup of the current state ("before restore") unless the
   installation is fresh.
3. Write the validated content to `restore-pending/` next to the database
   and exit with a dedicated status code. systemd (`Restart=on-failure`) and
   Docker (`restart: unless-stopped`) start it again; a manually started
   server prints that it must be started again.
4. At startup, before the database is opened, a complete `restore-pending/`
   (marked by a final `ready` file) replaces `piworker.db` and its sidecars
   and `tls/`; the old ones move to `restore-previous/` until the next
   successful start.
5. The restored state is the whole state: flows that were running start
   again (`restoreRunningFlows`), accounts and tokens are those of the
   backup, every session is revoked.

The configuration file is not written by the editor restore: on managed
installs it is owned by root, and in Docker it is the environment. The
editor shows when the backup's configuration differs. `piworkerctl
restore` restores it too.

## Updates

### Update check (every installation)

Once a day, after a random delay, the server asks GitHub for the latest
stable release (`/releases/latest` redirect, no API token). The editor
shows "PiWorker vX.Y.Z is available" with a link to its notes; Docker and
manual installations get the command to run. `[updates] check = false`
turns it off; nothing else is sent.

### Release trust

Releases are not signed for now: a manual signing step per release was
judged too much friction. A release is trusted as today, downloaded over
HTTPS from the repository's releases and checked against its
`SHA256SUMS`. The automatic policy limits the exposure (opt-in, stable
only, a waiting period). Signing is revisited when it can be automated
without a manual step (ADR-0033).

### Requested updates (managed installations)

The server decides *when*; root only executes:

- The editor's "Update now" and the automatic policy write a request file
  (`update-request`, the version it expects) in the data directory.
- `piworkerctl install` adds `piworker-update.path`, which runs
  `piworkerctl update --requested` as root when the file appears. The
  manager treats the file as untrusted: it only accepts the latest stable
  release, never older than the installed one, and removes the request.
- The outcome (`update-result`, written by the manager: installed, rolled
  back, refused, with the reason) is shown in the editor; a rolled-back
  version is skipped by the automatic policy until a newer one appears.

### Automatic policy

`off` (only the check) by default; `auto` is opt-in in Settings. With
`auto`, a release is installed when it is stable, at least 3 days
old (time to withdraw a bad one), and, while PiWorker is 0.x, only within
the same minor version (other updates are offered, not installed); in the
configured nightly window with a random delay.

### Update backups

`piworkerctl update` keeps its stopped full copy (it is the rollback and it
includes the old binary) but keeps only the newest 3; `--keep-backups N`
changes it.

## Phases

1. Backup core: snapshot, archive, local destination, schedule, retention,
   Backups page, restore from the editor and from the command line.
   Retention for `piworkerctl update` copies.
2. Restore from the first-run setup page.
3. S3-compatible destination and age encryption.
4. Update check and notice.
5. Requested and automatic updates.

Every phase is test-first; phases 1–3 under ADR-0032, 4–5 under ADR-0033.
