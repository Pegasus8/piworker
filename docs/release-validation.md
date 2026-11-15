# Local release validation — 2026-09-14

Candidate: working tree based on `03f28f9` on `revival`, including the local
documentation, version, health and responsive-canvas fixes. These changes have
not yet been committed or run through remote CI. Test builds use
`v0.0.0-validation`; this is not the selected public release version.

Environment: macOS ARM64, Go 1.27.1, Bun 1.3.14, Docker 27.3.1 in Colima.
Docker has the legacy builder and no Buildx plugin. Linux native packages were
cross-compiled in Ubuntu 22.04 with glibc 2.35 and Go 1.27.1.

## Results

| Check | Result |
|-------|--------|
| `make fmt-check vet test-race` | Passed across the Go packages, including health/version regressions. |
| Frontend unit tests | 5 passed, 20 assertions. |
| Frontend typecheck and production build | Passed; embedded frontend rebuilt. |
| Full Playwright suite | 34 passed, including live backend scenarios and mobile resize regression. |
| Integrated mobile UI | Reviewed the browser-test screenshot; nodes and controls remain visible in the narrow editor. |
| Installer behavior tests | 14 passed on macOS and in Ubuntu 22.04. OS services/downloads are mocked. |
| Bash syntax and ShellCheck | Passed locally and ShellCheck passed in Ubuntu 22.04. |
| GitHub workflow lint | `actionlint` passed. This does not execute GitHub's release/publish events. |
| `govulncheck@v1.8.0 .` | 0 affected symbols/imported packages; one module-only advisory remains in unused OpenPGP. |
| `bun audit --json` | Empty findings, exit 0. |
| Native macOS release build | Passed with embedded frontend; reports `PiWorker v0.0.0-validation`. |
| Linux ARM64/AMD64/ARMv7 CGO builds | All three compiled and packaged; all SHA256 manifest entries verified. |
| ARM64 native binary on Ubuntu/glibc 2.35 | Smoke test passed. |
| AMD64 native binary on Ubuntu/glibc 2.35 under emulation | Smoke test passed. |
| Production Dockerfile ARM64 | Built successfully; smoke test and the image's actual HEALTHCHECK command passed. |

The container smoke test fetches the embedded HTML and assets, checks the version,
logs in, creates a flow and variable/secret data, evaluates a real processing-node
preview, then removes and recreates the container using the same temporary volume.
It verifies preserved sessions, accounts, flows and variables, and that secret
values are not returned by the names API. Containers and test volumes are removed.
The Ubuntu wrappers exercise the exact glibc binaries, not the production Alpine
image. The production image runs as its configured non-root user.

Reproduce the image checks with:

```bash
python3 -B scripts/smoke_container.py piworker:release-validation-arm64
python3 -B scripts/smoke_container.py piworker:release-validation-glibc-arm64
python3 -B scripts/smoke_container.py piworker:release-validation-glibc-amd64 --platform linux/amd64
```

The native cross-build also needed explicit `libc6-dev-amd64-cross` and
`libc6-dev-armhf-cross` headers in the validation container, which installed
packages without recommendations. This environment adjustment did not change
the release dependency versions or the production Dockerfile.

## Raspberry Pi results reported by the owner

The owner supplied terminal output from `rpi@rpi4` showing both commands completed
with `Resultado: passed` and every listed check marked `[OK]`:

- `python3 validate_pi.py` — report `piworker-validation-79ee4dd5.json`.
- `sudo python3 validate_pi.py --systemd` — report `piworker-validation-7af1cae3.json`.

Both reports were generated under `/home/rpi/piworker-pi-test-kit/` on the Pi.
Evidence here is the supplied terminal output; the JSON files were not retrieved
or inspected. It confirms package verification, executable startup, account
setup/login, embedded asset serving, flow execution, session/data persistence
after restart and account recovery, including through the transient systemd unit.
The output does not identify the OS version or selected CPU architecture.

These two runs close the real-device execution check for the isolated modes.
The subsequent installation-kit evidence below also covers the installed service,
installer lifecycle and boot enablement. Browser and hardware checks remain open.

