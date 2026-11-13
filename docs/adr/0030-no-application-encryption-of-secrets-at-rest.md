# ADR-0030: No application encryption of secrets at rest; private database files and encrypted exports

**Status:** Accepted
**Date:** 2026-11-13
**Deciders:** maintainer
**Design:** [docs/design/secrets-at-rest.md](../design/secrets-at-rest.md)

## Context

Secret values and broker passwords are stored in plaintext in SQLite. The
TODO asked whether to encrypt them, with key management and recovery. A Pi
has no hardware key store and PiWorker must resume unattended after a power
cut, so any key would live on the same disk as the database; backups copy
the whole data directory. A binary started by hand created its database
with the caller's umask, often readable by every local user.

## Options considered

### Option A: encrypt values in the application (key file or environment variable)

| Dimension | Assessment |
|---|---|
| Complexity | Medium–High: key handling, migration, rotation, restore procedures |
| Security | Protects only a database copied without its key; not a stolen device, not root |
| Pi resources | Negligible |
| Maintenance | A lost key loses every secret; every backup instruction changes |

### Option B: passphrase at start, or SQLCipher

**Pros:** real protection for a stolen device (passphrase). **Cons:** no
unattended restart (passphrase); SQLCipher has option A's key problem plus a
new cgo dependency on six targets.

### Option C: no application encryption; private files; encrypted exports; disk encryption documented

**Pros:** covers the realistic local threat (other users) with no key to
lose; the one place PiWorker writes secrets to a file on purpose is
encrypted. **Cons:** a copy of the database (or the device) still exposes
the secrets; that remains the admin's to protect (disk encryption, backups
kept like credentials).

## Decision

Option C.

- The application does not encrypt the database or its values.
- `storage.NewSQLiteStore` creates a new database file with mode 0600 and,
  for an existing one and its `-wal`, `-shm` and `-journal` files, removes
  the permission bits of other users (owner and group bits stay), logging
  each change; it never fails the start for this. Not on Windows, not for
  in-memory databases.
- An export of the stores that includes secret values is encrypted with a
  passphrase (ADR-0031); otherwise it carries no secret values.
- The documentation states the limit, recommends full-disk encryption for
  devices that may be stolen and treating backups as credentials.

## Consequences

- Easier: no key to lose; restores stay a file copy.
- Harder: whoever copies the database or holds the device reads the
  secrets.
- Revisit when: a hardware key store is usable unattended, PiWorker writes
  backups off the device (issue #128), or several admins or untrusted local
  users share a device.

## Action items

1. [x] Private database files (`internal/storage/file_mode.go`).
2. [x] Encrypted secret values in exports (ADR-0031).
3. [x] Documentation: README, user guide, operations guide, release checklist.
