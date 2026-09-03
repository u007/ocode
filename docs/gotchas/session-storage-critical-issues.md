---
type: Gotcha
title: Session Storage Critical Issues
description: Critical issues discovered in session storage code review
timestamp: 2026-09-03T06:57:01Z
---
## Overview

This document captures critical issues discovered during a session storage code review. These issues span compilation failures, concurrent data loss, migration bugs, and persistence gaps in the SSE endpoint.

---

## 1. Compilation Errors in Tests Due to Function Signature Change

**Impact:** Tests fail to compile after a function signature change in the session storage layer.

**Details:** A recent refactor altered the return type/syntax of a core session storage function, causing all dependent test files to fail compilation. The change was not propagated to test mocks and stubs, breaking the build pipeline.

**Resolution:** Update test mocks to match the new signature, or revert the function signature if the change was unintended.

---

## 2. Concurrent Message Loss with INSERT OR IGNORE

**Impact:** Session messages are lost under concurrent write scenarios.

**Details:** The session storage uses `INSERT OR IGNORE` for idempotent writes, but this does not account for race conditions where two parallel turns attempt to persist the same session state simultaneously. One turn's INSERT OR IGNORE silently drops the other's changes, leading to partial state loss.

**Resolution:** Replace `INSERT OR IGNORE` with a proper upsert mechanism (e.g., `INSERT ... ON CONFLICT ... DO UPDATE`) or serialize concurrent writes via a mutex/lock before persisting.

---

## 3. Legacy Migration Data Deletion on Failure

**Impact:** Legacy session data is deleted when a migration fails mid-way.

**Details:** The migration routine deletes old session data only after successfully writing new-format data. If the process crashes or the write fails mid-migration, the old data is already gone and the new data was never committed, resulting in complete session data loss.

**Resolution:** Implement a two-phase migration: (1) write new data alongside old, (2) only after successful verification, delete old data. Use atomic operations or journaling to guarantee consistency.

---

## 4. Missing Partial Turn Persistence in SSE Endpoint

**Impact:** Server-Sent Events (SSE) endpoint does not persist partial turns, causing loss of incremental progress.

**Details:** The SSE streaming endpoint pushes partial model responses to the client in real time, but it does not save these partial states to the session store. If the connection drops or the process restarts, the partial turn is lost and must be regenerated from scratch.

**Resolution:** Persist partial turn state to the session store on every SSE chunk, or buffer partial turns in a temporary store that can be resumed after a disconnect. Ensure the persistence hook is triggered after each streaming segment is yielded.

---

## Related Gotchas

- [Auto-Permission Prompt — TOCTOU Install Race](auto-permission-prompt-atomic-race.md)
- [Auto-Permission — Judge Must See the Session WorkDir, Not the Process CWD](auto-permission-judge-process-cwd.md)
- [Journal DB Connection Pool Leak](journal-db-connection-pool-leak.md)