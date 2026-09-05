---
type: Gotcha
title: Seatbelt Profile Test Coverage Gap
description: Seatbelt profile addition for /dev/null and /dev/tty now has test coverage: TestSeatbeltProfileGrantsDevNullOnly, TestSeatbeltAllowsDevNullDiscard, TestSeatbeltDeniesDevTTYFreshOpen added plus pre-existing profile tests in profile_darwin_test.go. Coverage gap closed.
tags:
  - seatbelt
  - gotcha
  - sandbox
  - test-coverage
  - macOS
  - Linux
timestamp: 2026-09-05T02:15:34Z
---
## Resolved

- **Gap:** Seatbelt profile additions for /dev/null and /dev/tty shipped with zero test coverage, risking untested security changes on macOS/Linux parity.
- **Fix:** Added `TestSeatbeltProfileGrantsDevNullOnly`, `TestSeatbeltAllowsDevNullDiscard`, `TestSeatbeltDeniesDevTTYFreshOpen` tests plus leveraged pre-existing profile tests in `internal/shell/sandbox/profile_darwin_test.go`.
- **Date:** 2026-09-05
