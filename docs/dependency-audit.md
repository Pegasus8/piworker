# Release candidate check — 2026-11-18 (v1.0.0-beta.2)

On `a423579` (Go 1.27.4, Bun 1.4.4):

- `govulncheck` v1.10.0 `-mode=binary` on the CGO release binary: **0
  reachable, 0 imported-package vulnerabilities**; one module-level finding,
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) (`golang.org/x/crypto/openpgp`
  is unmaintained), a package PiWorker does not import. (`./...` source mode
  could not load Go 1.27 code with this govulncheck release; the binary mode
  covers what ships.)
- `bun audit`: one **high** advisory, picomatch ≤4.0.7
  ([GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf),
  ReDoS through repeated extglobs), reached through Vite, vue-tsc, Tailwind
  and vue-router's build plugin. No fixed release exists yet (4.0.7 is the
  newest), so there is no override to add. picomatch only matches the
  project's own file globs at build time; it is not in the shipped bundle
  (`web/dist` has no trace of it). Recheck when a fix is published.

# Dependency addition — 2026-11-15: Anthropic Go SDK (LLM nodes)

Change: the LLM nodes (ADR-0038) add
`github.com/anthropics/anthropic-sdk-go/v2` **v2.9.0** (Apache-2.0), the
official client of the Claude API, used only by `internal/llm/anthropic.go`.
The OpenAI-compatible client is plain `net/http` and adds nothing.

New modules in `go.mod` (compare with `git diff origin/revival -- go.mod`),
all indirect through the SDK:

| Module | Version | License | Why it is there |
|---|---|---|---|
| `github.com/tidwall/gjson`, `sjson`, `match`, `pretty` | v1.18.0, v1.2.5, v1.1.1, v1.2.1 | MIT | The SDK's JSON decoding and request building |
| `github.com/invopop/jsonschema` | v0.14.0 | MIT | Schemas from Go types in the SDK's helpers |
| `github.com/bahlo/generic-list-go` | v0.2.0 | BSD-3-Clause | Through `invopop/jsonschema` |
| `github.com/buger/jsonparser` | v1.1.2 | MIT | Through `invopop/jsonschema` |
| `github.com/pb33f/ordered-map/v2` | v2.3.1 | Apache-2.0 | Through `invopop/jsonschema` |
| `go.yaml.in/yaml/v4` | v4.0.0-rc.2 | MIT and Apache-2.0 | Through `invopop/jsonschema` |
| `github.com/standard-webhooks/standard-webhooks/libraries` | v0.0.1 | MIT | The SDK's webhook verification (linked, not used by PiWorker) |
| `go.opentelemetry.io/otel`, `otel/metric`, `otel/trace`, `auto/sdk` | v1.33.0, v1.1.0 | Apache-2.0 | The SDK's tracing spans; PiWorker registers no tracer provider, so the global no-op one is used and nothing is exported |
| `github.com/go-logr/logr`, `go-logr/stdr` | v1.4.2, v1.2.2 | Apache-2.0 | Through OpenTelemetry |

The SDK's own `go.mod` also requires the AWS and Google Cloud SDKs (for its
`bedrock` and `vertex` packages); PiWorker does not import those packages,
so their modules are neither in PiWorker's `go.mod` nor built.
`go list -deps .` counts 55 packages from the new modules in the binary,
all pure Go (the MCP bridge `cmd/piworker-mcp` does not import them and
stays `CGO_ENABLED=0`).

How it is used: the client is built per call with
`option.WithoutEnvironmentDefaults()` (no `ANTHROPIC_*` environment
variables, profiles or custom headers: the LLM connection is the only source
of key and endpoint), `option.WithMaxRetries(0)` and PiWorker's own HTTP
client; error texts are taken from the API's message, capped and with the
key redacted, never from the SDK's error string.

Checks on this tree (Go 1.27.4, CGO enabled):

- `govulncheck` v1.9.0 `./...`: **0 reachable, 0 imported-package
  vulnerabilities**; 1 module-level finding, the known
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) in
  `golang.org/x/crypto` (`openpgp`, not imported). The database now lists a
  fix in a pseudo-version after v0.58.0
  (`v0.58.1-0.20261009181718-2f1f834330b2`); move to the next tagged
  x/crypto release when one exists.
- Not covered: the SDK's behaviour against the live API (the tests use a
  local fake server), and `bun audit` (the frontend gained no dependency).

---

# Release candidate audit — 2026-11-14 (`v1.0.0-beta.1`)

Candidate baseline: `0c7abfe` on `revival`; the remediation below is part of the
release-preparation commit. Earlier reports below are historical.

