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
- [x] Update the release notes for native HTTPS (ADR-0011): HTTPS on 8443 with a
  local CA trusted once per device, 8080 redirecting (health, CA download and
  webhooks served directly), `piworkerctl tls on|off`, and configurations with a
  lone `addr = ":8080"` refused at start (move to `addr = ":8443"`,
  `http_addr = ":8080"`). The HTTP-only limitation no longer applies.
- [x] Update the release notes for the AI agents work (ADR-0009…0023): agent
  tokens and tools, authoring through drafts, the agent deploy levels, versions and
  rollback, the bridge, and the behaviour changes under "Compatibility and limits".
- [ ] Choose the version and stable versus prerelease status; replace `Unreleased`
  only when publishing. `v*` tags currently prepare drafts without automatically
  classifying prereleases. Review the draft's prerelease flag explicitly.
- [ ] Validate migration from the old 2020 release if it is to be supported.
  Otherwise retain the explicit unvalidated-migration notice and require backups.
- [ ] After the first compatible stable release is public, update the README's
  prepublication notices and verify its download commands from a fresh machine.

Native HTTPS is implemented: a local CA generated on first start, automatic
server-certificate renewal, the "Trust this device" page and `piworkerctl tls`
([ADR-0011](adr/0011-native-https-with-local-ca.md), [operations](operations.md#https-and-the-local-ca)).
Encrypted-at-rest secrets and the remaining secret features tracked in
[TODO](../TODO.md) are deferred capabilities, not implemented features to
advertise in this release. Review whether the stated limits fit the intended
audience before choosing stable status.

## Candidate validation

- [ ] Record green Go formatting/vet/race tests, frontend unit/type/build/browser
  tests, installer tests, all Linux CGO builds and multi-platform Docker builds
  for the candidate commit. The tag workflow runs these before creating the draft.
- [ ] Refresh dependency checks against the candidate and record results. The
  [dependency audit](dependency-audit.md) is a dated snapshot, not an ongoing gate;
  native SQLite and container OS scanning were outside its scope.
- [ ] Inspect all three archives: `piworker-linux-<arch>` and `piworkerctl` must
  be present. Verify their entries in `SHA256SUMS`, and the standalone manager entry.
- [ ] Check the six bridge binaries in the draft, `piworker-mcp-linux-amd64`,
  `-linux-arm64`, `-linux-armv7`, `-darwin-amd64`, `-darwin-arm64` and
  `-windows-amd64.exe`, against their `SHA256SUMS` entries; `--version` must print
  the tag. Run one against the candidate (`--url https://…:8443/mcp --token-file …
  --ca-fingerprint …`) from Claude Desktop or `claude mcp add`, following
  [cmd/piworker-mcp/README.md](../cmd/piworker-mcp/README.md): `tools/list` answers,
  a wrong `--ca-fingerprint` stops it before any request, and a token file readable
  by others is refused.
- [ ] Run the agent authoring smoke test below against the candidate.
- [x] On the owner's Pi, test the real local-package installer lifecycle, failed
  update recovery, reboot and preserved session/variables/flow execution.
- [ ] Complete browser setup/editing and hardware checks on the Pi. Secret
  persistence across the real installer lifecycle is not asserted by this kit;
  the earlier isolated runner covered secret-name persistence across restart.
- [ ] Run `scripts/smoke_container.py` against the candidate image: HTTPS on 8443
  verified with the CA from the data volume, plain-HTTP health and CA download on 8080.
- [ ] Test anonymous container pull, first setup and persistent data after recreation.
  Published-image checks happen after publication; do not announce image availability
  until the image workflow succeeds.

Draft assets are not anonymously downloadable. Before publication, download them
as a maintainer for archive/binary checks. For the installer's real GitHub download
path, use a published prerelease with an explicit version on a test device; a
prerelease does not exercise the stable `latest` link. Final stable-link checks
must be completed after stable publication and before announcing availability.

### Agent authoring smoke test

On the candidate with HTTPS on, `[mcp] enabled = true` and `author_enabled = true`,
from Claude Code or the bridge (see the README's
[AI agents](../README.md#ai-agents-mcp) section and
[design §10](design/mcp.md#10-authoring-and-agent-deploy)):

1. As the admin, save and deploy a flow **Manual Inject → Transform → HTTP Request**
   (POST to a fixed `https://` URL you control, no secrets). Set **Agent deploy** to
   **Low-risk flows** (password), then create a token with `read`, `author` and
   `deploy`. `tools/list` shows the read, author and deploy tools; `prompts/list`
   shows `diagnose_flow` and `author_flow`.
2. **Draft and approve.** The agent reads the flow (`pw_get_flow`), changes the
   Transform expression with `pw_save_draft` (`baseVersion` = the version it read),
   and checks it with `pw_validate_flow` and `pw_dry_run` (the HTTP Request is a
   stub listed in `wouldExecute`; nothing is sent). The draft appears under
   **Agents → Drafts** with a badge. Approve it: the running flow is redeployed, and
   **History** lists the new version as approved, from the draft.
3. **Low refuses a destination change.** The agent saves a draft that changes the
   HTTP Request URL to another host and calls `pw_deploy_flow {flowId, draftId}`:
   refused (`forbidden`) naming `destination_changed` and `new_host`, without the
   URL; the flow keeps running the approved version and the draft stays waiting.
   Reject it with a reason; `pw_get_draft` shows the reason.
4. **Agent deploy and rollback.** The agent saves another Transform change and
   deploys it with `pw_deploy_flow {flowId, draftId}` (allowed at low). The flow
   card and the editor show **Deployed by agent** with the token's name and
   **Changed by agent**; **History** marks the version as not approved. **Roll back
   to version N** restores the approved version as a new version, redeploys it, and
   both marks are gone.
5. **Optimistic saves.** Stop the flow and open it in two editor tabs; save a change
   in one. Saving in the other answers 409 and offers **Reload**, without
   overwriting.
6. **Agents → Audit** lists the agent's `pw_save_draft` and `pw_deploy_flow` calls,
   the refused one included. Set **Agent deploy** back to **Off**: the deploy tools
   leave `tools/list` and `pw_deploy_flow` is refused.

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
