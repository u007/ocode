---
type: Plan
title: Root Elevation Implementation Plan
description: Implementation plan for approving and password-unlocking LLM-issued sudo/su bash commands without the secret reaching the LLM or transcript — elevation detection/gating, a per-Agent keyed secret store, PTY injection with echo suppressed, TUI/web masked dialogs, remote hardening, and end-to-end verification (Tasks 1–6).
tags:
  - plan
  - permissions
  - elevation
  - security
  - pty
timestamp: 2026-11-17T05:06:32Z
---
# Root Elevation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the human approve and password-unlock LLM-issued `sudo`/`su` bash commands without the secret ever reaching the LLM or transcript, locally, on Windows, under WSL, and over remote SSH.

**Architecture:** Elevation detection and a write-only secret field ride the existing permission ask/resolve channel. A per-Agent keyed in-memory store holds the secret; a context getter injects it into a PTY-based bash execution branch on Unix (echo suppressed via `term.MakeRaw`), with a native-elevation attempt on Windows. No new tool, no tools-array change, no base-prompt change.

**Tech Stack:** Go 1.26.1, `github.com/creack/pty v1.1.24`, `golang.org/x/term` (`MakeRaw`/`Restore` only — `State` fields are unexported), Charm TUI, React SPA.

**Spec:** `docs/superpowers/specs/2026-11-17-root-elevation-design.md`

**Detail:** decisions, not code — the default; the implementer writes the implementation.

## Global Constraints

- Never place the secret in a tool result, `PermissionResponse`, SSE frame, transcript message, debug log, argv, or env var. `agent.go:2315-2321` feeds tool output back to the model.
- Never use `SUDO_ASKPASS` or an inherited fd; the 2026-11-17 spike proved sudo closes fd 3 to the helper on macOS.
- No changes to the `tools` array or base prompt (Anthropic cache order tools → system → messages).
- New JSON fields are `omitempty` so existing frames stay byte-identical.
- Retry cap is exactly 3 consecutive re-asks before deny.
- Windows: `NeedsPassword = false`, no masked field; UAC authenticates; clear unsupported error when no interactive desktop.
- Headless `run`/`acp` refuses elevation with a clear error before any prompt.
- Tests overriding HOME use the package's `setHomeTree(t, home)` helper, never a bare `t.Setenv("HOME", …)`.

## Review Focus

1. Command output echoing the password → a fake-sudo PTY test asserts the secret appears zero times in returned output.
2. Resolve request body being logged or persisted → endpoint test asserts no logger or session transcript contains the password.
3. Auto-permit / YOLO bypassing the human → gating test asserts elevation asks survive every non-human decision path.
4. Parent and subagent caches cross-talking → keyed-store test asserts `requestID` isolation and per-Agent ownership.
5. Remote proxy leaking the field → proxy test asserts the password never appears in proxy logs or browser-bound frames.

---

### Task 1: Elevation detection, gating & wire format

**Risk:** high — security branching in the permission decision path; a bypass exposes root access.

**Files:**
- Modify `internal/agent/permissions.go` (`PermissionRequest` struct `:94-164`; `Decide()` `:2091`; `isHardBlockedCommand` `:7468-7487`)
- Modify `internal/server/handler_permissions_resolve.go` (`PermissionEvent` `:42-120`; body struct `:174-183`)
- Test `internal/agent/permissions_test.go`; Test `internal/server/handler_permissions_resolve_test.go`

**Interfaces:**
- Produces: `PermissionRequest.NeedsPassword bool`, `PermissionRequest.ElevationCommand string`; `PermissionEvent.NeedsPassword bool`, `PermissionEvent.ElevationCommand string`; resolve body `Password string` (write-only).

- [ ] **Step 1: Write failing tests.** Assert: (a) a leading `sudo <cmd>` / `su -c <cmd>` yields a permission ask with `NeedsPassword=true` and the command in `ElevationCommand`; (b) auto-allow, a matching persisted prefix, and YOLO mode cannot produce an allow decision for it; (c) `echo pw | sudo -S <cmd>`, `sudo -S <cmd>`, `SUDO_ASKPASS=... sudo <cmd>` are HardDeny; (d) the resolve handler accepts `password` but the resulting `PermissionResponse` and emitted SSE frame contain no password field.

