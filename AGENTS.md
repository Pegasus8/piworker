# Project instructions

## Test-driven development

Use TDD for all behavior changes in both the Go backend and the frontend:

1. Write a meaningful test describing the expected behavior or reproducing the
   bug before changing the implementation.
2. Run it and verify that it fails for the expected reason.
3. Implement the minimum change needed to pass the test.
4. Refactor while keeping the tests green, then run the relevant checks.

Cover user-visible behavior and important failure cases, rather than copying
implementation details into assertions. For frontend behavior, include suitable
automated tests and verify the integrated UI. For concurrency-sensitive backend
changes, run the race detector. Documentation-only changes require review for
accuracy; do not invent executable tests for prose.

Report which checks passed and any validation that could not be completed.

## CodeGraph

Prefer the configured CodeGraph tools for structural code questions: definitions,
callers, callees, impact, signatures, and focused context. Use `codegraph_context`
first and `codegraph_explore` for related source. Use native text search for
literal strings, comments, or details in files already inspected. Do not delegate
architecture exploration or re-check graph results through grep.

Pass the project path explicitly if workspace detection fails. If the project
has no index, ask before initializing it with `codegraph init -i`. Allow the
watcher time to index edits before querying them.