- `govulncheck` v1.9.0 (CGO enabled, Go 1.27.3) found **3 reachable
  vulnerabilities**: [GO-2026-6866](https://pkg.go.dev/vuln/GO-2026-6866) and
  [GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860) in `golang.org/x/net`
  v0.60.0 (`html`, fixed in v0.62.0) and
  [GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859) in `net/http` (HTTP/1
  server crash, fixed in Go 1.27.4). Module-only (not called):
  [GO-2026-6865](https://pkg.go.dev/vuln/GO-2026-6865) in
  `github.com/klauspost/compress` v1.19.2 and the known
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) (`x/crypto/openpgp`).
- Remediation: Go 1.27.4 (`go.mod`, both Dockerfiles, README), `x/net` v0.62.0,
  `klauspost/compress` v1.20.2 (and, through `go get`, `x/crypto` v0.58.0,
  `x/sys` v0.49.0, `x/sync` v0.24.0, `x/text` v0.43.0).
- Repeated `govulncheck ./...`: **0 reachable, 0 imported-package
  vulnerabilities**; only GO-2026-5932 remains at module level (no fixed release,
  package not imported). `govulncheck -mode=binary` on the ARM64 release binary
  agrees.
- `bun audit` found one **high** advisory, follow-redirects 1.16.1 through
  axios ([GHSA-mrwp-f3q8-3g9p](https://github.com/advisories/GHSA-mrwp-f3q8-3g9p)).
  The override is now `follow-redirects ^1.16.3`; `bun audit`: **no
  vulnerabilities** (258 packages). The braces advisory of the October report
  no longer appears.
- The production image's runtime base moved from `alpine:3.19` (out of support)
  to `alpine:3.24`, the release the Go builder stage already uses.
- After the updates: `go test -race ./...`, `go vet`, gofmt, frontend unit tests
  (243), typecheck, production build and the container smoke test pass.

Scope as before: no scan of the bundled SQLite C code or of container OS
packages beyond moving to a supported Alpine release.

# Candidate audit — 2026-10-23

Candidate baseline: `83534cf` on `revival`; remediation is included in the PR.
The September report below is historical, not a statement about current findings.

## Refreshed results

- `govulncheck` v1.9.0 with CGO enabled initially found 11 reachable standard-library
  vulnerabilities using Go 1.27.1. Updated Go (go.mod, production/development Docker
  images and README) to 1.27.3 and x/net from v0.59.0 to v0.60.0.
- Repeated `govulncheck ./...`: **0 reachable vulnerabilities, 0 imported-package
  vulnerabilities**, one module-only advisory not called by the application.
- `bun audit --json` initially flagged braces, follow-redirects,
  postcss-selector-parser, source-map-js and Vite. Updated packages within existing
  major ranges, including Vite 7.3.7, and pinned transitive correction ranges using
  overrides: follow-redirects >=1.16.1, source-map-js >=1.2.2 and
  postcss-selector-parser >=7.1.7. The parser override crosses its transitive major;
  TypeScript, production build and browser tests must remain validation gates.
- Remaining frontend finding: **high severity braces <=3.0.3**,
  [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm).
  Registry latest queried during this audit is 3.0.3, so no patched release was
  available. It is pulled by Tailwind through micromatch/chokidar for build/dev
  tooling. It is not served as a runtime dependency of the embedded static app.
  `bun audit` therefore exits 1; this is recorded and not suppressed. Replacing
  the Tailwind toolchain is a separate change, not an untested security override.
- Backend `go test -race ./...` passed after updates. Frontend unit tests
  (5 tests, 20 assertions), TypeScript and production build passed.

## Scope and reproduction

Run `govulncheck ./...` with CGO enabled and the compiler/SDK available, and
`cd web && bun audit --json`. On this Mac, the default Xcode compiler/Git shims
were blocked by an unaccepted license. Using the installed Command Line Tools
compiler, Git and SDK explicitly allowed the full CGO scan and Go race tests.
This audit does not cover bundled SQLite C advisories, container OS packages,
malicious packages, or application logic vulnerabilities; see the merge review
for the command-template finding and its subsequent correction. No claim of zero security risk is made.

---

# Dependency audit and remediation — revival

Date: 2026-09-12. Baseline revision: `5a71c88`. Remediation: `7ba0999` and `918f11f`.

This report records those revisions, not the current release candidate. Go was
subsequently moved to 1.27.1; see [Go modernization](go-modernization.md). Test
counts and scan results below are historical and must be refreshed for release.

## Remediation update

The baseline below is retained as the pre-update audit. The remediation
updates Go to 1.26.7 (including both Dockerfiles and README), x/net to 0.59.0,
x/crypto to 0.57.0, x/sys to 0.48.0 and x/text to 0.42.0. CI selects Go from go.mod.
The frontend now uses Axios 1.20.0, Vite 7.3.6 and plugin-vue 6.0.8; the lockfile
was re-resolved to include patched transitive packages within their declared ranges.

The frontend now uses Vue 3.5.42, Vue Flow core 1.48.2, Pinia 4.0.3,
Vue Router 5.3.1 and vue-tsc 3.3.11. The initial update exposed excessive type
instantiation in the flow store. The persisted Node/Edge models now select the
serializable identity, position, connection and application data fields rather
than inheriting Vue Flow's recursive rendering/component types. Runtime state
remains deeply reactive. Typechecking failed before this correction and passes
after it; a store regression test covers selected-node configuration updates,
position changes and connection removal.

Security checks before the updates failed. After the updates:

- `bun audit --json`: empty result, exit 0.
- `govulncheck .`: 0 symbol-level and 0 imported-package findings, exit 0.
- Remaining module-only advisory: [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)
  concerns the unmaintained `golang.org/x/crypto/openpgp` package. PiWorker does
  not import it; x/crypto is still required for bcrypt. There is no fixed version
  listed. This is documented, not suppressed or counted as an application finding.
- Backend race tests and `go vet`, frontend unit tests, TypeScript and all three
  Playwright scenarios pass (five frontend unit tests total).
- Full embedded release build and Linux ARM64 Docker build pass. A temporary
  production container passed UI, setup, login, session and logout smoke checks.

No exploit tests or application behavior changes were added for upstream fixes;
the failing security scans provide the before/after regression check, alongside
the existing behavior tests. Native SQLite and base-OS scanning remain outside
this audit's scope. Updates are grouped into backend security, frontend compatibility/security and
audit documentation commits.

## Historical baseline (before remediation)

## Scope and conclusion

The initial audit below was recorded before any dependency manifests or lockfiles
were changed. Its versions and recommendations are historical; see the remediation
results above for the current state.
The old default branch's 185 open Dependabot alerts are not a count of vulnerabilities
in revival: 162 refer to the removed `webui/frontend/package-lock.json`, and 23 to
that branch's `go.mod`. They should not be dismissed wholesale: revival has its own
current findings.

The highest-priority confirmed code path is RSS parsing through `gofeed` into
`golang.org/x/net/html` v0.47.0. The official database describes excessive CPU use
when parsing arbitrary HTML; govulncheck traces this to the executable through the
RSS trigger. Update x/net to a compatible patched version and exercise RSS parsing.
[Official advisory](https://pkg.go.dev/vuln/GO-2026-5028).

## Go

`govulncheck` v1.8.0, database last modified 2026-09-10, analyzed `.` as the actual
entry point (also scanned `./...` separately). The source scan used the local
Go 1.26.5 toolchain: 11 distinct advisories have symbol-level call traces. Static
reachability does not prove exploitability for every configuration; TLS/HTTP2,
ASN.1 and HTML correctness advisories require their individual preconditions.

| Advisory | Component | Fixed version reported |
|---|---|---|
| [GO-2026-5025](https://pkg.go.dev/vuln/GO-2026-5025) | golang.org/x/net | v0.55.0 |
| [GO-2026-5026](https://pkg.go.dev/vuln/GO-2026-5026) | stdlib | v1.26.6 |
| [GO-2026-5027](https://pkg.go.dev/vuln/GO-2026-5027) | golang.org/x/net | v0.55.0 |
| [GO-2026-5028](https://pkg.go.dev/vuln/GO-2026-5028) | golang.org/x/net | v0.55.0 |
| [GO-2026-5029](https://pkg.go.dev/vuln/GO-2026-5029) | golang.org/x/net | v0.55.0 |
| [GO-2026-5030](https://pkg.go.dev/vuln/GO-2026-5030) | golang.org/x/net | v0.55.0 |
| [GO-2026-5972](https://pkg.go.dev/vuln/GO-2026-5972) | stdlib | v1.26.6 |
| [GO-2026-6088](https://pkg.go.dev/vuln/GO-2026-6088) | stdlib | v1.26.6 |
| [GO-2026-6089](https://pkg.go.dev/vuln/GO-2026-6089) | stdlib | v1.26.6 |
| [GO-2026-6090](https://pkg.go.dev/vuln/GO-2026-6090) | stdlib | v1.26.6 |
| [GO-2026-6218](https://pkg.go.dev/vuln/GO-2026-6218) | stdlib | v1.26.6 |

The source scan also matched modules without symbol-level call traces, including
x/crypto, x/sys and x/text. These are upgrade candidates, not demonstrated attack
paths. One x/crypto advisory has no fixed version listed; upgrading alone cannot
be assumed to clear every advisory.

Toolchains must be distinguished: the locally cached Docker builder is Go 1.25.14,
not 1.26.5. A binary-mode scan of the existing `piworker:session-review` Linux image
reported 26 distinct dependency advisories and no standard-library advisories.
This image predates the Command fix but has the same dependency versions. Binary
mode lacks source call-chain precision and must not be treated as 26 proven
exploitable paths. Native/C SQLite and Alpine OS packages were not scanned.
Re-scan the exact release binaries and image after updates. The local compiler
needs a patched version (the scan reports Go 1.26.6 fixes); do not infer that
Docker has the same standard-library findings.

## Frontend and build tools

`bun audit --json` using Bun 1.3.14 matched 15 package names and 61 advisory/range
entries. Some entries share an advisory ID across different version ranges, so
61 is not a count of distinct exploitable vulnerabilities.

| Package | Audit entries | Reported severities |
|---|---:|---|
| axios | 29 | high, low, moderate |
| baseline-browser-mapping | 1 | moderate |
| brace-expansion | 4 | high, moderate |
| browserslist | 2 | high |
| defu | 1 | high |
| esbuild | 1 | moderate |
| follow-redirects | 1 | moderate |
| form-data | 1 | high |
| minimatch | 3 | high |
| nanoid | 5 | high |
| picomatch | 4 | high, moderate |
| postcss | 4 | high, moderate |
| postcss-selector-parser | 1 | low |
| rollup | 1 | high |
| vite | 3 | high, moderate |

### Production exposure versus tooling

- **Axios 1.13.2:** used by the browser API client. Many advisories concern its
  Node HTTP adapter (redirects, proxies, streamed uploads), which is not the
  PiWorker browser execution path. Browser/configuration advisories still warrant
  an update; the matched ranges extend up to versions below 1.18.0. No exploit
  was demonstrated. For the cookie-name ReDoS specifically, the advisory requires
  attacker-controlled XSRF cookie-name configuration; the current API client does
  not expose that setting. [Advisory](https://github.com/advisories/GHSA-hfxv-24rg-xrqf).
- **defu 6.1.4 and nanoid 5.1.6:** present through radix-vue 1.9.17, a UI dependency.
  They cannot be dismissed merely as dev dependencies. Actual retained bundle
  paths and attacker-controlled arguments still need validation. nanoid 3.3.11
  also appears through PostCSS. [defu advisory](https://github.com/advisories/GHSA-737v-mqg7-c878).
- **Vite 5.4.21, esbuild, Rollup, PostCSS and glob/browser-data tooling:** primarily
  development/build exposure. The release serves prebuilt static files from Go;
  it does not run the Vite server. Update these too, but do not describe Vite
  development-server file-read advisories as vulnerabilities of the Go listener.
  Moving beyond the affected Vite <=6.4.2 range requires checking the Vue plugin,
  Bun compatibility and the production build.

## Recommended remediation order

1. Update x/net and align supported, patched Go toolchains; validate RSS/HTTP
   behavior and rebuild every release target. Check new modules' minimum Go version.
2. Update Axios within its supported line; rerun login, CSRF, logout, recovery
   and event-stream tests. Review/update radix-vue's affected transitive packages.
3. Upgrade the frontend build toolchain in a separate change. Re-run typecheck,
   browser tests and the embedded build, then repeat both dependency audits.
4. Add repeatable vulnerability scanning to CI with explicit handling of
   non-reachable and tooling findings. Re-evaluate default-branch alerts after
   revival lands rather than closing historical alerts based only on this audit.

## Reproduction

```sh
# From repository root; JSON output must be interpreted for finding records.
# A zero process exit code with JSON output is not proof of no findings.
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -json .
cd web
bun audit --json
bun pm why defu
bun pm why nanoid
```

The historical baseline did not include fixes; the remediation section above
records the subsequent updates. This audit does not include penetration testing,
a full bundle reachability analysis, or OS/container vulnerability scanning. Results are a dated
snapshot and should be refreshed when choosing upgrade versions.

## October validation addendum

After remediation, all 34 Playwright browser tests passed, including the tests
that build and exercise the real Go backend. Go vet passed as well. The embedded
frontend was regenerated from the updated lockfile. The initial browser attempt
failed because Go invoked the Xcode Git shim; the successful run explicitly used
Command Line Tools Git/compiler/SDK. No test cases were disabled for the refresh.

## Command-template parser addendum

The placeholder correction adds `mvdan.cc/sh/v3` v3.14.1 for shell syntax parsing.
A full CGO-enabled govulncheck rerun after this addition found 0 reachable and
0 imported-package vulnerabilities; the one unused module-only finding remains.
The parser validates context; it does not execute the configured command.
