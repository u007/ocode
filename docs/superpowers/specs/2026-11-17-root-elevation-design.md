---
type: Design
title: Root Elevation via sudo/su with a Masked Human Password Prompt (design)
description: Approved design spec for letting LLM-issued bash commands whose leading token is sudo or su run elevated. The human enters the password in a masked TUI/web field; the secret never reaches the LLM, transcript, SSE frames, logs, error strings, argv or env. It is injected over a PTY with echo suppressed and held in a session-scoped in-memory cache. Covers detection/gating, wire format, secret store, PTY execution (creack/pty + term.MakeRaw), the Windows native-elevation limit, remote/WSL routing via ocode serve --remote, security invariants, failure modes and a test matrix.
tags:
  - design-spec
  - permissions
  - sudo
  - elevation
  - pty
  - secret-handling
  - tui
  - web
  - remote-ssh
  - wsl
timestamp: 2026-11-17T05:01:24Z
---
# Root Elevation via sudo/su with a Masked Human Password Prompt (design)

**Status:** Approved design (brainstorming architectural path), 2026-11-17. Implementation-ready; no product code is written by this page.

**Anchor convention:** every `file:line` citation below is **as-of 2026-11-17**. Re-derive each anchor by symbol before relying on it — line numbers drift when code above a citation changes.

## 1. Goal and threat model

Let an LLM-issued `bash` tool call run a command that begins with `sudo` or `su`, with the password supplied by a **human** through a **masked** input. The mechanism must work locally on macOS, Linux and Windows, under WSL, and for remote SSH projects where the agent and `bash` run on the remote host.

**Guarantees — the password is never:**

- visible to the LLM (not in tool output, tool errors, or any result fed back into the prompt);
- written to the session transcript, an SSE frame, a log line, or an error string;
- placed in `argv` or `os.Environ()` (no `sudo -S`, no `SUDO_ASKPASS=...`, no env assignment);
- persisted to disk (no session file, snapshot, or debug dump);
- available to any other session or sub-agent.

**Non-goals / residual risk.** The sandbox is already documented as write-integrity confinement, **not** confidentiality (`docs/concepts/sandbox-permission-mode.md`). Likewise, this design protects the secret from the LLM's *observation channels*, not from a malicious command the LLM itself writes: an elevated command can read its own PTY/stdin and transform the bytes (for example base64), which defeats exact-byte output stripping. Strip the exact password bytes from captured output as defense in depth, but record this as a known residual risk in the same class as the sandbox's "no secrets protection".

## 2. Locked user decisions (do not re-open)

1. **Surfaces:** TUI and web/desktop. Headless `run` / `acp` refuse with a clear error — there is no UI to prompt.
2. **Lifetime:** per-session, in-memory cache only. Re-prompt after restart; zero on session end.
3. **Windows:** attempt native elevation via `Start-Process -Verb RunAs`. **On Windows `NeedsPassword` is FALSE and NO masked password field is shown** — UAC performs authentication on the secure desktop, and ocode only confirms elevation intent. When no interactive desktop exists (headless, remote or service session), return a clear unsupported error. Record as a known limit.
4. **Scope:** both `sudo` and `su` in v1. `su` has no askpass and no `-S`; it uses the same PTY path.
5. **Feasibility spikes already run** (see §3) — record as evidence, do not repeat.

## 3. Evidence / spikes (run 2026-11-17, throwaway `/tmp` files)

- **SUDO_ASKPASS with an inherited file descriptor — REJECTED.** The helper process *is* invoked on macOS, but `sudo` closes the inherited `ExtraFiles` fd 3 before `exec`ing the helper. Helpers observed `Bad file descriptor` / `FD3_UNAVAILABLE`. **Do not use `SUDO_ASKPASS`, askpass helpers, or the fd trick.**
- **PTY with echo suppressed — APPROVED mechanism.** `github.com/creack/pty` v1.1.24 (`go.mod:15`) plus `term.MakeRaw` (`golang.org/x/term`) on `ptmx.Fd()` suppresses echo. Writing `SECRET123\n` to the master was read by the child exactly once, with no echo leak (captured output `GOT:[SECRET123]\n`).
- **`golang.org/x/term.State` has unexported fields in this version.** Use `term.MakeRaw` + `term.Restore`; do not attempt field tweaks on `State`.

## 4. Architecture

### 4.1 Detection and gating in `Decide()`