## Remaining limits

- Device permissions, GPIO/I2C/1-Wire behavior and Pi browser validation remain
  open. Managed installation and reboot passed on the owner's Pi as recorded below.
- ARMv7 compiled, but this Docker VM cannot execute ARMv7 (`exec format error`).
- The full AMD64 production Dockerfile build could not finish here: the legacy
  builder cannot mix the native frontend stage with the emulated backend stage,
  and an all-AMD64 retry stalled in Bun installation under QEMU. That temporary
  build was stopped. The native AMD64 binary did pass the independent smoke test.
  Require the CI Buildx matrix to pass before publishing all image architectures.
- Remote CI, release creation, public download URLs, GHCR publication and anonymous
  pulls were not triggered by this validation. The prior remote failures remain
  historical evidence until a run of the new commit replaces them.
- The dependency scans do not cover the native SQLite C code or container OS
  packages. Their audit scope remains as described in the dependency report.
- Vite reports existing chunk-size and mixed static/dynamic import warnings.
  They do not fail the build; no bundle-performance improvement is claimed.
- Migration from the 2020 release remains unvalidated.

## Packages for the Pi

The additional `scripts/validate_pi.py` runner passed its 10 tests, including a
real native-binary integration test, and its complete package-driven scenario
passed in a network-isolated Ubuntu ARM64 container as a non-root user. It covers
setup, login, real flow execution, persistence and account recovery. Its optional
transient-systemd mode has command-isolation tests and subsequently passed on the
owner's Pi as reported above. `piworker-pi-test-kit.tar.gz` bundles the runner and both Pi archives.

`build/release-validation/` contains the three Linux binaries, their `.tar.gz`
archives, `piworkerctl` and `SHA256SUMS`. The native build recipe is saved there
as `Dockerfile.validation`. Build output is git-ignored and is not published.
Use the archive matching the Pi's OS architecture. See the
[Pi checklist](raspberry-pi-validation.md) for commands and acceptance steps.

## Real installation test kit (local validation)

Added `validate_installation.py` and the accompanying
[runbook](raspberry-pi-installation-test.md). Local validation passed 33 Python tests
(including the native binary integration), Bash syntax and ShellCheck. The new
local-release installer test failed before implementation because the manager
still attempted a GitHub download, then passed with `PIWORKER_RELEASE_DIR` support.
The new validator's safety tests were added before the implementation and passed
afterward; they cover existing paths, private state, changed installation and boot
gates. All four packaged archive checksums and embedded managers were verified.

An additional disposable Ubuntu 22.04 ARM64 container passed the complete sequence
using the real candidate binaries, installer, database and API: install, account
and flow setup, forced health failure and rollback, successful update,
uninstall/reinstall, persisted session/data/flow execution, and final uninstall.
**Systemd supervision and reboot were simulated in this local integration.** It is
not evidence of actual Pi boot behavior. The container initially lacked curl;
installing that prerequisite allowed the integration to run. The validator now
checks core prerequisites before creating its state directory.

The kit contains two builds of the same application code with different version
metadata. It does not validate cross-version schema migrations. Real reboot
subsequently passed on the owner's Pi (below). Chromium rendering and optional
hardware checks remain open; release download/publication is outside this test.


## Real installation and reboot results reported by the owner

The owner supplied terminal output for `sudo python3 validate_installation.py
prepare` and `sudo python3 validate_installation.py resume` on `rpi@rpi4`, plus
the complete JSON contents of `/var/lib/piworker-release-test/resume-report.json`.
The report has `status: passed` and all 17 checks passed. This is owner-supplied
real-device evidence, separate from the simulated container integration above.

Confirmed checks cover both package validations, real install and health, seed
data and flow execution, rollback after a forced update health failure, successful
update with preserved session/data, uninstall, reinstall with preserved
session/data, service enablement, unchanged installation after reboot, automatic
service startup, boot enablement, preserved session/data and flow execution after
boot, and final uninstall. `resume` requires a changed boot ID on the same machine
and checks service activity before starting anything. The service was uninstalled
at completion; configuration, data and backups were retained.

