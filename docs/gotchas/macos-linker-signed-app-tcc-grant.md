---
type: Gotcha
title: Linker-signed Go .app can never hold a TCC grant
description: |-
  A Go binary copied into a hand-built .app keeps only the toolchain's linker signature, which macOS TCC treats as unsigned, so Privacy & Security shows the app enabled while Accessibility, Screen Recording, Microphone and Full Disk Access grants never match. Fix: re-sign the finished bundle (OCODE_CODESIGN_IDENTITY, else an auto-detected ocode-dev identity, else ad-hoc); an ad-hoc requirement is the cdhash and is orphaned on every rebuild, while a certificate-anchored requirement survives. Windows and Linux need no per-app grant.
tags:
  - gotcha
  - macos
  - desktop
  - codesign
  - tcc
  - permissions
  - accessibility
  - screen-recording
timestamp: 2026-11-16T02:31:42Z
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

## The fix: re-sign the finished bundle

`scripts/bundle-macos.sh:136-148` re-signs after the `Info.plist` and every
resource are in place, resolving the identity in a deliberate order:

1. `OCODE_CODESIGN_IDENTITY` if set (`scripts/bundle-macos.sh:136`).
2. Else a dedicated `ocode-dev` identity visible to
   `security find-identity -p codesigning`, captured first then matched in the
   shell rather than through a pipeline (`scripts/bundle-macos.sh:140-143`).
3. Else ad-hoc: `codesign --force --sign -` (`scripts/bundle-macos.sh:147`).

Only the `ocode-dev` name is auto-detected on purpose, so installing an
unrelated certificate never changes ocode's signature behind the user's back
(`scripts/bundle-macos.sh:129-131`).

The script then reads the result back (`scripts/bundle-macos.sh:154`) and prints
`signed with:` and `designated requirement:` (`scripts/bundle-macos.sh:156-157`),
warning when the requirement is `cdhash …` (`scripts/bundle-macos.sh:158-168`).
After the requirement it also prints a **trust note when the chosen identity is
not macOS-trusted** (`scripts/bundle-macos.sh:170-178`): `security find-identity
-v` does not list a self-signed identity — it reports `CSSMERR_TP_NOT_TRUSTED` —
so instead of letting "console fine, grants dead" recur, the build says so,
naming the identity and pointing at Keychain Access or `make signing-cert`. That
is expected for the self-signed `ocode-dev` and deliberately accepted; the note
is the hint to check when a grant still fails after re-granting.
The script runs under `set -euo pipefail` (`scripts/bundle-macos.sh:8`), so a signing
failure stops the build instead of shipping an app whose grants are silently
inert. `scripts/bundle-macos.sh:10-15` now refuses to run off macOS before it
deletes the target bundle, so `make signing-cert` and `make desktop-app` fail
with a clear message on Windows and Linux.

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

## Make the requirement stable: `make signing-cert`

Ad-hoc signing still repairs the signature, but its designated requirement is
the cdhash. For a requirement that survives rebuilds, create a one-time
self-signed code-signing identity:

```bash
make signing-cert        # ./scripts/create-dev-signing-cert.sh
```

The target (`Makefile:134-137`) runs `scripts/create-dev-signing-cert.sh`, which
generates a self-signed `ocode-dev` certificate (`openssl req`,
`scripts/create-dev-signing-cert.sh:37-46`), exports it to PKCS#12, imports it
into the login keychain with codesign as the only allowed consumer
(`security import -T /usr/bin/codesign`, `scripts/create-dev-signing-cert.sh:62`),
then proves the key by signing a copy of `/bin/echo`
(`scripts/create-dev-signing-cert.sh:64-74`). It is idempotent: an existing
identity is detected and the script exits 0
(`scripts/create-dev-signing-cert.sh:19-29`).

After it succeeds, run `make desktop-app` and re-grant the OS permissions once;
later rebuilds keep the grants.

Gotchas that cost time to rediscover:

- **`security find-identity -v` does not list a self-signed identity.** With
  `-v`, a self-signed identity reports `CSSMERR_TP_NOT_TRUSTED` because only
  identities chaining to a trusted root are kept. Use `-p codesigning` without
  `-v` for detection. Trust is not required for codesign or for the designated
  requirement to be satisfiable — both scripts omit `-v` for exactly this reason
  (`scripts/create-dev-signing-cert.sh:19-21`, `scripts/bundle-macos.sh:133-135`).
- **`security find-identity … | grep -q` can fail a *matching* pipeline under
  `set -o pipefail`.** `grep -q` exits at the first hit and closes the pipe, so
  the producer dies of SIGPIPE (141) and the pipeline reports failure. Capture
  the output first, then match in the shell (`scripts/bundle-macos.sh:137-143`).
- **Homebrew OpenSSL 3/4 writes PKCS#12 with PBES2, which `security import`
  rejects** with `MAC verification failed`. The script exports with `-legacy`
  and falls back to stock macOS LibreSSL's default export, which imports
  (`scripts/create-dev-signing-cert.sh:48-58`).
- **The keychain may ask once** whether codesign may use the key. Answer
  "Always Allow" (`scripts/create-dev-signing-cert.sh:60-62`).

## Requirements, measured

Ad-hoc:

- `codesign -d -r-` → `# designated => cdhash H"…"` — the leading `#` marks a
  requirement codesign synthesized because none is stored.
