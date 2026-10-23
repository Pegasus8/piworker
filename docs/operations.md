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

The manager currently provides install/update/uninstall only. Start/stop/status
and logs are systemd commands. The service runs as `piworker`, starts on boot,
reads `/etc/piworker/piworker.toml`, and stores its database in
`/var/lib/piworker/piworker.db`. Its explicit database argument takes precedence
over the TOML `db` setting. Changes to the configuration need a service restart.
Shell exports are not inherited by an already configured systemd service.

## Setup and account recovery

Open `http://<device-address>:8080` and use the setup code from the service logs.
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
root-owned SQLite sidecar files. These commands can run while the service is up.
Logs may contain setup codes and flow output; redact them before sharing.

## Backups and recovery after a failed update

Every managed update makes a private directory under `/var/backups/piworker/`
and prints its path. It contains `piworker` (the old binary), `data/` (the entire
stopped data directory) and `config/` (including the old installed-version marker).
Backups are retained automatically; review disk usage and remove only backups
you no longer need. Protect them like credentials because the database contains
accounts and secrets. Keep an off-device copy for disk failure recovery.

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

## Troubleshooting

| Symptom | What to check |
|---------|---------------|
| Download returns 404 | The release needs `piworkerctl`, the archive for your CPU and `SHA256SUMS`; old releases lack these assets. |
| Binary cannot run | Use the matching CPU/OS, glibc 2.35+ and the required runtime libraries; ARMv6 is unsupported. |
| Health check fails | Inspect logs, check for an occupied port, and supply `PIWORKER_HEALTH_URL` when using a non-default address. |
| Login fails after enabling Secure cookies | That option needs browser access through HTTPS; it does not create a TLS listener. |
| GPIO/I2C permission denied | Enable the device/bus and give the `piworker` service user the appropriate groups for your OS, then restart it. |
| Another lifecycle operation is running | Check for an active installer. Only remove `/run/lock/piworker-install.lock` if a crashed/interrupted operation left it behind and no lifecycle process remains. |

## Uninstall and reinstall

`sudo piworkerctl uninstall` removes the service and executables. It retains the
service account, data, configuration and backups. Download the installer again
and run `install` to reuse those retained files. Complete erasure is a separate,
deliberate removal of `/var/lib/piworker`, `/etc/piworker` and the backups; verify
you no longer need them first. This command does not affect Docker containers.
