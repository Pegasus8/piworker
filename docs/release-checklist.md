# First revival release checklist

Review date: 2026-09-14. Remote baseline: `03f28f9` on `revival`, plus local fixes.
This is a readiness review, not evidence that a release has been validated or
published. Recheck each item against the exact commit chosen for the tag.

## Confirmed findings to close

- [x] **ShellCheck correction:** the [baseline run](https://github.com/Pegasus8/piworker/actions/runs/34785604572)
  failed on ShellCheck SC2015 in `scripts/piworkerctl`. The local review replaces
  that conditional with explicit `if`; ShellCheck and all 14 installer tests pass
  on macOS and Ubuntu 22.04. A new remote CI run is still required after pushing.
- [x] **Mobile browser test:** the [preceding run](https://github.com/Pegasus8/piworker/actions/runs/34784573181)
  passed 32 tests and failed `web/e2e/node-playground.spec.ts:99`: reopening a node
  timed out because it was outside the viewport. A deterministic resize regression
  reproduced this locally; resizing now refits the graph. All 34 browser tests pass.
- [x] **Version identification:** `-version`/`--version` now reports the injected
  build version before config/database initialization. Make, CI and Docker publish
  pass the version; subprocess tests cover injection and no startup side effects.
- [x] **Health semantics:** an unhealthy closed store now returns HTTP 503, making
  HTTP-based installer/container checks fail correctly. The regression test failed
  before the correction and passes afterward.
- [x] **Pi execution:** the owner reported successful process and transient-systemd
  runs on `rpi4`, including flow execution, restart persistence and account recovery.
  See the supplied-output evidence in the [validation report](release-validation.md).
- [x] **Real install and reboot validation:** the owner supplied the complete
  `resume-report.json` from `rpi4`: all 17 checks passed, including install, failed
  update rollback, update, uninstall/reinstall, boot enablement and preserved
  session/data/flow execution after reboot. Packages were supplied locally.
- [ ] Record the Pi OS and selected architecture; validate other advertised
  architectures too, or explicitly narrow support.

See the [local validation report](release-validation.md) for completed checks,
environment limits and generated packages, and the
[Pi checklist](raspberry-pi-validation.md) for the owner's final hardware validation.

## Documentation and compatibility

- [x] Describe native installation/update/uninstall, retained data and supported CPUs.
- [x] Correct the standalone archive executable name and document checksum verification.
- [x] Explain native service account recovery, stopped-database backup and manual restore.
- [x] Clarify that bootstrap credentials are optional and shell exports do not
  configure the native systemd service.
- [x] Prepare [release notes](../CHANGELOG.md), including HTTP, secret storage and
  legacy migration limitations.
- [ ] Choose the version and stable versus prerelease status; replace `Unreleased`
  only when publishing. `v*` tags currently prepare drafts without automatically
  classifying prereleases. Review the draft's prerelease flag explicitly.
- [ ] Validate migration from the old 2020 release if it is to be supported.
  Otherwise retain the explicit unvalidated-migration notice and require backups.
- [ ] After the first compatible stable release is public, update the README's
  prepublication notices and verify its download commands from a fresh machine.

HTTPS assistance, encrypted-at-rest secrets and additional secret-reference
features are tracked in [TODO](../TODO.md). They are deferred capabilities, not
implemented features to advertise in this release. Review whether the stated
limits fit the intended audience before choosing stable status.

## Candidate validation

- [ ] Record green Go formatting/vet/race tests, frontend unit/type/build/browser
  tests, installer tests, all Linux CGO builds and multi-platform Docker builds
  for the candidate commit. The tag workflow runs these before creating the draft.
- [ ] Refresh dependency checks against the candidate and record results. The
  [dependency audit](dependency-audit.md) is a dated snapshot, not an ongoing gate;
  native SQLite and container OS scanning were outside its scope.
- [ ] Inspect all three archives: `piworker-linux-<arch>` and `piworkerctl` must
  be present. Verify their entries in `SHA256SUMS`, and the standalone manager entry.
- [x] On the owner's Pi, test the real local-package installer lifecycle, failed
  update recovery, reboot and preserved session/variables/flow execution.
- [ ] Complete browser setup/editing and hardware checks on the Pi. Secret
  persistence across the real installer lifecycle is not asserted by this kit;
  the earlier isolated runner covered secret-name persistence across restart.
- [ ] Test anonymous container pull, first setup and persistent data after recreation.
  Published-image checks happen after publication; do not announce image availability
  until the image workflow succeeds.

Draft assets are not anonymously downloadable. Before publication, download them
as a maintainer for archive/binary checks. For the installer's real GitHub download
path, use a published prerelease with an explicit version on a test device; a
prerelease does not exercise the stable `latest` link. Final stable-link checks
must be completed after stable publication and before announcing availability.

## Publication sequence

1. Select the candidate commit on `revival` and obtain green CI. Both workflows
   and the manager must exist in the commit being tagged.
2. Choose the release version, finalize the notes, push a `v*` tag at that commit,
   and wait for the draft-release job to succeed.
3. Review draft assets, notes and prerelease/latest settings. Publish the draft
   manually through GitHub with a maintainer account. This triggers `Publish image`;
   publication through a workflow's `GITHUB_TOKEN` will not trigger another workflow.
4. Verify the GHCR job and all advertised image platforms; make the package public
   for anonymous pulls. A failed image build does not retract the GitHub release.
5. Verify public asset downloads/checksums and install/update on a fresh machine.
   For a stable release, verify both GitHub's latest installer and GHCR `latest`.
6. Update the release checklist with links to actual evidence and publish the
   user-facing announcement only after the advertised artifacts work.

The default branch is currently `master`, whose workflow directory contains only
`golangci-lint.yml`. Decide separately when to merge revival or change the default
branch so the repository homepage and dependency monitoring reflect the new code.
This is not a prerequisite for the tag-based `release` event: it uses the tagged
commit. Manual `workflow_dispatch` availability does require the workflow on the
default branch. See GitHub's [event reference](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#release)
and [token-trigger rules](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow).
