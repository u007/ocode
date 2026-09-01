---
type: Decision
title: Shell Sandbox Backend Availability Status
description: Decision on handling sandbox backend availability (available, unsupported, unavailable) with fail-closed requirements and API status definitions.
tags:
  - architecture
  - sandbox
  - security
  - shell
timestamp: 2026-08-31T17:26:30Z
---
# Shell Sandbox Backend Availability

## Status Classification

Each sandbox backend (macOS `sandbox-exec`, Linux `bwrap`/Landlock, Windows equivalent) must be classified at startup:

| Status | Meaning | Behavior |
|--------|---------|----------|
| `available` | Binary present and functional | Use for confinement |
| `unsupported` | Platform has no sandbox backend | Warn user, degrade gracefully |
| `unavailable` | Expected backend missing or broken | **Fail-closed** — refuse sandbox mode |

## Fail-Closed Requirement

When the requested sandbox mode requires a backend that is `unavailable`, ocode **must not** fall back to unrestricted execution. The startup sequence must:

1. Probe the backend (e.g. `sandbox-exec -h`, `bwrap --version`)
2. Record status in the session config
3. If status is `unavailable` and sandbox mode is `strict`, refuse to start the agent turn with a clear error message

A `unsupported` status (platform genuinely has no equivalent) may allow a degraded "best-effort" mode with explicit user acknowledgment, but never silently drop confinement.

## API Status Definitions

The `/api/sandbox/status` endpoint (or equivalent internal query) returns:

```json
{
  "backend": "sandbox-exec | bwrap | none",
  "status": "available | unsupported | unavailable",
  "features": ["network-restrict", "fs-read", "fs-write"],
  "message": "Optional human-readable note"
}
```

The `features` array advertises which confinement dimensions the backend supports. The permission manager consults this to decide whether a requested rule can be enforced or must be rejected.

## Gotcha: Start-Time Probe vs Runtime Failure

The probe at startup is a point-in-time check. A backend that was `available` at session start can become unavailable mid-session (e.g. `bwrap` binary removed, kernel lockdown). The sandbox wrapper should handle exec-time failures gracefully — surface the error to the agent loop, don't silently execute unconfined.