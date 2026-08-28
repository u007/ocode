---
type: Gotcha
title: Debug Instrumentation Ships Unconditionally
description: 'Process gotcha: temporary Date.prototype instrumentation ships unconditionally in production builds, causing global prototype mutation, altered date behavior, and authenticated network requests.'
tags:
  - frontend
  - debug
  - prototype-mutation
  - production-safety
  - gotcha
  - fixed
timestamp: 2026-08-28T07:01:08Z
---
# Debug Instrumentation Ships Unconditionally

## What happened

A code review of `debug.ts` discovered temporary `Date.prototype` instrumentation (monkey-patching) that shipped **unconditionally** — it runs in production builds, not just development. The affected code:

- Mutates `Date.prototype` globally, altering date behavior for the entire application.
- Sends authenticated network requests to an external endpoint for diagnostic telemetry.
- Has no development-flag gate or removal pathway before release.

## Why this matters

Unconditional prototype mutation is a **process gotcha**: the code was intended as a temporary diagnostic aid but ended up in production. The blast radius is wide because `Date` is one of the most widely used built-ins:

1. **Global prototype mutation** — every date object in the application (and any third-party library loaded in the same runtime) is affected.
2. **Altered date behavior** — downstream code that depends on standard `Date` semantics may produce incorrect results silently.
3. **Authenticated network requests in production** — the instrumentation makes network calls gated only by the presence of an auth token, not by environment, leaking data from every production client.

## Prevention

- **Development-flag gating** — any diagnostic instrumentation that modifies built-in prototypes must be wrapped in a `process.env.NODE_ENV !== 'production'` / `import.meta.env.DEV` guard (or equivalent). If the flag is absent at build time, dead-code elimination removes the code.
- **Removal before release** — temporary diagnostic code should have a tracking issue or TODO comment and be removed before merging to the release branch. Consider using a lint rule or CI check that flags `Date.prototype` assignments outside test files.
- **Review checklist** — when reviewing changes that modify built-in prototypes, explicitly verify the production guard and ask: "does this need to ship?"

## Detection

Run a grep for `Date.prototype` in source files (excluding tests and `node_modules`) as a quick sanity check before merge:

```bash
grep -rn "Date\.prototype" src/ --include='*.ts' --include='*.js' | grep -v node_modules | grep -v '\.test\.'
```

Any matches in non-test source should be gated or removed.

## Status (2026-08-28)

This bug has been **fixed and removed**. The temporary `Date.prototype` instrumentation was deleted from `web/src/debug.ts`. A grep for `Date.prototype` in `web/src/` now returns no matches. The `debug.ts` file retains its core caching functionality without the prototype mutation side-effect.