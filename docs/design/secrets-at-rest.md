# Secrets at rest: review (2026-11-13)

Status: decided in [ADR-0030](../adr/0030-no-application-encryption-of-secrets-at-rest.md).

PiWorker keeps every secret value (`secrets` table), the broker
connections' passwords (`broker_connections`) and any credential an admin
typed straight into a node setting (inside the flow JSON) in its SQLite
database, in plaintext. The release checklist listed "secrets are not
encrypted at rest" as a known limit and the TODO asked whether to encrypt
them with key management and recovery. This document is that review.

## What is in the database

| Data | Stored as | Useful to an attacker who reads the file |
|------|-----------|-------------------------------------------|
| Secret values, broker passwords | plaintext | yes: API keys, bot tokens, SMTP/MQTT passwords |
| Credentials typed into node settings | plaintext inside the flow | yes (the editor already steers them into secrets) |
| Account password | bcrypt hash | an offline guessing target only |
| Agent tokens | SHA-256 of a 256-bit random secret | no |
| Sessions, recovery codes | hashed | no |
| Flows, variables, audit, runs | plaintext | flow logic, payload previews |

Next to the database, `tls/ca.key` (the local CA's private key, 0600) is at
least as sensitive: whoever holds it can impersonate any site to the devices
that trust the CA.

## Who could read it

| Threat | Does app-level encryption help? |
|--------|---------------------------------|
| **Stolen SD card or disk** (the whole device) | Only if the key is *not* on that disk. A Pi has no TPM or secure element, and PiWorker must start unattended after a power cut, so the key would sit on the same card (a key file, an environment variable in the unit, systemd credentials without a TPM). Then the thief has both. Full-disk encryption (LUKS) with a passphrase or network unlock is the layer that works here, and it covers `ca.key` and the payloads too. |
| **A copy of the database leaves the device** (a backup on a NAS or in the cloud, a copy sent for debugging, the update backups under `/var/backups/piworker`) | Only if the copy excludes the key. Every documented backup copies the whole data directory, and must (a restore needs the key, or every secret is lost). A key kept elsewhere is a second thing to back up, and the one people lose. |
| **Another local user or process** on the device | Not needed: file permissions do this. The installer runs the service as its own user with `umask 077`, a 0750 data directory and `UMask=0027`. A binary started by hand used the caller's umask (often 0644: readable by every user); fixed in this review (below). |
| **Root, or code running as the service** (a compromised node, `Execute Command`) | No: the running process must decrypt the values to use them, so the key is in reach. |
| **AI agents** | Unrelated: agents never read values (ADR-0021); encryption at rest changes nothing there. |
| **SQLite leftovers** (free pages, an old WAL) | Encryption of new writes would not erase the plaintext already in free pages until a `VACUUM`. |

## Options

1. **Encrypt values in the application** (AES-GCM with a key file next to
   the database, or an environment variable). Protects only a database copied
   *without* its key file. Costs: key generation and storage, rotation,
   migration of existing rows, a recovery story for a lost key (there is
   none: the secrets are gone), and every restore procedure and backup
   instruction changes. For the threats above it adds little over file
   permissions plus disk encryption.
2. **Key derived from a passphrase typed at start.** Real protection for a
   stolen device, but PiWorker must resume flows after a power cut without a
   person; a device that waits for a passphrase is not one users run.
3. **SQLCipher** (encrypt the whole file). The same key placement problem as
   option 1, plus a different cgo dependency on every platform we build for.
4. **No application encryption; protect the file and the places where
   secrets leave on purpose.** File permissions enforced by the binary,
   disk encryption documented for the device, and secrets encrypted with a
   passphrase whenever PiWorker itself writes them to a file (the
   export/restore of the stores, ADR-0031).

## Decision

Option 4 (ADR-0030):

- No application-level encryption of the database for now. Its threat
  coverage on a Pi without a hardware key store does not justify the key
  management and the risk of losing every secret.
- `storage.NewSQLiteStore` creates a new database file with mode 0600 and
  removes other users' access from an existing database and its `-wal`,
  `-shm` and `-journal` files, logging the change (owner and group bits are
  kept: an installation may grant its group read access on purpose).
- An export that includes secret values is encrypted with a passphrase
  (scrypt + AES-256-GCM); without one the export carries no secret values.
- The operations guide recommends full-disk encryption for devices that may
  be stolen, and treating backups as credentials (it already does).

## Revisit when

- The device has a hardware key store PiWorker can use unattended (a TPM on
  x86_64, a secure element), which would make option 1 cover a stolen disk.
- Backups become a PiWorker feature (issue #128) that writes copies off the
  device: those copies should be encrypted like exports.
- More than one administrator, or untrusted local users, share a device.
