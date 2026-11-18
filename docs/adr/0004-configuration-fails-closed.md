# ADR-0004: Configuration fails closed

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer

## Context

`PIWORKER_AUTH=""` (common with `${VAR}` in compose files) or any unrecognised
value disabled authentication. A `-config` path with a typo silently loaded
defaults, and unknown TOML keys (e.g. `atuh = false`) were ignored. The legacy
`jwt_secret` setting had no effect but was still accepted.

## Options considered

### Option A: Lenient parsing with warnings
**Pros:** never blocks startup. **Cons:** warnings in a systemd journal are not
read; insecure fallbacks persist.

### Option B: Fail closed at startup
| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | Misconfiguration cannot silently weaken the server |
| Maintenance | Users must fix config errors before the service starts |

## Decision

Startup fails, naming the problem, when: a boolean environment variable is
empty or not one of true/false, t/f, 1/0, yes/no, on/off (case-insensitive,
surrounding whitespace ignored); a config path other than the default
`piworker.toml` (from `-config` or `PIWORKER_CONFIG`) does not exist (a missing
`piworker.toml` is tolerated); the TOML file has unknown keys. Removed settings
are therefore errors in the TOML file (`jwt_secret`) and as flags
(`-jwt-secret`); unknown `PIWORKER_*` environment variables are not checked, so
the former `PIWORKER_JWT_SECRET` is ignored. New settings must use strict
parsing, be validated explicitly and have tests; a config validation function
(cross-field checks such as conflicting listener ports or MCP without auth) is
added with the MCP/HTTPS work.

## Consequences

- Easier: trusting that what the file says is what runs.
- Harder: upgrades that remove settings require editing the config.
- Revisit when: settings are deprecated after a release — then deprecate with a
  one-release grace period instead of an immediate error.
