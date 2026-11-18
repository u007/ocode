---
type: Concept
title: Root elevation via sudo/su with human-only password entry
description: As-built architecture of the completed root-elevation feature — a masked human-only password prompt for LLM-issued leading sudo/su bash commands, a per-Agent single-use in-memory secret store, PTY injection with echo suppressed (creack/pty + term.MakeRaw), native Windows UAC elevation, remote-SSH/WSL routing, gating and hardening, security invariants, verification, and residual limits.
resource: internal/agent/elevation.go
tags: [permissions, elevation, sudo, security, pty, secret-handling, tui, web, remote-ssh]
timestamp: 2026-11-18T00:59:10+08:00
status: active
---
# Root elevation via sudo/su with human-only password entry

As-built record of the completed root-elevation feature: designed, implemented, reviewed and committed on `feature/root-elevation` (worktree `.worktrees/root-elevation`, HEAD `25eb349a9184989f6f303814f40fede008dbe6d2`, 2026-11-18). The main checkout does not carry this code yet. Anchors are **file path + symbol**, not line numbers — the source lives in the worktree and line numbers drift. For the forward-looking design and the task checklist see [the design spec](../superpowers/specs/2026-11-17-root-elevation-design.md) and [the implementation plan](../superpowers/plans/2026-11-17-root-elevation.md).

## Goal and threat model

Let the LLM ask for root by emitting a `bash` call whose command begins with `sudo` or `su`. A human supplies the password in a masked field. The secret must never reach the LLM or enter the session transcript. Guarantees:

- The LLM never sees the password — not in tool output, tool errors, or anything fed back into the prompt.
- The secret is never written to the transcript, an SSE frame, a log line, or an error string.
- It is never placed in `argv` or the environment (no `sudo -S`, no `SUDO_ASKPASS=...`).
- It is never persisted to disk; it lives in a per-session in-memory cache, zeroed on use and reset.
- It is never available to another session or sub-agent.

## Locked decisions (user-approved 2026-11-17)

1. **Surfaces:** TUI + web/desktop; headless `run`/`acp` refuses with a clear error — there is no UI to prompt.
2. **Password lifetime:** per-session in-memory cache, keyed per pending ask, zeroed on use / reset.
3. **Windows:** attempt native elevation via `Start-Process -Verb RunAs`; UAC authenticates and no password is collected; clear unsupported error when no interactive desktop exists.
4. **Scope:** both `sudo` and `su` in v1.
5. **Feasibility spikes run first** (evidence below).

## Spike evidence (2026-11-17)

- **SUDO_ASKPASS + inherited fd — REJECTED.** The helper process is invoked on macOS, but `sudo` closes the inherited fd 3 before exec'ing the helper (`Bad file descriptor`). Never use `SUDO_ASKPASS`, askpass helpers, or the fd trick.
- **PTY + `term.MakeRaw` — APPROVED.** `github.com/creack/pty` plus `term.MakeRaw` on the master suppresses echo; the secret is delivered once with zero leak. `golang.org/x/term.State` fields are unexported in this version — use `MakeRaw`/`Restore`, not field tweaks.

## Implemented architecture (file path + symbol)