The report explicitly lists these omissions:

- GitHub release download.
- Physical GPIO/I2C operation.
- Interactive browser workflow.
- Power-loss recovery.
- Hardware checks: no devices or sensors selected.
- Chromium rendering: `--browser` was not selected.

The supplied report does not identify OS version, selected architecture or an exact
source commit. It validates the supplied kit on this Pi, not all advertised CPU
architectures or a subsequently rebuilt/tagged candidate. The two candidates
exercise replacement/rollback with different version metadata; legacy migration
and cross-version schema migration remain unvalidated.

## x86_64 native validation — 2026-11-11

Candidate: `9a64d6c` on `claude/stoic-shannon-9ik9n2` plus the x86_64
temperature fix (System Metric and host status read `x86_pkg_temp`, or the
`coretemp`/`k10temp` hwmon sensor, instead of `thermal_zone0`).

Environment: Linux x86_64 cloud container (Ubuntu, glibc 2.39), Go 1.27.3,
Bun 1.3.14; no systemd, no thermal or hwmon sensors.

| Check | Result |
|-------|--------|
| `make build-linux` (embedded frontend, CGO, `x86_64-linux-gnu-gcc`) | `piworker-linux-amd64` built, ELF x86-64. |
| `validate_pi.py --binary build/piworker-linux-amd64` | All 9 checks passed: version, startup/health, first-account setup, embedded UI assets, login, flow deploy and execution, data surviving restart, account recovery. |
| `go test ./...`, `go test -race` on the touched packages, `make fmt-check vet test-installer` | Passed. |

Not tested on x86_64: `piworkerctl install` with systemd and the GitHub
download (`validate_installation.py`), a physical reboot, and a real CPU
sensor (covered by fixture tests for Raspberry Pi, Intel and AMD layouts).


## Zigbee smoke test (simulated Zigbee2MQTT) — 2026-11-11

Candidate: `d07a641` on `claude/stoic-shannon-9ik9n2` (backend). Linux x86_64
container; the PiWorker binary with `-tls=off -auth=false`, a real MQTT broker
(amqtt 0.12.1, Python) and a script playing Zigbee2MQTT with the SONOFF S60ZBTPF
and SNZB-02P definitions of `internal/zigbee/testdata/bridge_devices.json`
(retained bridge state, info and devices; it answers `/set` and `/get`).

12/12 checks passed through the REST API: connection test; connection stored
with its password never listed; discovery of the devices, the bridge (online,
version) and the sensor's cached state; a flow "humidity ≥ 65 → plug on,
≤ 55 → off" created and deployed, turning the simulated plug on at 71 % and off
at 52 %; permit join publishing `{"time":240,"value":true}`; and a flow naming
an undefined connection refused at deploy with 400.

Not tested: a real coordinator, real devices and Zigbee2MQTT itself.

## v1.0.0-beta.1 candidate validation — 2026-11-14

Candidate: `0c7abfe` on `revival` plus the release-preparation commits of
2026-11-14 (security updates, validation-script fix, re-embedded frontend,
documentation). Built with `VERSION=v1.0.0-beta.1`. Environment: Linux x86_64
cloud container, Go 1.27.4, Bun 1.3.14, Docker 29.9.2 with QEMU user emulation
for arm64 and arm/v7. CI on the tag still has to pass before the draft exists.