- Every rebuild yields a different cdhash.
- `AXIsProcessTrusted()` → false, and `screencapture` → `could not create image
  from display`, even with the Privacy toggles ON.

Certificate-signed:

- `codesign -d -r-` → `designated => identifier "com.u007.ocode" and
  certificate leaf = H"…"`.
- `codesign --verify --strict` OK, and the requirement is identical across
  rebuilds.

**Removing and re-adding the `.app` does not restore a grant.** The TCC record
is keyed to the stored requirement, not to the bundle's presence, so a changed
cdhash is a new client that needs a fresh user grant. Re-grant once after
switching to a stable identity — not after every rebuild.

## Windows and Linux need no per-app grant

Neither platform has a TCC analogue, so a rebuild cannot lose a grant:

- **Windows** has no per-application consent for screenshots, input or file
  writes; access is governed by UAC and NTFS ACLs. The desktop server binds
  loopback `127.0.0.1:0` (`internal/desktop/boot.go:421`, or a saved loopback
  port), so it also raises no Windows Firewall prompt. The catalog reports
  `windows.no-explicit-grant` with status `not_required`
  (`internal/sysperm/catalog_fallback.go:10-20`).
- **Linux** has POSIX permissions only. Screen and input tooling is `xdotool` +
  `scrot` (X11) or `ydotool` + `grim` (Wayland) — system packages and udev
  access, not a per-app grant. The catalog reports `linux.no-explicit-grant` /
  `not_required` (`internal/sysperm/catalog_fallback.go:21-31`).

The catalog table lives in the build-tag-free
`internal/sysperm/catalog_fallback.go` as `genericEntries(goos string)`, so its
Windows and Linux rows are testable on any host. The build-tagged
`internal/sysperm/catalog_other.go:13` only delegates:
`func builtinEntries() []Entry { return genericEntries(runtime.GOOS) }`.

The persisted on/off intent plus `Reconcile` remain the fallback for ad-hoc
macOS builds and for grants the user has not re-approved
(`internal/sysperm/sysperm.go:4-11`).

## Status honesty

Measured and verified: the ad-hoc failure reproduces, the certificate-anchored
designated requirement is stable across rebuilds, and
`codesign --verify --strict` accepts both signatures. The TCC probe below
measured grant persistence at the mechanism level for a throwaway
Microphone-requesting bundle — but end-to-end TCC acceptance for the real
`ocode.app` is still unobserved: the probe's TCC row could not be inspected
(`TCC.db` was unreadable without Full Disk Access), and the real app still
needs one relaunch and one grant.

## Verification (measured 2026-11-16)

**Same requirement across different code.** Two bundles built from different
binaries (`bin/ocode-desktop` and `bin/ocode`), same `--identifier
com.u007.ocode`, same `ocode-dev` certificate: `codesign -d -r-` returned the
identical `identifier "com.u007.ocode" and certificate leaf = H"bdcd560f…"` for
both, and both passed `codesign --verify --strict`.

**TCC grant survives a rebuild (isolated probe).** A throwaway Swift app, bundle
id `com.u007.ocode.tccprobe`, cert-signed, requesting Microphone (single-button
consent dialog; same TCC client matching as Accessibility/Screen Recording):

- V1 (cdhash `9c71134e…`): `authorizationStatus` 0 → granted → 3.
- V2, a DIFFERENT binary (cdhash `36608e15…`), same certificate: **started at
  status 3** — the grant survived the rebuild with no prompt.
- V3, same new code but ad-hoc signed (cdhash `37a8864d…`): **started at status
  0** — the control, reproducing the bug.

This closes the previously pending "grant survives a rebuild" claim at the
mechanism level. The real app still needs one relaunch and one grant, because a
running process keeps its old signature and only a relaunch makes macOS evaluate
the new one.

**Cross-platform execution.**

- `go test ./internal/sysperm/` ran inside `golang:1.26.11-alpine` (Linux) → ok.
- `GOOS=linux` and `GOOS=windows` `go vet ./internal/sysperm/` → ok;
  `GOOS=windows go test -c` produced a test binary.
- `TestGenericEntriesWindows`, `TestGenericEntriesLinux` and
  `TestGenericEntriesUnknownOSNamesTheOS` now run natively on every host because
  `catalog_fallback.go` has no build tag.

**Remaining limits.**

- No Windows host was available: the Windows behavior is proven by compilation
  plus the pure-function tests, not by execution on Windows.
- The `xdotool`+`scrot` (X11) and `ydotool`+`grim` (Wayland) paths were not
  exercised.
- `~/Library/Application Support/com.apple.TCC/TCC.db` was not readable without
  Full Disk Access, so the probe's TCC row could not be inspected, and `tccutil
  reset Microphone com.u007.ocode.tccprobe` reported "No such bundle
  identifier"; the probe bundles were deleted, so the leftover record cannot
  match anything.

## Where the denied paths surface

- `internal/sysperm/catalog_darwin.go:20` — the Accessibility check runs
  `$.AXIsProcessTrusted()`.
- `internal/computer/permissions_darwin.go:110` — the Screen Recording probe
  runs `screencapture`.
- `internal/computer/driver_darwin.go:44` — the screenshot action itself runs
  `screencapture`.

See [Computer use](../computer-use.md) (the macOS prerequisites in
`docs/computer-use.md:126-128`) for the grant list these paths require.
