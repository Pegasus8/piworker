# Operating a native PiWorker installation

These instructions apply to `piworkerctl` installations. Manual binaries and
Docker have their own process manager and data paths; do not mix them with the
native installer. Never run lifecycle commands against a disposable test copy
on the same system as a production managed installation: its paths are fixed.

## Everyday commands

```bash
sudo piworkerctl update
piworker -version
systemctl status piworker
sudo systemctl restart piworker
sudo systemctl stop piworker
sudo systemctl start piworker
sudo journalctl -u piworker -n 100 --no-pager
```

The manager provides install/update/uninstall and `tls on|off`. Start/stop/status
and logs are systemd commands. The service runs as `piworker`, starts on boot,
reads `/etc/piworker/piworker.toml`, and stores its database in
`/var/lib/piworker/piworker.db`. Its explicit database argument takes precedence
over the TOML `db` setting. Changes to the configuration need a service restart.
Shell exports are not inherited by an already configured systemd service.

## Setup and account recovery

Open the HTTPS address the installer printed (for example
`https://raspberrypi.local:8443`; `http://<device-address>:8080` redirects there)
and use the setup code from the service logs. Until the device trusts the local
CA the browser warns; trust it first (see [HTTPS and the local CA](#https-and-the-local-ca)).
If the code expired and no account exists yet, generate another one:

```bash
sudo -u piworker /usr/local/bin/piworker -config /etc/piworker/piworker.toml -db /var/lib/piworker/piworker.db -setup-code
```

For an existing account, select **Forgot password?** in the browser and run:

```bash
sudo -u piworker /usr/local/bin/piworker -config /etc/piworker/piworker.toml -db /var/lib/piworker/piworker.db -recover-account YOUR_USERNAME
```

Enter the printed code and new password in the browser. Both codes expire after
30 minutes and are usable once. Use the service account to avoid creating
root-owned SQLite sidecar files. These commands can run while the service is up,
and they need only the database: a listener or TLS problem in the configuration
that keeps the service from starting does not block them.
Logs may contain setup codes and flow output; redact them before sharing.

A recovery (and, unless you keep them, a password change) revokes every agent
token in the same step as the new password. If the revocation fails, the
password is not changed either: retry with the same code.

## HTTPS and the local CA

PiWorker serves HTTPS on port 8443 with its own certificate authority (CA),
created on first start in `/var/lib/piworker/tls/` (`ca.crt`, `ca.key`,
`server.crt`, `server.key`). Port 8080 redirects to HTTPS but answers
`/api/health`, `/piworker-ca.crt` and (by default) webhooks directly.

- **Trust the CA once per device.** Open `/trust` in the editor (also linked from
  the HTTP warning and from Settings): it downloads the CA and shows per-OS steps.
  Compare the fingerprint with the one printed by `piworkerctl install` or in the
  log (`sudo journalctl -u piworker | grep "CA fingerprint"`), or read it with
  `openssl x509 -in /var/lib/piworker/tls/ca.crt -noout -fingerprint -sha256`.
- **Renewal is automatic.** The server certificate is re-issued without a restart
  30 days before it expires and within 10 minutes of a new address or host name.
  It never outlives the CA. Extra names go under `hosts` in the `[tls]` section.
- **The CA lasts 10 years and is never replaced on its own.** From 90 days before
  it expires, the log (about once a day) and the Trust this device page show a
  warning. To replace it, run the command below, restart the service and trust
  the new CA on every device:

  ```bash
  sudo -u piworker /usr/local/bin/piworker -config /etc/piworker/piworker.toml -db /var/lib/piworker/piworker.db -tls-regenerate-ca
  sudo systemctl restart piworker
  ```
- **Turning HTTPS off** (recovery, or behind your own HTTPS proxy):
  `sudo piworkerctl tls off`; back on with `sudo piworkerctl tls on`. With TLS off
  the service serves plain HTTP on the `addr` port (8443 by default).
- Handshake errors from browsers that do not trust the CA yet, scanners and
  probes (including connections that negotiate HTTP/2 and then send something
  else) are logged only at debug level (`-debug`), at most 10 per minute. Other
  HTTP server errors are warnings, at most 30 per minute; the number of lines
  left out is logged when the next minute starts.

## Backups and recovery after a failed update

Every managed update makes a private directory under `/var/backups/piworker/`
and prints its path. It contains `piworker` (the old binary), `data/` (the entire
stopped data directory) and `config/` (including the old installed-version marker).
Backups are retained automatically; review disk usage and remove only backups
you no longer need. Protect them like credentials because the database contains
accounts and secrets, and `data/tls/ca.key` is the private key of the local CA
that your devices trust: whoever holds it can impersonate any website to them.
Keep an off-device copy for disk failure recovery, stored as securely; if a copy
may have been exposed, regenerate the CA (see HTTPS in the README) and trust the
new one on each device.

PiWorker does not encrypt secrets inside its database
([ADR-0030](adr/0030-no-application-encryption-of-secrets-at-rest.md)): it
keeps the file private instead. A new database is created with mode 0600,
and at startup an existing database (and its `-wal`, `-shm` and `-journal`
files) loses any access for other users; the log says
`Database file was readable by other users; removed their access` when that
happens. Group access is left as the installation set it. If the device
could be stolen, encrypt its disk (LUKS on Raspberry Pi OS or a PC): that is
the layer that protects the database, `tls/ca.key` and the backups together.

For a manual backup, stop the service first, copy the whole data directory and
configuration while preserving ownership/permissions, then start it again.
Do not copy only `piworker.db` while the service writes to it: SQLite may also
have a WAL and other sidecar files. Include the binary and, for disaster recovery,
`piworkerctl` and `/etc/systemd/system/piworker.service` in your copy.

If an update fails, the manager attempts to restore the old binary and data.
If it reports that recovery failed, keep the service stopped and inspect the
printed backup directory. To recover manually:

1. Verify the backup contains the old binary, `data/` and `config/`, and that
   there is enough free space to preserve the failed installation too.
2. Stop `piworker.service` and confirm it is inactive before touching SQLite.
3. Move the current data directory aside; restore the entire backup `data/` to
   `/var/lib/piworker`, preserving ownership and permissions. Do not overlay an
   old database onto a directory containing WAL files from a newer database.
4. Restore the matching `piworker` binary to `/usr/local/bin/piworker` with mode
   755. Restore the backed-up configuration and installed-version marker under
   `/etc/piworker`, preserving ownership and permissions. Keep any newer config
   separately before replacing it.
5. Start the service, inspect logs and confirm login and saved flows. Deployed
   flows may resume and perform real actions; isolate external devices/services
   when testing a backup on another machine.

A backup restores the state at its creation time, not changes made afterward.
Automatic rollback does not undo external effects from running flows.

## Agents: when one misbehaves

If an AI agent did something you did not expect (a prompt injection, a
leaked token, a wrong instruction), contain it first, then undo and review.
Everything below happens in the editor (**Agents** in the navigation).

1. **Revoke the token.** **Agents → Tokens → Revoke** on the token, or
   **Revoke all** when you do not know which one. It needs no password and
   takes effect at once: the token stops working on `/mcp`, its requests
   waiting for approval are rejected and its pending drafts are voided.
   Account recovery and a password change (unless you keep the tokens) also
   revoke every token.
2. **Turn agent deploy off.** Set **Agent deploy** to **Off** (needs your
   password). This removes the deploy tools from every token but undoes
   nothing: flows agents already deployed keep running, privileged ones
   included. To shut agents out entirely, set `enabled = false` in the
   `[mcp]` section and restart the service: `/mcp` then answers 404.
3. **Undo what it changed.** On the flows list and in the editor, look for
   **Deployed by agent** and **Changed by agent**. Stop a flow you do not
   want running, or open **History → Roll back** to save the version you
   last approved as the current one (a running flow is redeployed with it).
   An agent's *stop* leaves no marker on the flow: a flow an agent stopped
   shows only in the audit. Reject any draft still waiting under **Drafts**.
4. **Read the audit.** **Agents → Audit** lists every call, read, draft,
   deploy and stop of each token for 30 days (filter by token, tool, flow,
   outcome and time), plus the calls refused as busy, cooling down or with
   invalid arguments, counted per token. A flow's run history says which
   runs an agent token started and which version they ran. Ordinary rows are
   capped at the newest 50 000; a privileged tool's runs and approvals and a
   privileged flow's deploys and stops are kept the full 30 days.
5. **Rotate what it could reach.** Change the secrets used by flows the
   token could call, read or deploy, short ones first (the **Agents** page
   counts secrets under 16 characters: an agent can confirm guesses of
   them), and the credentials of the services those flows reach.

Then create a new token with only the tools and scopes the agent needs.

## Troubleshooting

| Symptom | What to check |
|---------|---------------|
| Download returns 404 | The release needs `piworkerctl`, the archive for your CPU and `SHA256SUMS`; old releases lack these assets. |
| Binary cannot run | Use the matching CPU/OS, glibc 2.35+ and the required runtime libraries; ARMv6 is unsupported. |
| Health check fails | `piworkerctl` checks `https://127.0.0.1:<addr port>/api/health` against the local CA (plain HTTP with `[tls] mode = "off"`). Inspect logs, check for occupied ports (8443 and 8080), and supply `PIWORKER_HEALTH_URL` when using a non-default address. |
| Service refuses to start: `http_addr` uses the same port as `addr` | An old configuration with a lone `addr = ":8080"`: set `addr = ":8443"` and `http_addr = ":8080"` (or `http_addr = ""`). |
| Browser warns about the certificate | Trust the local CA on that device (`/trust`) and check its fingerprint. After trusting it, a warning means something is wrong: do not continue. |
| Locked out after a TLS change | `sudo piworkerctl tls off`, then fix the configuration and `sudo piworkerctl tls on`. |
| Login fails after enabling Secure cookies | `session_secure` is only for `[tls] mode = "off"` behind your own HTTPS proxy; it needs browser access through HTTPS. With native HTTPS, cookies are already Secure. |
| GPIO/I2C permission denied | Enable the device/bus and give the `piworker` service user the appropriate groups for your OS, then restart it. |
| Another lifecycle operation is running | Check for an active installer. Only remove `/run/lock/piworker-install.lock` if a crashed/interrupted operation left it behind and no lifecycle process remains. |

## Uninstall and reinstall

`sudo piworkerctl uninstall` removes the service and executables. It retains the
service account, data, configuration and backups. Download the installer again
and run `install` to reuse those retained files. Complete erasure is a separate,
deliberate removal of `/var/lib/piworker`, `/etc/piworker` and the backups; verify
you no longer need them first. This command does not affect Docker containers.
