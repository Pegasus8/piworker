# Go 1.27 modernization

PiWorker requires Go 1.27.1 in go.mod and both Dockerfiles. CI reads go.mod.
The version was verified against [official stable downloads](https://go.dev/dl/).

## Applied APIs

- Authentication decodes bounded request bodies with `encoding/json/v2.UnmarshalRead`.
  Duplicate JSON names, invalid UTF-8 and multiple top-level values are rejected.
  Valid Unicode credentials and surrounding whitespace remain supported.
  Responses and stored flow data retain the `encoding/json` v1 API and semantics.
- The router worker pool uses `sync.WaitGroup.Go` (introduced in Go 1.25) to register,
  launch and account for each worker in one operation. Locking, shutdown ordering,
  queue policy and node execution behavior are unchanged.

Tests for malformed authentication JSON failed before the decoder change.
No broad migration of persisted JSON or public API responses was attempted:
JSON v2 has different defaults, which need deliberate compatibility decisions.

[Go 1.27 release notes](https://go.dev/doc/go1.27) also describe runtime allocation
improvements and the new implementation behind the v1 JSON API. These are compiler/
runtime benefits; no PiWorker benchmark was run, so no specific speedup is claimed.
The goroutine-leak profile is available in the runtime, but no new profiling endpoint
has been exposed by PiWorker.

## Validation

- `go test -race ./...` and `go vet ./...` passed on macOS with Go 1.27.1.
- All three Playwright authentication/HTTP-warning scenarios passed.
- Linux ARM64 production Docker image compiled with Go 1.27.1.
- `govulncheck .` reported no affected symbols or imported packages; the previously
  documented module-only OpenPGP advisory remains.
