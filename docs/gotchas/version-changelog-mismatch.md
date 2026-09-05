---
type: Gotcha
title: Version-Changelog Mismatch
description: Version mismatch between version.go (0.8.83) and CHANGES.md resolved — CHANGES.md [Unreleased] now includes –– **Version Bump** — 0.8.82 → 0.8.83 entry; `go test ./internal/version/` passes as of this commit. Status updated to resolved-as-of-this-commit.
tags:
  - version
  - changelog
  - gotcha
  - release-process
  - synchronization
timestamp: 2026-09-05T02:16:11Z
---
## Resolved

- **Gap:** Version mismatch between version.go (0.8.83) and CHANGES.md, where CHANGES.md was lagging at 0.8.80.
- **Fix:** Updated CHANGES.md [Unreleased] section with –– **Version Bump** — 0.8.82 → 0.8.83 entry; verified `go test ./internal/version/` passes.
- **Date:** 2026-09-05
