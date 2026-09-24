---
type: Spec
title: Makefile Version Bump Targets (up-patch / up-minor) — Design Spec
description: Approved design spec for Makefile up-patch/up-minor version bump targets with POSIX-shell helper.
tags:
  - makefile
  - version-bump
  - design-spec
  - release
timestamp: 2026-09-24T14:35:26Z
---
# Makefile Version Bump Targets (`up-patch` / `up-minor`) — Design Spec

**Status:** Approved
**Date:** 2026-09-24
**Scope:** `Makefile`, `scripts/bump-version.sh` (new), `CONTRIBUTING.md`, regression tests (new)

## 1. Problem / Current State

The canonical version lives in exactly one Go constant:

- `internal/version/version.go:3` — `const Version = "0.8.110"`.

Keeping other artifacts in sync is currently manual:

- `CHANGES.md` has a first `## [Unreleased]` section containing a line
  `- **Version Bump** — 0.8.97 → 0.8.110`. `internal/version/version_test.go`
  (`TestVersionMatchesChangelog`) fails if the current `Version` does not
  appear in a `**Version Bump**` line inside that `[Unreleased]` section
  (with a fallback to any mention of the version in the section).
- `Makefile` and `scripts/bundle-macos.sh` read the Go constant
  (`bundle-macos.sh` parses `internal/version/version.go` with `sed` when no
  version argument is passed) and use it for install paths, `Info.plist`
  (`CFBundleShortVersionString`), and the remote-CLI artifact directory
  (`remote-binaries/<version>`).

A release bump therefore requires touching two files consistently by hand,
with a test as the only guard. This feature automates that step.

## 2. Goals

1. One command to bump patch/minor across `version.go` and `CHANGES.md`
   atomically-in-spirit (fail before partial replacement as far as practical).
2. The bumped version is immediately propagated by the existing
   `install` and `desktop-app` pipelines.
3. The bump logic is small, POSIX-shell, and covered by a regression test
   that runs entirely in temporary fixtures.

### Non-goals

- No automated release/tag/publish flow.
- No `major` bump (version is pre-1.0; `patch`/`minor` cover the real cases).
- No rewrite of `bundle-macos.sh` version resolution (it already consumes the
  canonical constant).
- No changelog summarization — the human still writes the release notes; only
  the `**Version Bump**` line's target version is rewritten.

## 3. Design

### 3.1 Helper script: `scripts/bump-version.sh`

A dedicated POSIX-shell script (proposed path `scripts/bump-version.sh`)
accepting exactly one argument: `patch` or `minor`.

**Version arithmetic**

| Mode    | Rule                                   | Example      |
|---------|----------------------------------------|--------------|
| `patch` | increment PATCH                         | `0.8.110` → `0.8.111` |
| `minor` | increment MINOR, reset PATCH to 0       | `0.8.110` → `0.9.0`   |

**Validation (fail fast, before any write)**

1. Read the current version from `internal/version/version.go`.
2. Require it to match `MAJOR.MINOR.PATCH` exactly (three dot-separated
   non-negative integer components, nothing else). Malformed values →
   actionable message on **stderr** and **non-zero exit**.
3. Missing/invalid CLI argument → usage message on stderr, non-zero exit.
4. `CHANGES.md` must contain a first `## [Unreleased]` section, and that
   section must contain a `**Version Bump** — old → new` line. If either is
   absent → actionable stderr message and non-zero exit **before either
   file is modified**.

**Replacement contract**

- Edit only two things:
  1. `internal/version/version.go`: the `const Version = "..."` value.
  2. `CHANGES.md`: the **first** `**Version Bump** — old → new` line inside
     the **first** `## [Unreleased]` section — replace the versions only.
- Everything else (all other changelog content, the rest of `version.go`)
  is preserved byte-for-byte.
- **Failure contract:** all structural checks (current version parses,
  `[Unreleased]` section exists, bump line exists and matches the expected
  old version) run *before* either file is replaced. If a check fails, both
  files are untouched and the script exits non-zero with a message naming
  the missing/invalid structure and how to fix it. Replacement still writes
  both files in immediate succession; partial replacement (e.g. `version.go`
  written but `CHANGES.md` write fails on I/O) is avoided as far as
  practical — the script prepares both new contents in memory/temp files
  first, then commits them — but a crash between the two final writes is a
  documented residual risk (recoverable by re-running with the restored
  old version or fixing the line by hand; `go test ./internal/version`
  detects any mismatch).

### 3.2 Makefile targets

Add to `.PHONY` and define:

