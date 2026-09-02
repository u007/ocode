---
type: Gotcha
title: Auto-Permission Prompt — TOCTOU Install Race
description: 'TOCTOU race in auto-permission prompt install: concurrent older build can downgrade the bundled gatekeeper prompt.'
tags:
  - auto-permission
  - gotcha
  - race-condition
  - prompt
timestamp: 2026-09-02T07:06:55Z
---
## TOCTOU Race in `InstallAutoPermissionPrompt`

The auto-permission prompt install path in `config/auto_permission_prompt.go`
has a Time-of-Check-Time-of-Use (TOCTOU) race: `InstallAutoPermissionPrompt`
checks whether the current prompt is already installed and up-to-date, then
writes the prompt file in a separate step — without holding a file lock across
the entire check → write sequence.

### The race window

```
Process A (newer build)          Process B (older build)
─────────────────────────        ─────────────────────────
check prompt status → stale
                                 check prompt status → stale
                                 writes older prompt (v1.5.0)
writes newer prompt (v1.8.0)
```

Both processes see the prompt as needing an update. The second writer
silently overwrites the first's prompt, which may be a newer version.
When a concurrent older ocode build finishes second, the bundled
gatekeeper prompt is **downgraded** — the security tightening from
`v1.8.0` (blanket OS temp auto-permission) is replaced with the older,
more restrictive prompt.

### Impact

- A concurrent older ocode instance can overwrite a newer prompt with an
  older version without error or warning.
- The downgrade is invisible: the installed prompt file looks valid, just
  semantically wrong.
- This is the same class of bug already documented for the kaizen index
  (`gotchas/kaizen-index-prompt-version-lock.md`): serialise with
  `filelock.WithFileLock`.

### Fix

Wrap the entire `check → install` sequence in a cross-process advisory
lock (`filelock.WithFileLock`) so only one writer can evaluate and
install at a time. The lock must span from the status check through the
write, not just the write itself — releasing between check and write
leaves the race open.

### Files

- `config/auto_permission_prompt.go` — `InstallAutoPermissionPrompt`

### Related

- `gotchas/kaizen-index-prompt-version-lock.md` — same class, kaizen index
- `gotchas/plugin-auto-permission-security.md` — the policy change the
  prompt enforces