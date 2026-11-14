# ADR-0001: Record architecture decisions

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer

## Context

PiWorker is being revived and extended (hardening, MCP, native HTTPS) largely
through AI-assisted changes. Rationale lives in chat sessions, PR descriptions
and commit messages, where it is hard to find and easy to contradict later.
Several decisions (CSRF without auth, webhook location, deploy semantics, agent
permissions) are security-relevant and must not be undone by accident.

## Options considered

### Option A: PR descriptions only
**Pros:** no extra files. **Cons:** not discoverable from the code; no status;
superseded decisions are invisible.

### Option B: One large architecture document
**Pros:** single place. **Cons:** history and alternatives get rewritten away;
merge conflicts; unclear what is decided vs. described.

### Option C: Lightweight ADRs in `docs/adr/`
**Pros:** one decision per file, immutable once accepted, explicit
supersession, reviewable in the same PR as the code. **Cons:** some writing
overhead per significant change.

## Decision

Use ADRs in `docs/adr/` following [the README](README.md) (when required,
numbering, statuses, supersession) and [the template](template.md). Long
designs live in `docs/design/` and are linked from ADRs. `CLAUDE.md` makes ADRs
a repository rule so assisted changes follow it too.

## Consequences

- Easier: understanding why constraints exist before changing them; reviewing
  security-relevant changes.
- Harder: significant changes need a short extra document.
- Revisit when: the overhead clearly outweighs the benefit.

## Action items

1. [x] Add README, template and the initial ADRs (0002–0013).
2. [x] Add the rule to `CLAUDE.md`.