```make
up-patch:
	sh scripts/bump-version.sh patch
	$(MAKE) install
	$(MAKE) desktop-app

up-minor:
	sh scripts/bump-version.sh minor
	$(MAKE) install
	$(MAKE) desktop-app
```

(Ordering is the design; exact recipe syntax at implementation time.)

**Why sequential recursive makes instead of prerequisites:**

- Prerequisites are not a safe ordering barrier under `make -j` — with
  parallel make, `up-patch: bump install desktop-app` could run all three
  concurrently, letting builds read the *old* version.
- A bump failure or build failure must **stop** later steps; sequential
  recipe lines (each a separate shell invocation that fails the target on
  non-zero) give exactly that.

**Semantics of each target:**

1. Bump **synchronously** (script must finish successfully).
2. Run `$(MAKE) install` — rebuilds and installs with the newly bumped
   canonical version.
3. Only then run `$(MAKE) desktop-app` — packages `bin/ocode.app` with that
   version (`Info.plist` `CFBundleShortVersionString`) and includes the
   remote CLI artifacts (`desktop-remote-binaries`, `remote-binaries/<new
   version>`).

Because builds run after the bump, downstream consumers (`install`
destination, `ocode --version`, About panel, embedded remote binary
directory) all observe the new version.

### 3.3 Regression test (written BEFORE implementation)

A failing shell regression test covering, in **temporary fixture copies**
of `version.go`/`CHANGES.md`/`Makefile` (never the real workspace):

1. `patch` arithmetic: `0.8.110` → `0.8.111`.
2. `minor` arithmetic: `0.8.110` → `0.9.0` (patch reset to 0).
3. Malformed current version → non-zero exit, actionable stderr, no file
   changed.
4. Missing CLI argument / unknown mode → non-zero exit, usage on stderr.
5. Missing `[Unreleased]` section or missing `**Version Bump**` line →
   non-zero exit, **neither** file modified.
6. Correct changelog line replaced — only the first bump line in the first
   `[Unreleased]` section; unrelated changelog content byte-identical;
   unrelated files untouched.
7. Target command order: bump → `install` → `desktop-app` (asserted against
   stubbed `$(MAKE)`/recipes recording invocation order).
8. Failure stops subsequent commands: if the bump or `install` fails,
   `desktop-app` never runs.
9. Version propagation: stubbed downstream builds observe the newly bumped
   version (stub `install`/`desktop-app` echo the version parsed from the
   fixture `version.go`).

The test must run entirely under `mktemp -d` fixtures and clean up after
itself.

### 3.4 Documentation: `CONTRIBUTING.md`

Add the new commands to the build section with an explicit operational
note: **`up-patch` and `up-minor` are not lightweight metadata-only
operations** — each also runs `make install` and then `make desktop-app`,
so expect a full web build, install to the Go bin destination, and a
desktop app packaging cycle.

## 4. Alternatives Considered

| Alternative | Verdict | Why |
|---|---|---|
| Inline shell in the Makefile recipe | Rejected | Multi-step validation/replacement logic inline in Make is fragile (quoting, `$` escaping) and hard to test in isolation. |
| A Go command (`go run ./cmd/bump-version`) | Rejected | Unnecessary bootstrap/toolchain coupling: the bump must work when the Go tree is the thing being edited and adds build/test surface for a ~2-file text edit. POSIX shell is sufficient and directly testable. |

## 5. Validation Plan (post-implementation)

1. Run the focused regression test suite (must pass; it was written first
   and failed before implementation).
2. `go test ./internal/version` — confirms `TestVersionMatchesChangelog`
   still holds at the bumped version.
3. Execute **one real** `make up-patch` on the working tree:
   `0.8.110` → `0.8.111`. This runs `make install` then `make desktop-app`.
   - Verify the install destination via `go env GOBIN` / `go env GOPATH`
     (or `command -v ocode` + `ocode --version` where applicable) reports
     `0.8.111`.
   - Verify `bin/ocode.app/Contents/Info.plist` contains `0.8.111`
     (`CFBundleShortVersionString`).
   - Confirm the embedded remote binary directory is `0.8.111`
     (`remote-binaries/0.8.111`).
4. **Do not bump a second time. Do not run `up-minor` on the real tree.**

## 6. Files Touched (implementation)

- `scripts/bump-version.sh` — new helper (POSIX sh).
- `Makefile` — `.PHONY` entries + `up-patch` / `up-minor` targets.
- `CONTRIBUTING.md` — new commands + "not metadata-only" note.
- Regression test file(s) — new, fixture-based (path chosen at
  implementation time alongside existing test conventions).

Not touched: `internal/version/version.go` and `CHANGES.md` are modified
*by* the tool at runtime (one real `up-patch` run in validation), not by
the implementation itself.