`Decide()` (`internal/agent/permissions.go:2091`) is the single choke point. Add an elevation pre-check **on the raw command words, before wrapper peeling**:

- Parse `params.Command` the way `buildBashCmd` does (`bash -c <command>` on Unix, `cmd /C <command>` on Windows) and take the first non-assignment word. `sudo` is registered as a transparent wrapper in the wrapper-flags map (`internal/agent/permissions_wrappers.go:31`) and is peeled by `effectiveCommandWords` (`:52`); a naive check on peeled words would make a leading `sudo` disappear and let the underlying command be auto-allowed. Detect on the unpeeled leading token.
- If the leading token is `sudo` or `su` (basename match), set `ElevationKind = "sudo" | "su"` and `ElevationCommand = params.Command`, and force the outcome to a **human ask**. On Unix set `NeedsPassword = true`; on Windows set `NeedsPassword = false` — UAC authenticates on the secure desktop, so the forced ask is a plain elevation-intent confirmation and no masked field is shown. Enforce this inside `Decide()` so no caller can bypass it. No auto-allow source may satisfy an elevation ask: config allow rules, persisted bash prefixes (`docs/concepts/bash-prefix-always-allow.md`), YOLO mode, the auto-permission judge / any `Decider`, and `AlwaysDecisionRule` / `AlwaysDecisionTool` are all overridden.
- **Inline-secret smuggling is a HardDeny** — no prompt, tool error with a clear message ("elevation requires a masked password prompt; inline secrets are not accepted"). Patterns: `-S` / `--stdin` on `sudo`; any `SUDO_ASKPASS` env assignment anywhere before the elevation token; a pipe or redirection feeding the elevation (`echo … | sudo`, `printf … | sudo`, here-doc / here-string), extending the existing `; sudo`, `&& sudo`, `| sudo` hard-block list in `isHardBlockedCommand` (`internal/agent/permissions.go:7468-7487`, chain list at `:7479`) to also cover `su`; and `su -c` forms attempting to pass a password inline.
- **No shell-function wrapper and no command rewriting.** The PTY runs the original command string unchanged, preserving quoting.

### 4.2 Wire format

`PermissionRequest` (`internal/agent/permissions.go:94-164`) gains three fields, all `omitempty`:

- `NeedsPassword bool` → `needs_password`
- `ElevationKind string` → `elevation_kind` (`"sudo"` / `"su"`)
- `ElevationCommand string` → `elevation_command`

`omitempty` is load-bearing: the `PERMISSION_ASK:` sentinel (`internal/tool/misc.go:15`, built in the ask path at `internal/agent/agent.go:4313-4341`) is a tool result that is persisted and fed to the LLM, so existing frames must stay byte-identical when the flags are absent (see `docs/concepts/prompt-cache-stability.md`). The sentinel carries only these metadata flags plus the already-visible command — **never a password**. It is fine for the LLM to know that an elevation prompt was needed; it must not know the value.

Web `POST /api/permissions/resolve` (decision consts `internal/server/handler_permissions_resolve.go:16-33`, request body struct at `:174-183`, handler at `:173`) gains a **write-only** `password` field. The handler:

1. reads `password` and `requestID`,
2. stores it into the owning Agent's `ElevationManager` (keyed by this ask's request ID) **before** `HandleApprovedToolCall` re-executes the tool,
3. never echoes it in the HTTP response, the `PermissionEvent` SSE frame (`internal/server/handler_permissions_resolve.go:42-120`), or any persisted payload.

Use one boundary helper — `detachPassword(body) (secret []byte, publicBody)` — called before any sentinel, SSE, or persisted structure is built, so there is a single auditable strip point.

TUI: the sentinel parser (`internal/tui/model.go:15249-15293`) reads `needs_password`; the masked value is attached to the permission response through the same resolve path (in-process / RC bridge, or the `/api/permissions/resolve` body). It never enters `Message.Content`, the TUI message model, or a debug line.

### 4.3 Secret store

Own one `ElevationManager` on the **Agent** (`*agent.Agent`) — the object both the TUI and the server's `agentSession` wrap. The **TUI (in-process) and the RC bridge therefore reach the same per-Agent store**: a `bash` tool call always resolves the secret from the manager of the Agent that owns that call, so the secret never leaves the process that owns the tool call. Do not put the store on `agentSession` (it would miss the TUI and RC bridge).

Per-Agent data model — a **keyed store, one entry per pending ask**:

- `mu sync.Mutex`, `secrets map[string][]byte` keyed by `requestID`, `generation uint64`. Each entry is bound to the ask it answers, so a stale resolve cannot reuse another ask's secret and concurrent pending asks cannot overwrite one another.
- `Store(password []byte, requestID string)`, `Get(requestID string) ([]byte, bool)` returning a **copy**, `Clear(requestID string)` overwriting the bytes, nil-ing them and deleting the entry after resolution (consume-once). `ClearAll()` zeroes every entry at session teardown.

Because each Agent owns its own manager, parent and subagent caches are naturally isolated — no inheritance and no shared map.

**Lifecycle — zero the secret on:** session eviction / close, `/new`, `/clear`, `/reset-id`, TUI session switch / carry-forward, and process restart (nothing is ever written to disk). Re-prompt after restart. Keep this per-session lifetime: the manager dies with the Agent/session.

**Injection is via a context getter, not a tool field.** Mirror `tool.WithTodoScope` (set in `executeToolCallWithContext`): add `tool.WithElevationSecret(ctx, getter)` / `tool.ElevationSecretFromContext(ctx)`. Tool instances and MCP clients are shared across sessions (process-wide `mcpCache`), so a tool struct field would leak the secret across sessions — never put it there. `HandleApprovedToolCall`'s re-execution must carry a context with the getter bound to the freshly stored secret. The execution path copies the bytes, writes them to the PTY, and zeroes its copy immediately.

### 4.4 Execution (`internal/tool/exec.go`)

Add an elevation branch selected by the presence of the secret getter on the tool context.

**Unix (macOS / Linux, including the WSL guest — `bash` runs there):**

- Use `pty.Start(cmd)` (`github.com/creack/pty` v1.1.24, `go.mod:15`) on the **existing** `*exec.Cmd` produced by `buildBashCmd` (`internal/tool/bash_build.go:80-101` — it already does `bash -c`, `setProcGroup`, `cmd.Dir` and the sandbox wrap, but has no PTY or stdin today).
- Before writing, call `term.MakeRaw(ptmx.Fd())`; write `secret + "\n"` to the master **immediately** — no prompt detection is needed (spike result). Restore with `term.Restore(fd, oldState)`.
- Capture stdout/stderr from the master. **Strip the exact password bytes from captured stdout, stderr and any error string** before returning. This is load-bearing: `internal/agent/agent.go:2315-2321` runs `scanToolResult` → `TruncateToolResult` → `Message{Role: "tool"}` and feeds the result to the LLM, so any echoed secret leaks.
- On timeout / cancel, kill the process group. `setProcGroup` is already applied; `pty.Start` creates a new session, so signal the negative pid (precedent: `killProcessGroup` at `internal/shell/session_unix.go:428`, and `pty.Start` at `:163` / `internal/server/handler_terminal.go:541`). Zero every transient buffer that held the secret.

**Windows:** attempt native elevation with `Start-Process -Verb RunAs` from PowerShell. **`NeedsPassword` is FALSE and NO masked password field is shown** — UAC performs authentication on the secure desktop; ocode only confirms the elevation intent through the forced human ask. If no interactive desktop exists (headless, remote or service session), return a clear unsupported error ("elevation unavailable without an interactive desktop"). Keep the OS split in the style of `internal/server/handler_terminal_windows.go`. Record the limit in docs and `TODO.md`.

### 4.5 Surfaces

- **TUI — `PermissionModal`** (`internal/tui/permission_modal.go:17`) currently maps only `y`/`n`/`a`/`t`/enter via `OnChoice` and has no text input. Add a masked single-line field (Bubble Tea text input with password echo masking) shown **only** when `NeedsPassword`. On approval, include the entered value in the resolve payload; keep it in model state only — never in the message model, a frame, or a debug line (alt-screen safety: no raw writes to stdout/stderr). Normal asks keep the existing key map and rendering.
- **Web/desktop — `PermissionDialog.tsx`** (`web/src/components/Chat/PermissionDialog.tsx:151`, `handleResponse` `:248`, buttons `~721-756`, radio inputs `~652-654`): add `<Input type="password">` shown only when `needsPassword`, included in the resolve body only on explicit approval. `QuestionDialog.tsx`'s plain `Input` is **not** a secure channel — do not reuse it. The value is not rendered in the transcript `ToolBlock`, not placed in an SSE frame, and not logged; it lives in browser memory only.
- **Headless `run` / `acp`:** refuse a leading `sudo`/`su` with a clear error ("sudo/su elevation requires an interactive TUI or web/desktop session"), detected by the absence of a UI asker / terminal kind.

