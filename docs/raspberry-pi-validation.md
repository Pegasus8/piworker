# Final Raspberry Pi validation

## Automated checks (recommended first)

Copy `scripts/validate_pi.py`, the matching archive and `SHA256SUMS` to a directory
on the Pi. Python 3.9+ is the only extra requirement; Go, Bun and Docker are not
needed. In that directory run:

```bash
python3 validate_pi.py
```

The script detects ARM64 versus ARMv7, verifies the selected archive's checksum,
extracts only known regular files and starts a temporary instance on a free
loopback port. It tests first-account setup, login, embedded assets, real flow
deployment/manual execution, persistent global/flow variables, secret-name
storage, restored flows/sessions after process restart, and account recovery.
It stops its instance and removes its temporary database afterward.

Each run writes `piworker-validation-<id>.json` in the current directory, without
credentials or session tokens. Exit code 0 means all automated checks passed,
1 means failure, and 130 means interruption. Use `--report report.json` for an
explicit report path; an existing report is never overwritten. On cleanup failure,
the report records the temporary directory that needs attention.

An explicit package or trusted unpacked binary can also be selected:

```bash
python3 validate_pi.py --package piworker-linux-arm64.tar.gz
python3 validate_pi.py --binary ./piworker-linux-arm64
```

The `--binary` option skips archive checksum verification. The script also runs
on a development machine with a matching local binary for regression testing.

To run the same application checks through **real systemd** on the Pi:

```bash
sudo python3 validate_pi.py --systemd
```

This mode uses a transient `piworker-validation-<random>` unit, a non-root `nobody`
identity and private temporary data. It stops/collects that unit afterward and
does not modify, enable, disable or uninstall `piworker.service`. It does not
test the installer's actual unit, update/rollback flow or boot enablement.
The owner has reported successful process and transient-systemd runs on `rpi4`;
see the [recorded results](release-validation.md#raspberry-pi-results-reported-by-the-owner).
Reboot, GPIO/I2C/1-Wire, browser rendering and the real
installer remain explicitly listed as untested in the report. No automatic reboot
or hardware writes are performed.

The prepared local `build/release-validation/piworker-pi-test-kit.tar.gz` includes
the script, both Pi archives and the manifest. After copying it to the Pi:

```bash
tar -xzf piworker-pi-test-kit.tar.gz
cd piworker-pi-test-kit
python3 validate_pi.py
```

Use a test Pi and a separate database. Do not connect test flows to production
actuators or external services. Record the OS, CPU, version, result and relevant
redacted logs for each step. This checklist complements automated tests; it is
not a claim that GPIO, boot or the installed PiWorker service have been tested.

## Try the local candidate binary

The local validation produces archives in `build/release-validation/`, marked
`v0.0.0-validation`. They are test artifacts, not a published release. Copy the
archive for the Pi's OS architecture and `SHA256SUMS` to a new directory on the Pi.
Use ARM64 for 64-bit Raspberry Pi OS, or `arm` for ARMv7 32-bit OS. ARMv6 is unsupported.

```bash
uname -m
getconf GNU_LIBC_VERSION
# In the directory containing the copied archive and checksum manifest:
sha256sum --check --ignore-missing SHA256SUMS &&
tar -xzf piworker-linux-arm64.tar.gz
./piworker-linux-arm64 -version
./piworker-linux-arm64 -addr :18080 -db ./validation.db
```

Replace `arm64` with `arm` if needed. Require glibc 2.35+ and successful checksum
verification before executing anything. Open `http://<pi-address>:18080`, use
the printed setup code and create an account. Keep the terminal open while testing.

- [ ] The UI and initial account setup work from another LAN device.
- [ ] Create, save, deploy and manually run a harmless flow; inspect its result.
- [ ] Create global/flow variables and a test secret; check they survive a restart.
- [ ] Stop with Ctrl+C, run the same command again, then verify account access,
  saved configuration and expected restoration of deployed flows.
- [ ] Resize the editor on a phone and reopen node configuration; nodes remain reachable.
- [ ] Confirm `curl -f http://127.0.0.1:18080/api/health` succeeds while running.
- [ ] Test the actual GPIO/I2C/1-Wire devices you intend to support with appropriate
  permissions. Record hardware models and OS/bus configuration separately.

## Validate the managed service and real download path

Stop the standalone binary first. Full installer validation requires a compatible
published release or prerelease: local test archives are not served by GitHub,
and draft assets require authentication. A prerelease lets you test explicit-version
installation before promoting a stable release. Replace `vX.Y.Z` below with its tag.

```bash
curl -fL https://github.com/Pegasus8/piworker/releases/download/vX.Y.Z/piworkerctl -o piworkerctl &&
sudo bash ./piworkerctl install vX.Y.Z
systemctl status piworker
piworker -version
sudo journalctl -u piworker -n 50 --no-pager
```

- [ ] Install on a clean system and complete setup at port 8080.
- [ ] Reboot the Pi; confirm automatic service startup and account/flow persistence.
- [ ] Confirm the `piworker` service user can access each required hardware device.
- [ ] Update explicitly to a compatible test version (reinstalling the same tag
  exercises replacement but does not prove migration between different versions).
  Check the reported backup directory and preserved accounts, variables and secrets.
- [ ] On this test installation, force a health-check failure with
  `sudo PIWORKER_HEALTH_URL=http://127.0.0.1:1/api/health piworkerctl update vX.Y.Z`.
  The command must fail and restore the old binary/data. Verify the service and login
  afterward, then run a normal update to confirm recovery.
- [ ] Run `sudo piworkerctl uninstall`; confirm the service is gone and data/config
  remain. Download/run the installer again; existing accounts and flows must return.
- [ ] After stable publication, repeat the README's unversioned download and
  `sudo piworkerctl update` to verify GitHub's `latest` path.

Use the [operations guide](operations.md) for setup-code renewal, account recovery
and manual restore. Do not post setup codes, cookies, secret values or full
databases when reporting a result.

## Real installer, reboot, browser and hardware kit

Use the [installation test kit](raspberry-pi-installation-test.md) after the isolated
checks pass. It tests the real lifecycle with local candidate packages and resumes
after reboot. Optional Chromium and explicit device/sensor checks are included.