- **Detection and gating** — `internal/agent/permissions.go`: `leadingElevationCommand` is wrapper-chain aware (`env` / `command` / `nohup sudo`) and recognizes inline-secret forms (`sudo -S`, `-kS`, `--stdin`, `SUDO_ASKPASS`, chained), which are HardDenied. No auto path, judge, YOLO mode, or persisted allow rule can satisfy an elevation ask — only a human decision can. `IsElevationRequest` is the shared predicate.
- **Wire format** — `internal/agent/permissions.go`: `PermissionRequest.NeedsPassword`, `PermissionRequest.ElevationCommand`, and `PermissionRequest.ElevationKey` (non-secret, `json:"-"`); the two JSON fields are `omitempty`. The resolve endpoint (`internal/server/handler_permissions_resolve.go`) carries a **write-only** password in its body. `PermissionResponse` deliberately has **no** secret field, pinned by `TestPermissionResponseHasNoSecretField` (`internal/agent/permission_response_shape_test.go`).
- **Per-Agent secret store** — `internal/agent/elevation.go`: `ElevationManager` is a `map[requestID][]byte`; `Store` copies on write, `Take` is single-use and zeroes on read, `Clear`/`ClearAll` zero on `/new`, `/clear`, `/reset-id`, eviction and shutdown. A 3-strike wrong-password cap (`noteFailure` / `failureCount`) lives here.
- **Context injection** — `internal/tool/elevation_ctx.go`: carries only a getter closure (`WithElevationSecret` / `ElevationSecretFromContext`) plus an **independent** flag (`WithElevationFlag` / `ElevationFlagFromContext`), never the secret bytes. The bash tool fails closed with `elevationNoSecretMessage` if the flag is set but no secret is available.
- **PTY execution** — `internal/tool/elevated_unix.go` (`//go:build !windows`): `pty.Start` + `term.MakeRaw`, writes the secret once, scrubs the exact secret bytes from stdout/stderr/errors, takes a race-free snapshot of partial output, zeroes transient buffers, and kills the process group on cancellation. `internal/tool/elevated_windows.go` attempts native elevation and returns `errNoInteractiveDesktop` when no interactive desktop exists.
- **Masked dialogs** — TUI (`internal/tui/model.go`, `internal/tui/permission_modal.go`; tests in `internal/tui/elevation_dialog_test.go`) and web (`web/src/components/Chat/PermissionDialog.tsx`, a `type="password"` field cleared before any await). The web **rehydration** path `parsePermissionFromMessage` in `web/src/stores/chatStore.tsx` must map `needs_password` / `elevation_command` or the field vanishes after reload; the live SSE path passes them automatically. Pinned by `PermissionDialog.elevation.test.tsx` and `chatStore.test.tsx`.
- **Remote SSH/WSL** — the resolve endpoint that receives the password is the **execution host's** server via `/api/remote/{host}/api/...`; the secret crosses the SSH tunnel into remote memory only; the proxy never logs the body and the remote token never reaches the browser (pinned by `TestHandleRemoteProxy_ElevationPasswordCrossesTunnel`, `internal/server/handler_remote_proxy_test.go`).
- **Sub-agent asks** — the secret is staged through an in-process handoff (`SetPendingElevationSecret` in `internal/agent/elevation.go`) keyed by the non-secret `ElevationKey`, then moved into the **child** agent's own `ElevationManager`. The parent store is untouched; `PermissionResponse` never carries the secret.
- **Hardening** — headless clear-error guard wired into `run` / `acp` / orchestrate / subagent / advisor; the auto-continue gate blocks on a pending secret ask; Pulse `permission_resolve` explicitly refuses elevation asks; 3-strike wrong-password cap.

## Invariants / gotchas

- **Never use `SUDO_ASKPASS`** — the spike proved fd inheritance fails on macOS.
- **Never put the secret in `PermissionResponse`, SSE, the transcript, logs, `argv`, or env** — `TestPermissionResponseHasNoSecretField` pins the struct shape.
- **Gate execution on the independent elevation FLAG, not on secret presence.** On Windows `NeedsPassword=false`; on Unix a missing secret must fail **closed** rather than run unprivileged. Key guards on `IsElevationRequest` / `ElevationCommand`, never on `NeedsPassword`.
- **The web reload path must map the two new fields** (`needs_password`, `elevation_command`); the live SSE path passes them automatically.
- No new tool, tools-array entry, or base-prompt change: the new JSON fields are `omitempty` so existing frames stay byte-identical (prompt-cache order is tools → system → messages).

## Verification

Six implementation tasks, each with a task-scoped review plus a final whole-branch review; every review finding was fixed and re-reviewed. Full Go suites (`agent`, `tool`, `server`, `tui`) green, `go vet` clean, Windows cross-build clean; web vitest 5788/5789 (one pre-existing unrelated `HighlightedCode` flake); `tsc --noEmit` clean. `TestElevationEndToEnd_AskStorePTYNoSecretLeak` (`internal/agent/elevation_integration_test.go`) drives ask → store → PTY re-execution against a fake sudo and proves the secret appears **zero** times in the tool result, the session transcript, and the JSON that becomes SSE frames.

## Residual / known limits (recorded honestly, not defects)

- A process-global pending-secret handoff entry can linger if a host stages a secret and the child is cancelled before taking it. It is non-blocking and never persisted or logged.
- `ElevationKey` is a new non-secret field (`json:"-"`) on the shared `PermissionRequest`.
- The Windows path is cross-compiled and unit-tested but **not** exercised on a live UAC desktop.
- The PTY escape branch could not be forced end-to-end on this host; it is covered by a deterministic snapshot unit test and `-race`.
- Pre-existing unrelated flakes: Go `TestHeadlessTurnBroadcastsAgentActivity`, `TestTaskToolBackgroundRunUnexpectedStopMarksFailed`, and one web `HighlightedCode` flake.

## Related

- [Root Elevation via sudo/su with a Masked Human Password Prompt (design)](../superpowers/specs/2026-11-17-root-elevation-design.md)
- [Root Elevation Implementation Plan](../superpowers/plans/2026-11-17-root-elevation.md)
- [Sandbox Permission Mode](sandbox-permission-mode.md) — the write-integrity-only model this feature sits alongside; it is not a confidentiality boundary.
- [Bash Prefix Always-Allow Rules](bash-prefix-always-allow.md) — the prefix-allow machinery that deliberately does not apply to elevation asks.
