# Real installation and reboot validation

This complements `validate_pi.py`: it runs **the real installer against production
paths** on a clean Linux/systemd Raspberry Pi, with Python 3.9+, curl, tar and glibc
2.35+. Do not use a Pi with an existing PiWorker installation or valuable PiWorker
data. Preflight refuses existing service, binary, configuration, data or backups,
and requires ports **8443** and **8080** to be free.
It does not automatically install OS dependencies or reboot the machine.

## HTTPS during the test

The installer sets up native HTTPS: the service serves the editor and API on
`https://<pi>:8443` with a certificate from the local CA it creates on first
start (`/var/lib/piworker/tls/ca.crt`). Port 8080 only redirects to HTTPS,
except `/api/health` and `/piworker-ca.crt`, which it answers directly.

The test talks to the service at `https://127.0.0.1:8443` and trusts **only the
installed CA** (`/var/lib/piworker/tls/ca.crt`); certificate verification is
never turned off. It also checks that port 8080 answers `/api/health` and
serves exactly the installed CA. Each phase reads the CA file again; the kit
does not check that the CA stays the same across update, reinstall and reboot
(PiWorker only replaces it with `-tls-regenerate-ca`), so compare
`/var/lib/piworker/tls/ca.crt` before and after if you need that.

To look at the test installation from another computer, download the CA from
`http://<pi>:8080/piworker-ca.crt`, compare its SHA-256 fingerprint with the
one `piworkerctl install` printed (`openssl x509 -in piworker-ca.crt -noout
-fingerprint -sha256`), trust it (the editor's **Trust this device** page has
per-OS steps) and open `https://<pi>:8443`. Remove that trust when you are done
with the test Pi.

## Run the prepared kit

Transfer `build/release-validation/piworker-installation-test-kit.tar.gz` to the Pi:

```sh
tar -xzf piworker-installation-test-kit.tar.gz
cd piworker-installation-test-kit
sudo python3 validate_installation.py prepare
sudo reboot
```

After reconnecting:

```sh
cd ~/piworker-installation-test-kit
sudo python3 validate_installation.py resume
sudo cat /var/lib/piworker-release-test/resume-report.json
```

`prepare` installs, creates an account/session and deployed flow, forces an update
health failure to test rollback, updates successfully, uninstalls, and reinstalls.
It checks that the session, configuration and data survive and that the flow still
executes. It leaves the service enabled and awaiting reboot. `resume` requires a
new Linux boot ID on the same machine and checks the service **before issuing any
start command**. It verifies the saved session, data and flow execution, then
uninstalls the service, binary and manager. Configuration, database and backups
remain for inspection, following the installer's normal uninstall behavior.

The private state directory `/var/lib/piworker-release-test` remains too. Its
`prepare-report.json` and `resume-report.json` contain results without credentials.
`installer.log` records installer output. **Do not share `cookies` or `state.json`.**
If a check fails, the script exits nonzero and leaves the installation available
for inspection; it does not erase data. Inspect the report, installer log and
`sudo journalctl -u piworker.service`. To stop/remove a test installation after a
failure, run `sudo bash /var/lib/piworker-release-test/piworkerctl uninstall` if
the install completed. Data and backups are deliberately retained. A fresh test
requires a clean Pi or manually moving aside the test artifacts after inspection.

## Browser and hardware options

If Chromium is already installed, include `--browser` during `prepare`:

```sh
sudo python3 validate_installation.py prepare --browser
```

This runs Chromium as the service account, with its sandbox enabled and a temporary
profile, opens `https://127.0.0.1:8443/`, verifies the rendered login and saves
`/var/lib/piworker-release-test/login.png`. Chromium cannot be pointed at a CA
file, so the script first verifies the certificate the service presents against
the installed CA and then lets Chromium accept only that certificate (its public
key pin); any other certificate still fails.
Failure to launch or render is a failed check; it is not silently counted as passed.
Without the flag the report explicitly marks browser rendering as not tested.
This does not replace interactive flow editing and mobile browser checks.

Optional hardware checks require explicit device paths. Examples (only select
paths actually present on your Pi):

```sh
sudo python3 validate_installation.py prepare --device /dev/gpiochip0 --device /dev/i2c-1
sudo python3 validate_installation.py prepare --sensor /sys/bus/w1/devices/28-0123456789ab/w1_slave
```

`--device` checks read/write **permissions of the piworker service account**, without
opening or driving the device. `--sensor` reads a specified 1-Wire temperature
sensor as that account and requires a valid CRC and temperature. Neither changes
groups or permissions. Physical GPIO/I2C operation through PiWorker nodes still
requires a test designed for the actual wiring; the kit does not assume pin numbers.

## Candidate packages and limits

The prepared kit includes ARM64 and ARMv7 archives in `initial/` and `update/`, each
with SHA256SUMS and the current lifecycle manager. The candidates use the same
application code with different version metadata (`v0.0.0-validation` and
`v0.0.0-validation-update`). This exercises binary replacement and restoration,
not schema migration between different application implementations.

For separately built releases, pass `--initial /path/to/first` and
`--update /path/to/second`. Each directory must contain the architecture's archive
and SHA256SUMS; the binaries must differ and expose a `vVERSION` with `-version`.
The bundled manager must support `PIWORKER_RELEASE_DIR`. Checksums detect corruption;
these locally supplied packages must come from a trusted source.

The installer now accepts `PIWORKER_RELEASE_DIR` with an explicit version to use
local archives while retaining checksum checks. The test exercises that real
installer path; GitHub download, release publication, remote CI, power-loss recovery
and hardware output remain separate checks. A `passed` report applies only to the
checks listed as passed, with omissions explicitly listed in `not_tested`.