| Check | Result |
|-------|--------|
| `make fmt-check vet`, `go test -race ./...` | Passed (before and after the dependency updates). |
| Frontend unit tests, `vue-tsc`, production build | 243 tests passed; typecheck and build passed. |
| Playwright browser tests | 70 passed. |
| Installer and validation-kit tests, `bash -n`, ShellCheck | 55 tests passed (1 skipped: needs a real binary), ShellCheck clean. |
| `govulncheck ./...`, `bun audit` | 0 reachable findings, 0 frontend findings after the updates in the [dependency audit](dependency-audit.md). |
| Linux builds (`build-linux`, `build-arm64`, `build-arm`) | ELF x86-64, aarch64 and ARM EABI5 hard-float; highest glibc symbol 2.34. `-version` prints `PiWorker v1.0.0-beta.1` natively (amd64) and under QEMU (arm64, ARMv7). |
| Archives and `SHA256SUMS` | Each `piworker-linux-<arch>.tar.gz` holds `piworker-linux-<arch>` and `piworkerctl`; all 10 manifest entries (3 archives, manager, 6 bridges) verify. |
| Six `piworker-mcp` bridges | Linux amd64/arm64/armv7 print `piworker-mcp v1.0.0-beta.1` (ARM under QEMU); the macOS and Windows binaries are Mach-O/PE for the right CPU and embed the version string (not executable here). |
| Bridge against the candidate (linux-amd64, stdio) | `initialize` and `tools/list` answer; a wrong `--ca-fingerprint` stops every request before the token is sent; a token file with mode 0644 is refused (exit 2). Claude Desktop itself was not used. |
| `scripts/smoke_container.py` | Passed for `linux/amd64`, `linux/arm64` and `linux/arm/v7` images (the last two emulated). The images were built with the release Dockerfile plus this environment's proxy CA and proxy variables, which a CI build does not need. |
| Agent authoring smoke test (checklist §Agent authoring) | All 6 steps passed over HTTPS with MCP protocol 2025-11-25; UI marks, History, rollback and the 409/Reload dialog checked with Playwright. |

Found and fixed during this validation:

- `smoke_container.py`, `validate_pi.py` and `validate_installation.py`
  compared `/api/health` with exactly `{"status": "healthy"}` and so rejected a
  healthy server since the answer also carries `instance` (ADR-0032). They now
  check the status; unit tests cover it. `piworkerctl` was not affected.
- The committed `internal/webui/dist` predated the Backups page; it is
  re-embedded. Release builds were not affected (they embed a fresh build).
- Go 1.27.4, `golang.org/x/net` v0.62.0, `follow-redirects` ^1.16.3 and an
  `alpine:3.24` runtime image (see the dependency audit).

Observations from the agent smoke test, resolved the same day:

- `pw_get_draft` showed a Transform expression the agent had written in its
  draft as `"<redacted>"`, because the view hid every setting agents cannot
  read in a stored flow. ADR-0021 says what the agent wrote comes back as
  written; the view now withholds only the settings the draft kept of the
  base (`KeptJSON`). Regression tests: `TestGetDraftEchoesTheExpressionTheAgentWrote`
  and the hidden `url` case of `TestDraftEchoesComeBackVerbatim`.
- A call to a built-in tool the settings do not serve (`pw_deploy_flow` with
  agent deploy off) was refused saying the token lacked the `call` scope. The
  refusal now reads "No tool … is available to this token (flow tools need
  the call scope)", the same for unknown and hidden names.
- `pw_validate_flow` takes no `draftId`, by design: `pw_save_draft` refuses a
  draft with blocking errors, so a saved draft is valid, and `pw_get_draft`
  lists its issues. Step 2 of the smoke test now follows that order
  (validate, save, read, dry-run the draft).

Not covered here: browser setup and editing on the Pi, hardware nodes, the
draft release's own assets, public downloads, GHCR publication and anonymous
pulls, ARMv7 and x86_64 on real devices.

## v1.0.0-beta.1 publication and Pi install — 2026-11-15

`v1.0.0-beta.1` (tag at `bf31481`, CI run 57 green) was published as a
prerelease with its 11 assets; `piworkerctl`'s SHA-256 matched the local
candidate build. The owner installed it on the Raspberry Pi 4 (ARM64) with
the documented by-tag command, the real GitHub download path, and created
the administrator in the browser. Two findings:

- The first attempt failed with "Cannot update account": the Pi's disk was
  full (4 KB free). Nothing in the log said so; beta.2 logs the cause and
  keeps the code valid for a retry. PiWorker had not filled the disk.
- A 404 before publication was expected: draft assets are not public.

Still open on the Pi: trusting the CA, editing and deploying a flow in the
browser, and a reboot with the published package.
