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