```go
func TestElevationAskSurvivesAutoAllowAndYolo(t *testing.T) { /* assert NeedsPassword true, level ask, HardDeny false */ }
func TestInlineSecretSudoIsHardDenied(t *testing.T) { /* table: "echo pw | sudo -S whoami", "sudo -S whoami", "SUDO_ASKPASS=/x sudo whoami" */ }
```

- [ ] **Step 2: Run tests; confirm FAIL** (`go test ./internal/agent ./internal/server -run 'Elevation|InlineSecret' -count=1`).

- [ ] **Step 3: Implement detection and fields.** In `Decide()`, detect leading `sudo`/`su` on bash commands before wrapper peeling; return an ask with the two new fields that no automatic path satisfies. Extend the hard-block list with the inline-secret patterns. Add the omitempty fields to both structs and the resolve body; never copy `Password` into `PermissionResponse` or SSE.

- [ ] **Step 4: Run tests; confirm PASS** (same command as Step 2).

### Task 2: Per-Agent keyed secret store & context injection

**Risk:** high — secret lifetime, concurrency, and re-execution context.

**Files:**
- Create `internal/agent/elevation.go`
- Modify `internal/agent/agent.go` (`Agent` struct `:403`, constructor, reset/rekey hooks)
- Create `internal/tool/elevation_ctx.go` (pattern of `WithTodoScope` at `internal/tool/todo_store.go:280`)
- Test `internal/agent/elevation_test.go`

**Interfaces:**
- Produces: `ElevationManager` with `Store(requestID string, secret []byte)`, `Take(requestID string) []byte`, `Clear(requestID string)`, `ClearAll()`; each stored `[]byte` is a copy, zeroed on take/clear. Agent field `elevation *ElevationManager`, initialized in the constructor. Context helpers `tool.WithElevationSecret(ctx, getter func() []byte) context.Context` and `tool.ElevationSecretFromContext(ctx) func() []byte`.

- [ ] **Step 1: Write failing tests.** Assert: two request IDs hold independent secrets; `Take` returns the secret and zeros it; `ClearAll` zeros every entry; a secret taken twice returns nil; a subagent's Agent has a distinct manager from its parent.

```go
func TestElevationManagerPerRequestIsolation(t *testing.T) { /* two IDs, Take one, other intact */ }
func TestElevationManagerTakeZeroes(t *testing.T) { /* backing array is zero after Take */ }
```

- [ ] **Step 2: Run tests; confirm FAIL** (`go test ./internal/agent -run ElevationManager -count=1`).

- [ ] **Step 3: Implement store and context helpers.** Add the keyed, mutex-guarded store; wire `ClearAll` into the session reset paths (`/new`, `/clear`, `/reset-id` rekey, eviction) and agent construction. Add the context helpers without storing the secret in the context itself — only a getter closure.

- [ ] **Step 4: Run tests; confirm PASS.** Also assert `HandleApprovedToolCall`'s re-execution carries the same context as the original call.

### Task 3: PTY elevation execution in the bash tool

**Risk:** high — process lifecycle, secret leakage, cross-platform build.

**Files:**
- Create `internal/tool/elevated_unix.go` (`//go:build !windows`)
- Create `internal/tool/elevated_windows.go`
- Modify `internal/tool/exec.go` (`:220-320`) and `internal/tool/bash_build.go` (`:80-101`)
- Test `internal/tool/elevated_unix_test.go`; Test `internal/tool/elevated_windows_test.go`

**Interfaces:**
- Consumes: `tool.ElevationSecretFromContext(ctx)`.
- Produces: `executeElevated(ctx context.Context, cmd *exec.Cmd, secret []byte) (output string, err error)` on Unix; a Windows implementation returning a clear unsupported error or a native-elevation attempt result.

- [ ] **Step 1: Write failing tests (Unix).** Use a fake `sudo`/`su` script on `PATH` that reads a line from its tty and prints a marker. Assert: (a) the secret is delivered exactly once and never appears in returned stdout/stderr; (b) an incorrect secret yields a tool error and no output leak; (c) timeout kills the process group and the transient secret buffer is zeroed; (d) the original command string is unchanged (no rewriting).

```go
func TestElevatedPTYSecretNeverEchoed(t *testing.T) { /* count(secret, output) == 0 */ }
func TestElevatedPTYWrongSecretClears(t *testing.T) { /* error surfaced, store cleared */ }
```

- [ ] **Step 2: Run tests; confirm FAIL** (`go test ./internal/tool -run ElevatedPTY -count=1`).

