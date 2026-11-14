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

## Launch gap review (2026-11-11)

Reviewed at `a5e2022` on `revival` (CI green for that commit: runs
[27](https://github.com/Pegasus8/piworker/actions/runs/42682905527) and
[28](https://github.com/Pegasus8/piworker/actions/runs/42682919812)). The code
is feature-complete for a first release; what remains is validation of the
candidate, publication and repository housekeeping. In order:

**Blocking the release**

- [x] The first publication is a **prerelease** (decided 2026-11-11). It is
  the only way to exercise the installer's real download path before a stable
  release moves `latest`. The tag workflow now drafts a tag with a hyphen as a
  prerelease, and the README documents installing by tag.
- [ ] Choose the version number. The only earlier tag is `v0.1.0-alpha.1`
  (2020), so any `v0.2.0-…` or `v1.0.0-…` prerelease tag sorts after it.
- [ ] Run every unchecked item under [Candidate validation](#candidate-validation)
  against the tagged commit: archives and checksums, the six bridge binaries,
  `scripts/smoke_container.py`, the agent authoring smoke test, browser setup
  and editing on the Pi.
- [x] Hardware nodes (GPIO Edge/Write, I2C Read, DS18B20) have no recorded run
  on a real Pi: they ship marked **experimental** (decided 2026-11-11) in the
  editor's node list and documentation card, the node reference, the README,
  the user guide and the release notes. Drop the mark once each is validated.
- [ ] Record the Pi OS and architecture already validated and decide on the
  other advertised CPUs (ARMv7, x86_64): validate or narrow the claim.
  x86_64: the automated checks passed natively on 2026-11-11
  ([report](release-validation.md#x86_64-native-validation--2026-11-11));
  the systemd installer and a reboot remain to be run on a real PC.
- [x] Migration from the 2020 release is **not supported** (decided
  2026-11-11); the README, the user guide and the release notes say so.

**Blocking the announcement**

- [ ] Nothing a new user can follow works until a release exists: the only
  published release is `v0.1.0-alpha.1` (2020), so
  `releases/latest/download/piworkerctl` and the `ghcr.io` image in the README
  and the user guide do not resolve to the revival yet.
- [ ] PR #288 (`revival` → `master`) is still a draft. Until it merges (or the
  default branch changes) the repository homepage shows the 2020 README, and
  Dependabot keeps opening pull requests against the removed `webui/frontend`
  (about twenty are open). Merge, then close those.
- [ ] After publication, remove the prepublication notices from the README and
  from `docs/guide/getting-started.md`, and rerun their commands on a fresh
  machine.

**Housekeeping worth doing before announcing**

- [x] Triaged the 29 open issues from 2019–2021 (2026-11-11). Closed 20, each
  with a note: 16 delivered by the revival (#1, #22, #43, #58, #65, #80, #81,
  #89, #91, #93, #137, #165, #166, #167, #169, #175) and 4 that only applied to
  the 2020 code (#113, #159, #164, #176). Nine stay open because they are
  still valid: #92 and #168 (node ideas), #112 (user management), #128
  (automatic backups), #129 (automatic updates), #170 (design, theme choice),
  #171 (accessibility), #172 (translations) and #249 (files created relative
  to the working directory).
- [x] [`SECURITY.md`](../SECURITY.md) asks for reports through GitHub's
  private vulnerability reporting.
- [ ] Enable **Private vulnerability reporting** in the repository settings
  (Settings → Advanced Security); without it the link in `SECURITY.md` does
  not accept reports.
- [x] `make fmt-check`, `make fmt` and the pre-commit hook use the `gofmt` of
  the Go version in `go.mod` instead of the first one on `PATH`.

**Documentation (done in this review)**

- [x] User guide under [`docs/guide`](guide/README.md): getting started with
  screenshots, concepts, recipes, AI agents step by step, troubleshooting.
- [x] Node reference generated from the registry and kept current by
  `TestNodeReferenceDocIsCurrent`.
- [x] README: links to the guide, the node names shown in the editor, the
  endpoints missing from the API reference (import/export/duplicate, manual
  injection, runs and events, flow variables, node test, metrics), and the
  claim that `/api/node-types` returns `risk` (it does not).
- [ ] The README is still about 1,400 lines of mixed user, operator and
  developer reference. After the release, move configuration, HTTPS and the
  AI agents reference into `docs/` and keep the README as an overview.

**Known limits to state, not to fix, for this release**

Secrets are not encrypted inside the database (decided in ADR-0030: the
database file is kept private and full-disk encryption is recommended); one
administrator per installation. Server texts (node documentation, API
errors) stay in English while the interface is English and Spanish. See
[TODO](../TODO.md).

Since the review: variables, connections and secrets can be exported and
restored (secret values encrypted, ADR-0031); an undefined secret in an
optional credential is a deploy warning (ADR-0028); the editor shows each
node's risk; MQTT nodes take named connections (ADR-0029).

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
- [ ] Choose the version; replace `Unreleased` only when publishing. The first
  publication is a prerelease: a `v*` tag with a hyphen is drafted with the
  prerelease flag. Still review the flag on the draft before publishing.
- [x] Migration from the old 2020 release is not supported; the notices say so.
- [x] Provide a task-oriented [user guide](guide/README.md) besides the README
  reference, with a node reference generated from the registry.
- [ ] After the first compatible stable release is public, update the prepublication
  notices in the README and the user guide and verify its download commands from a fresh machine.

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