### 4.6 Remote and WSL

The agent and `bash` run on the **execution host**: a remote ocode server reached through `Handler.HandleRemoteProxy` (`internal/server/handler_remote_proxy.go:26`), or a WSL target driven by `wsl.exe` (`internal/remote/wsl.go`, bare-host key). The resolve endpoint that receives the password is that **host's** server: the browser/local proxy forwards the body over the tunnel, where `NewAPIProxy` (`internal/remote/proxy.go:54`) and `InjectAuth` (`:121-153`) add the remote token. The secret crosses the SSH tunnel into remote-host memory only; the browser holds it transiently in the request body and the local proxy never logs or persists it. The remote server's `?token=` ban and websocket-subprotocol check (`internal/server/server.go:1002-1025`) are unchanged. The `ElevationManager` lives on the host-side **Agent** that owns the tool call (inside the WSL distro's server for WSL targets), not on a client-side `agentSession`.

### 4.7 Security invariants

1. **No auto path satisfies an elevation ask** — config rules, persisted prefixes, YOLO, judges and always-rules all force a human prompt. On Windows that forced ask is a plain elevation-intent confirmation, **not secret collection**: `NeedsPassword` is false, no masked field is shown, and UAC authenticates on the secure desktop.
2. **The auto-continue judge and Pulse `permission_resolve` / `question_answer` refuse `NeedsPassword` asks** (defer/deny); they must never answer a secret ask (see the Pulse self-escalation rule in `CLAUDE.md`).
3. **Subagents never inherit the parent cache.** The store is a keyed `map[requestID][]byte` on the per-Agent `ElevationManager`, and each Agent owns its own manager. A sub-agent's leading `sudo` raises its own ask, stored under that ask's request ID in the sub-agent's Agent manager only; parent and subagent caches are naturally isolated, with no inheritance or shared map.
4. **Headless refuses.**
5. **The tools array and base prompt are unchanged** — no new tool, no tool-schema field; preserve the Anthropic cache order `tools → system → messages` (`docs/concepts/prompt-cache-stability.md`).
6. **No secret channel** — no occurrence in transcript, SSE, logs, argv, env, error strings, snapshots, or tool results; strip at source and again as defense in depth.
7. **Prompt at ask time, before execution** — never a mid-tool pause waiting for a password.

### 4.8 Failure modes

- Ask and collect the password **before** the command runs; there is no "tool is running, please enter password" state.
- **Wrong password** → `sudo`/`su` fails → tool error; clear the cache so the next ask re-prompts, and cap consecutive re-asks at exactly **3**, then deny with a clear message.
- **Timeout / cancel** → kill the process group and zero the secret; the cancelled tool call gets its answer through `finishCancelledRound` so no orphan tool message remains and no tool is silently re-executed on the next turn (see `CLAUDE.md`, "Cancelling a turn").
- The `password` field is stripped from sentinel / SSE / persisted payloads at the single boundary helper before any of them are built.

## 5. Out of scope / deferred

- No askpass mechanism of any kind (spike rejected).
- No changes to the tools array, no new tool, no changes to the base prompt.
- No macOS Keychain / `security`, Linux polkit, or any persistent credential store.
- Windows native elevation is best-effort with the documented interactive-desktop limit.

## 6. Verification

**Test matrix**

| Area | Test location | What it pins |
|---|---|---|
| Detection + inline-secret HardDeny | `internal/agent/permissions_test.go` | Leading `sudo`/`su` forces a human ask under yolo/allow/judge; `-S`, `SUDO_ASKPASS`, piped/`/su` chains HardDeny |
| Ask flag + stripping | `internal/agent` + `internal/server` tests | `needs_password`/`elevation_kind` set; no password bytes in sentinel JSON, SSE frames, or persisted payload |
| Resolve endpoint | `internal/server/handler_permissions_resolve*_test.go` | Password never echoed in response/SSE/log; stored before `HandleApprovedToolCall`; bound to request id |
| Masked dialogs | `internal/tui` tests + `web/.../PermissionDialog.test.tsx` | Field shown only for secret asks; value sent only on explicit approval; not rendered in transcript |
| PTY execution | `internal/tool` tests (Unix; WSL-shaped) | Fake `sudo`/`su` script reads `SECRET123\n` exactly once, no echo in captured output; wrong password clears cache; re-ask cap |
| Secret store lifecycle | `internal/server`/agent tests | Zero on `/new`, `/clear`, `/reset-id`, eviction, session switch |
| Remote proxy | `internal/server/handler_remote_proxy_test.go` | Body field forwarded but never logged; remote token handling unchanged |
| Windows path | OS-tagged Go test | Unsupported error returned when no interactive desktop exists |

**Concept:** a test must fail if the secret appears in *any* observable channel — assert over the sentinel JSON, SSE frames, persisted session file, error string and captured PTY output, not just the happy path. Run a deliberate "wrong password" negative control and an echo-leak negative control.

**Commands**

```bash
go build ./...
go test ./internal/agent -run 'TestElevation|TestPermission' -count=1
go test ./internal/tool -run 'TestElevationPTY|TestPasswordStrip' -count=1
go test ./internal/server -run 'TestPermissionResolve|TestRemoteProxy|TestElevation' -count=1
(cd web && pnpm test -- src/components/Chat/PermissionDialog.test.tsx)
(cd web && pnpm typecheck)
```

## 7. Code anchors (as-of 2026-11-17)

| Anchor | What it is |
|---|---|
| `internal/tool/bash_build.go:80-101` | `buildBashCmd` — `bash -c` / `cmd /C`, `setProcGroup`, `cmd.Dir`, sandbox wrap; no PTY/stdin today |
| `internal/tool/exec.go:220-266`, `~405-410` | boundedBuffer stdout/stderr pump; result returned as a string |
| `internal/agent/agent.go:2315-2321` | `scanToolResult` → `TruncateToolResult` → `Message{Role:"tool"}` fed to the LLM |
| `internal/agent/permissions.go:94-164` | `PermissionRequest` (no secret field today) |
| `internal/agent/permissions.go:189-193` | `PermissionResponse` — Level / PersistRule / PersistTool only |
| `internal/agent/permissions.go:2091` | `Decide()` — gating choke point |
| `internal/agent/permissions.go:7468-7487`, `:7479` | `isHardBlockedCommand`; `; sudo` / `&& sudo` / `| sudo` chain list |
| `internal/agent/permissions_wrappers.go:31`, `:52` | `sudo` as a transparent wrapper; peeled by `effectiveCommandWords` |
| `internal/tool/misc.go:15` | `SentinelPermissionAsk = "PERMISSION_ASK:"` |
| `internal/agent/agent.go:4313-4341` | permission-ask path (main agent `OnPermissionAsk` nil) |
| `internal/tui/model.go:15249-15293` | TUI sentinel parse |
| `internal/tui/permission_modal.go:17` | `OnChoice` maps only y/n/a/t/enter — no text input |
| `web/src/components/Chat/PermissionDialog.tsx:151`, `:248`, `~721-756`, `~652-654` | dialog, `handleResponse`, buttons, radio inputs |
| `internal/server/handler_permissions_resolve.go:16-33`, `:174-183`, `:42-120` | decision consts, resolve body struct (handler at `:173`), `PermissionEvent` SSE frame |
| `internal/server/handler_remote_proxy.go:26` | `(*Handler).HandleRemoteProxy` |
| `internal/remote/proxy.go:54`, `:121-153` | `NewAPIProxy`, `InjectAuth` (remote token never reaches the browser) |
| `internal/server/server.go:1002-1025` | remote `?token=` ban + websocket subprotocol check |
| `internal/remote/wsl.go` | WSL target via `wsl.exe`, bare-host key |
| `internal/shell/session_unix.go:163`, `:428` | `pty.Start` precedent; `killProcessGroup` |
| `internal/server/handler_terminal.go:541`; `internal/server/handler_terminal_windows.go` | PTY terminal precedent; Windows stub pattern |
| `go.mod:15` | `github.com/creack/pty v1.1.24` |

## 8. Related docs

- `docs/concepts/sandbox-permission-mode.md` — the "write-integrity only, not confidentiality" framing this design inherits.
- `docs/concepts/bash-prefix-always-allow.md` — persisted prefix rules that must not satisfy an elevation ask.
- `docs/concepts/prompt-cache-stability.md` — why `omitempty` keeps tools/system/messages byte-stable.
- `docs/concepts/persistent-shell-session.md` — existing PTY session precedent.
- `docs/concepts/remote-persistent-sessions-terminals.md` — remote host execution model.
- `docs/concepts/inbound-content-guardrail.md` — `scanToolResult` / secret-stripping placement conventions.
