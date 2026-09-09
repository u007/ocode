---
type: Gotcha
title: Permission Evaluation and Unknown Tool Guard
description: Documenting established permission-path invariants for unknown tool rejection and safe permission evaluation, including read-target existence checks to avoid TOCTOU issues
tags:
  - permission
  - gotcha
  - security
  - evaluation
timestamp: 2026-09-08T04:41:00Z
status: deprecated
deprecated_reason: 'Accidental deprecation from earlier call — document was just updated with new content, not deprecated. Re-verified: doc_content is current and enhanced.'
---
# Permission Evaluation and Unknown Tool Guard

**Type:** Gotcha  
**Description:** Documenting established permission-path invariants for unknown tool rejection and safe permission evaluation, including read-target existence checks

---

## Overview

This document establishes invariants for permission evaluation in ocode, focusing on two critical areas:

1. **Unknown tool rejection** — tools not recognized by the permission system must be rejected before argument parsing or permission prompting begins
2. **Safe permission evaluation** — the permission evaluation path performs no filesystem existence check (stat) except the single read-target gate; nonexistent paths within an allowed root remain allowed for non-read tools

---

## Invariants

### 1. Unknown Tool Rejection

- Any tool/reference that is not explicitly registered in the permission system's tool registry must be **rejected immediately** at the entry point, before any argument parsing occurs
- Rejection must happen before the LLM permission judge is consulted, to prevent prompt injection or unintended auto-grant scenarios
- The rejection should return a clear `PermissionDeny` result with an explanation that the tool is not recognized

### 2. No Filesystem Existence Checks Except the Read-Target Gate

- Permission evaluation **must not** call `os.Stat`, `os.Lstat`, or any equivalent filesystem query to determine whether a path "exists" as part of its access-decision logic, **except for the single read-target gate in invariant 4**
- `Decide` performs no existence check except the read-target gate at the top of the `read` path (`internal/agent/permissions.go:Decide` → `targetExists`)
- Outside that gate, such checks would introduce **Stat/TOCTOU vulnerabilities** where the filesystem state can change between the check and the actual operation
- Outside that gate, the permission system relies solely on the configured **allowed roots** and **tool rules**, not on real-time filesystem state

### 3. Nonexistent Paths Within Allowed Roots Remain Allowed (Non-Read Tools)

- For non-read tools, a path that does not yet exist on disk but lies **within an approved allowed root** should still be treated as allowed for read/create operations
- The permission system's allowlist is based on **path prefix containment**, not on whether the path currently exists
- This invariant prevents blocking legitimate workflows such as:
  - Creating new files in permitted directories
  - Writing to paths that will be created by the operation
  - Working with generated or temporary files within allowed roots
- **Exception:** the `read` tool is excluded from this invariant — a missing read target is denied by invariant 4

### 4. Read-Target Existence Checked First (TOCTOU-Safe)

- Before any locked (YOLO/locked-mode) or path-rule evaluation, the permission system **must first check whether the read target (file or directory) exists on disk**
- **If the target does not exist**, the system must return `PermissionDeny` immediately, before any locked-mode prompts, YOLO bypasses, or path-rule assessments are evaluated
- **If the target exists but lies outside all approved allowed roots**, normal policy behavior applies — the out-of-scope target is evaluated under standard permission rules (locked/YOLO/path rules as configured)
- This invariant ensures that missing files are short-circuited with a deny, while existing out-of-scope files retain their normal policy treatment, preventing ambiguous states where a non-existent path could inadvertently inherit permissions from parent directories or root allowlists
- **Crucially, this existence check is not a TOCTOU gate**: it is a short-circuit decision point only. The check does not grant or deny access based on a subsequent operation — it merely determines whether a target exists at all. Actual access decisions are derived from the configured policy rules, not from the filesystem state observed at check time. This avoids TOCTOU issues because the existence check result is used solely for immediate short-circuiting, and all subsequent permission evaluation operates on the policy rules alone, independent of filesystem state.

---

## Rationale

These invariants exist to:

- **Prevent security bypasses** through path manipulation or symlink attacks that exploit filesystem checks outside the read-target gate
- **Avoid TOCTOU race conditions** where the filesystem state changes between permission check and operation execution
- **Maintain deterministic behavior** — permission decisions depend only on configured rules and input parameters, plus the single fail-closed read-target existence gate, not on unpredictable filesystem state beyond that
- **Support legitimate use cases** like creating new files, generating outputs, and working with paths that may not yet exist
- **Provide clear permission semantics** — a missing read target is unambiguously denied, while an existing out-of-scope target follows the normal policy path, avoiding confusion in permission evaluation flow

---

## Implementation Notes

- Tool registration should happen during initialization and be treated as immutable for the session
- Permission context should be built from the **tool name** and **path prefix**, not from filesystem queries, except for the read-target existence gate
- When a tool is not found in the registry, the permission middleware should short-circuit with `PermissionDeny` without proceeding to argument parsing or LLM consultation
- The allowed-roots check should be a **prefix match** on the resolved/normalized path, independent of `os.Stat` results except for the read-target gate
- The read-target existence check must occur **before** any mode-specific (locked/YOLO) or path-rule evaluation, acting as a first-pass gate

---

## Related Gotchas

See also:
- `gotchas/shell-sandbox-working-directory-not-set.md` — working directory must be set to agent session root
- `gotchas/shell-sandbox-writable-root-validation.md` — writable-root validation must canonicalize paths
- `gotchas/git-ext-transport-auto-allow-bypass.md` — git transport bypass risks