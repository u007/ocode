---
type: Gotcha
title: Linker-signed Go .app can never hold a TCC grant
description: |-
  A Go binary copied into a hand-built .app keeps only the toolchain's linker signature; macOS TCC treats that as unsigned, so Privacy & Security shows the app with its toggle ON while Accessibility, Screen Recording, Microphone and Full Disk Access grants never match. Fix: codesign the finished bundle ad-hoc; caveat: the cdhash changes on every rebuild and orphans the grant.
tags:
  - gotcha
  - macos
  - desktop
  - codesign
  - tcc
  - permissions
  - accessibility
  - screen-recording
timestamp: 2026-11-06T10:07:24Z
---
# Linker-signed Go .app can never hold a TCC grant

A Go binary copied into a hand-built `.app` is only **linker-signed** by the Go
toolchain. macOS TCC treats that as *unsigned*: the app appears in System
Settings → Privacy & Security with its toggle ON, but the grant never matches.
This is not a stale UI or a missing prompt — there is no signature identity for
TCC to bind the grant to.

## Symptom triad (all observed together, all while the pane showed the app enabled)

- `osascript -l JavaScript -e '$.AXIsProcessTrusted()'` returns `false` from the
  app's own child processes.
- `screencapture` through ocode's `computer` tool fails with
  `could not create image from display`.
- `agent-desktop status` (a CLI launched from the app) reports
  `"accessibility":"denied"`.

System Settings listed `ocode`, `ocode-desktop` and `ocode.app`, all ON. A full
app restart changed nothing, and removing then re-adding the entries did not
help. That last detail is the tell: the toggle is keyed to an identity the
running process never presents.

The same failure applies to every TCC grant, not just the two above —
Microphone, Full Disk Access, Files & Folders and Automation all resolve by code
signature.

## Diagnose it with `codesign -dv`

Before the fix, on the bundled binary:

```text
Identifier=a.out
CodeDirectory v=20400 size=669246 flags=0x20002(adhoc,linker-signed)
Signature=adhoc
Info.plist=not bound
Sealed Resources=none
```

`flags=...linker-signed`, `Info.plist=not bound` and `Sealed Resources=none`
mean there is no bundle identity and no sealed-resource hash for TCC to match.
If you see those three, stop looking at prompts.

## The fix

`scripts/bundle-macos.sh:116` re-signs the finished bundle, after the
`Info.plist` and every resource are in place and before the final `echo` at
`scripts/bundle-macos.sh:118`:

```bash
codesign --force --sign - --identifier com.u007.ocode "$APP"
```

The inline comment at `scripts/bundle-macos.sh:109-115` records why. The script
already runs under `set -euo pipefail` (`scripts/bundle-macos.sh:8`), so a
signing failure stops the build instead of shipping an app whose grants are
silently inert.

After the fix:

```text
Identifier=com.u007.ocode
CodeDirectory v=20400 size=167495 flags=0x2(adhoc)
Signature=adhoc
Info.plist entries=9
Sealed Resources version=2 rules=13 files=9
```

`codesign --verify --strict` then reports `valid on disk` and `satisfies its
Designated Requirement`.

`/Applications/ocode.app` is a symlink to `bin/ocode.app` in this repo, so
re-signing the build output covers the installed app too.

## Caveat: an ad-hoc signature is keyed to the cdhash

Every rebuild produces a new cdhash, so the ad-hoc signature changes and **the
existing TCC grant is orphaned** — while the toggle still reads ON. Recover by
removing the entry with `−` and re-adding it, or by re-accepting the prompt.
A stable self-signed certificate or a Developer ID identity would end that
churn; which one to use is the user's choice, not a default this script bakes
in.

## Status honesty

The re-sign is the correction and it produces a valid signature. At the time of
writing **no TCC grant had yet been observed working end-to-end after the
re-sign** — the app had not been restarted and re-granted. Treat the signature
repair as verified, and the final "grant now applies" confirmation as pending.

## Where the denied paths surface

- `internal/sysperm/catalog_darwin.go:20` — the Accessibility check runs
  `$.AXIsProcessTrusted()`.
- `internal/computer/permissions_darwin.go:110` — the Screen Recording probe
  runs `screencapture`.
- `internal/computer/driver_darwin.go:44` — the screenshot action itself runs
  `screencapture`.

See [Computer use](../computer-use.md) (the macOS prerequisites in
`docs/computer-use.md:124-130`) for the grant list these paths require.
