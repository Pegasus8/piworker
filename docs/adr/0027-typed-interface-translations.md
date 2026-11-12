# ADR-0027: Typed interface translations without an i18n library

**Status:** Proposed
**Date:** 2026-11-12
**Deciders:** maintainer

## Context

The editor was English only, with every string inline in components and in
the presentation modules (`lib/drafts.ts`, `lib/agentAccess.ts`, …). Users
asked for Spanish, and more languages may follow. The UI is embedded in a
single binary that runs on a Raspberry Pi; the bundle should stay small and
the build simple. Text that the server returns (API errors, node types,
their documentation and config schemas) comes from the Go registry.

## Options considered

### Option A: vue-i18n

| Dimension | Assessment |
|---|---|
| Complexity | Medium (plugin, message syntax, compiler or runtime-only build) |
| Pi resources | ~40 kB gzipped more in the bundle |
| Maintenance | Keys are strings: a missing translation shows at run time only |

**Pros:** pluralisation rules, lazy loading, a known API.
**Cons:** a significant dependency; string keys and message syntax that the
typecheck does not see; its message compiler needs `unsafe-eval` unless the
runtime-only build and precompiled messages are set up.

### Option B: typed message objects (chosen)

| Dimension | Assessment |
|---|---|
| Complexity | Low (one module, plain objects) |
| Pi resources | No dependency; only the catalogs |
| Maintenance | `en` is the source of truth; `es` is typed `typeof en`, so a missing, extra or mistyped message fails `vue-tsc` |

**Pros:** autocompletion and refactoring work on messages; parameterised
messages are TypeScript functions, so plurals and word order are code.
**Cons:** every language is in the bundle (fine for two); no translator
tooling.

## Decision

- `web/src/i18n/index.ts` holds the current locale (`ref`), `messages`
  (a `computed` of the active catalog) and `msg()` for plain modules, plus
  `formatDate`/`formatDateTime`/`formatNumber`/`formatRelative` on `Intl`
  with the active locale.
- Catalogs live in `web/src/i18n/<locale>/<area>.ts`; `en` is the source,
  every other locale is typed `typeof` its English counterpart. Messages with
  values are functions.
- Components read `messages`; modules call `msg()` when they build a string,
  never at import time, so switching language applies everywhere at once.
- The locale is the saved choice (`localStorage`), else the browser's first
  supported language, else English. `<html lang>` follows it. The language
  is chosen in the navigation and on the sign-in page.
- New UI text goes into the catalogs, in every language, in the same change.
- Out of scope: text the server sends (API errors, node names, node
  documentation and config schema descriptions) stays English for now.

## Consequences

- Easier: adding a language is a typed copy of `en`; untranslated text
  cannot ship silently.
- Harder: plurals are written per message instead of declared once.
- Revisit when: the server-side catalog (node types and their documentation)
  needs translating, or more than a few languages make bundling all of them
  costly.

## Action items

1. [x] Infrastructure, language switcher and a test that every message exists in every language.
2. [x] Spanish for every screen of the editor.
3. [ ] Decide how node types and their documentation are translated (server side).