- [ ] **Step 3: Implement the Unix branch.** `pty.Start` the existing `bash -c` invocation, `term.MakeRaw` on `ptmx.Fd()` to disable echo, write `secret + "\n"` to the master immediately (no prompt detection), read output with a bounded buffer, kill the process group on cancel/timeout, and scrub the exact secret bytes from stdout, stderr, and error strings before returning. Restore terminal state with `term.Restore`. No `SUDO_ASKPASS`, no command rewriting.

- [ ] **Step 4: Implement the Windows branch.** Attempt native elevation and return a clear unsupported error when no interactive desktop exists. Build-tag both files; confirm `GOOS=windows go build ./internal/tool` succeeds.

- [ ] **Step 5: Run tests; confirm PASS** on Unix and Windows build.

### Task 4: TUI masked elevation dialog

**Risk:** standard — UI state and key handling, no security branching.

**Files:**
- Modify `internal/tui/permission_modal.go` (`:17`), `internal/tui/model.go` (ask parse `:15249-15293`, choice handler)
- Test `internal/tui/elevation_dialog_test.go`

**Interfaces:**
- Consumes: `PermissionEvent.NeedsPassword` / ask payload fields from Task 1.
- Produces: on approval, `PermissionResponse.Level` plus the typed password handed to the store from Task 2.

- [ ] **Step 1: Add tests.** Assert the masked field appears only for `NeedsPassword` asks, that typed input renders masked, and that the chosen value reaches `Store(requestID, []byte(password))` only on approval (not on deny/escape).

- [ ] **Step 2: Implement the modal field and choice wiring.** Add a password input mode to `PermissionModal`; keep y/n/a/t shortcuts for non-elevation asks. On Windows, skip the field per Global Constraints.

- [ ] **Step 3: Run tests** (`go test ./internal/tui -run ElevationDialog -count=1`) and confirm PASS; run the full `internal/tui` suite.

### Task 5: Web masked dialog, resolve wiring & remote hardening

**Risk:** high — remote transport and a browser-side secret field.

**Files:**
- Modify `web/src/components/Chat/PermissionDialog.tsx` (`:151`, `:248`)
- Modify the web permissions API client to send `password` only on approval
- Modify `internal/server/handler_remote_proxy.go` (`:26`) tests only
- Test `web/src/components/Chat/PermissionDialog.elevation.test.tsx`; Test `internal/server/handler_remote_proxy_test.go`

**Interfaces:**
- Consumes: `PermissionEvent.NeedsPassword` / `ElevationCommand` and resolve body `Password` from Task 1.
- Produces: a resolve POST containing `password` for elevation asks only.

- [ ] **Step 1: Add tests.** Assert: masked `<Input type="password">` renders only for elevation asks; value is never placed in component state that is logged or re-rendered into chat; the resolve call omits `password` for ordinary asks; a remote-host resolve keeps the field in the body crossing the tunnel and never in a browser-visible frame or proxy log.

- [ ] **Step 2: Implement the web field and API call.** Show the password input only when `needs_password` is true; clear the field after submit; send it in the resolve body on approval only.

- [ ] **Step 3: Run web tests** (`bunx vitest run web/src/components/Chat/PermissionDialog.elevation.test.tsx` from `web/`) and server tests (`go test ./internal/server -run 'RemoteProxy|Permission' -count=1`); confirm PASS.

### Task 6: End-to-end verification & hardening sweep

**Risk:** high — cross-surface integration.

**Files:**
- Test `internal/agent/elevation_integration_test.go`
- Docs via the context sub-agent after implementation (per OKF invariant)

**Interfaces:**
- Consumes: all interfaces above.

- [ ] **Step 1: Add an integration test.** Drive: LLM-style `bash` call with `sudo whoami` → elevation ask with `NeedsPassword` → secret stored via resolve → PTY execution against a fake sudo → tool result contains the marker and zero occurrences of the secret → transcript and SSE serialization contain zero occurrences of the secret.

- [ ] **Step 2: Add cache-invalidation tests.** Assert `ClearAll` on `/new`, `/clear`, `/reset-id`, and session eviction; assert headless mode refuses; assert auto-continue judge and Pulse `permission_resolve` refuse secret asks.

- [ ] **Step 3: Run the full affected suites** (`go test ./internal/agent ./internal/tool ./internal/server ./internal/tui -count=1`; web full suite; `go vet ./...`; `tsc --noEmit`) and confirm clean.

- [ ] **Step 4: Record durable knowledge** via the context sub-agent and check `git diff docs/`.
