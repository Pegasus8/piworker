# Dependency audit and remediation — revival

Date: 2026-09-12. Baseline revision: `5a71c88`. Remediation: `7ba0999` and `918f11f`.

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

This audit does not include penetration testing, a full bundle reachability
analysis, OS/container vulnerability scanning, or fixes. Audit results are a dated
snapshot and should be refreshed when choosing upgrade versions.
