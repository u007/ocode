# Shell Sandbox — Implementation Plan (Index)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan part-by-part, task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **Plan style:** Per the repo owner's global CLAUDE.md Planning Rules, this plan is high-level — it names exact files, functions, and test assertions in prose, and does **not** embed code snippets. Each part file is self-contained; shared context lives here.

**Goal:** Add `sandbox` as a fourth, UI-togglable **permission mode** (alongside `normal`/`yolo`/`locked`). In sandbox mode the agent shell tool runs commands **without prompts** but each command is wrapped so **filesystem writes outside an allowed boundary fail at the OS level**. This is **write-integrity confinement only** — it does **NOT** protect secrets or prevent exfiltration (reads and network egress stay open; a sandboxed `python`/`node` can still read `.env`, `~/.ssh`, `auth.json` and POST them anywhere). The boundary is sourced from the existing `AllowedRoots()` scope model, classified into writable vs read-only. Toggling to/from sandbox works from both TUI and web exactly like the existing modes.

**Architecture:** A new `internal/shell/sandbox` package exposes one `Wrapper` interface with per-GOOS backends (macOS Seatbelt via `sandbox-exec`; Linux Landlock with a `bubblewrap` fallback; Windows no-op). A single unified bash-command builder in `internal/tool` replaces the two current raw command-construction sites (foreground `exec.go`, background `process.go`) and applies the wrap when the active mode is `sandbox`. `PermissionManager.Decide` treats `sandbox` as "YOLO's prompt-bypass **plus** OS write-confinement". TUI and web reuse their existing permission-mode toggle surfaces (status-bar click / mode API), not the `shift+tab` agent-mode cycle.

**Tech Stack:** Go 1.26.1 (per `go.mod`); `sandbox-exec` (Seatbelt) on darwin; Landlock LSM (kernel ≥5.13, ABI-probed, `PR_SET_NO_NEW_PRIVS`) on linux with a `bubblewrap` fallback; Windows no-op. Web UI is React (`web/src`).

**Spec:** This plan; the design + review discussion in this session.

## Decisions (settled — do not reopen)

1. **`sandbox` is a permission mode, not a config flag.** The mode is the toggle.
2. **Session-scoped, live-only — sandbox is never the durable default.** The web YOLO toggle already persists nothing (`HandleSetYolo` mutates live agents only); the TUI, however, *does* persist mode via `persistPermissions()` (`model.go:7029`). Decision: `sandbox` must **not** be written as the persisted default — the TUI persist path and any config saver must clamp/skip `sandbox` (persist `normal` in its place), so a restart and any freshly-constructed agent (including cron) start in `normal`. Sandbox is a deliberate per-session opt-in. *Rationale:* a powerful promptless mode should not silently outlive the session that enabled it. This is what makes the cron-inheritance risk impossible.
3. **Write-integrity primary, plus an explicit sensitive-data guard.** Base posture: writes confined to writable roots; reads/exec otherwise global; egress open (documented in the mode's help text — reads/network are *not* fully contained). **On top of that**, a named sensitive set is protected and is authoritative even in sandbox mode (sandbox does **not** bypass it). All of these resolve to **Ask** → routed through the auto-permission judge when `auto` is on, else a human prompt (Decision 9):
   - `auth.json` (ocode's token) → **read + write → Ask.** The agent never legitimately needs it, so the judge/human is expected to refuse — but it is not an absolute hard-deny (owner's choice; the judge is the gate).
   - ocode **config dir** (`~/.config/opencode`) and **data dir** (sessions/memory) → **read allowed, write → Ask.** *Rationale:* if the agent could silently overwrite ocode config it could self-grant a permission rule; the Ask routes the decision to the judge/human. (Config writes are auto-allowed in *normal* mode today; this Ask is scoped to sandbox mode unless the owner extends it.)
   - `~/.ssh`, `.env` files → **read + write → Ask.**
   The only absolute (never reaches the judge) is the existing hard-block set (`rm -rf /`, forkbomb, `| sh`, `; sudo`, `dd if=`, `mkfs`). Enforcement of the sensitive set is the **permission-layer check only** (backend-independent). We deliberately do **not** OS-block reads of the sensitive set: because these are **Ask** (the judge/human may approve), an OS hard-block would make approval impossible. The trade-off: the permission layer catches direct commands (`cat auth.json`, a redirect into `.ocode/settings.json`), but a read hidden inside an interpreter (`python -c`) is **not** caught — a documented residual, accepted as the cost of keeping these approvable rather than absolute. (This also means the backends do write-walls only — no Seatbelt read-deny, no bwrap hide-mounts — and the Landlock read gap is moot.)
4. **Boundary = one authoritative, capability-classified root model.** `AllowedRoots()` is extended to classify each root writable vs read-only; both the permission layer and the sandbox read from that single model. Writable ⊂ AllowedRoots; the rest are read-only at the OS layer too. No second directory list.
5. **Fail-closed on macOS/Linux.** If mode is `sandbox` and a backend is `Supported()` but not `Available()` (missing `sandbox-exec`, Landlock ABI too low + no `bwrap`, namespaces disabled), the command **fails before `cmd.Start()` and before any process-registry record exists**, for both foreground and background. Windows (`!Supported()`) is the only intentional degrade → `normal` prompting.
6. **Cron guard retained** even though (2) makes inheritance impossible: a blank `perm_mode` cron job resolves to `normal`. Cheap invariant against a future persistence change.
7. **Out of scope:** interactive PTY terminal (`handler_terminal.go`) and the web `!shell` path — both remain unsandboxed and are labelled as such in docs.
8. **Self-escalation guard (mode-independent).** The agent must not silently shortcut its own permissions by editing the files that define them. Writes to permission-config targets (`.ocode/settings.json`, `.claude/settings.json`, ocode config permissions, anything feeding `extra_allowed_paths` / allow-deny rules) — and loopback requests to `/api/permissions*` — never take the auto-allow shortcut, above every shortcut, in normal-auto/YOLO/sandbox alike. *Rationale:* these files live inside the writable workspace, so path-based confinement alone auto-allows them; the boundary must protect the definition of the boundary. **Disposition (owner's settled choice): Ask** — same treatment as `auth.json` — so it does not take the auto-allow shortcut and instead routes through the auto-permission judge when `auto` is enabled (Decision 9), else a human prompt. It is **not** a hard-deny; the judge/human is the gate. Known residual: a write hidden inside an interpreter isn't statically caught and the file is in a writable root. See Part 02 Task 6.
9. **Ask routes through auto-permission (sandbox keeps `auto` enabled).** Sandbox mode does **not** disable the LLM auto-permission layer (unlike YOLO). Any decision that resolves to **Ask** in sandbox mode — the whole sensitive set: `auth.json`, ocode config/data-dir writes, `~/.ssh`/`.env`, general sensitive paths — flows through the normal pipeline: if auto-permission is **enabled**, the LLM judge decides allow/deny; if **disabled**, the human is prompted. The **only** absolute set (never falls back to the judge) is the existing hard-block list (`rm -rf /`, forkbomb, `| sh`, `; sudo`, `dd if=`, `mkfs`). No new mechanism: Ask outcomes reuse the existing auto-permission path.

## Global Constraints

Every task implicitly includes these:

- Boundary sourced only from the capability-classified `AllowedRoots()` model (Decision 4). Guard against any root resolving to `/` (voids the boundary).
- No silent unsandboxed execution on macOS/Linux (Decision 5).
- Every backend init/wrap error is logged with what was attempted + the error (CLAUDE.md logging rule). `SetMode` silently ignores invalid modes today — preserve that (invalid input leaves the current mode unchanged), do not add error returns.
- Per-GOOS files use the existing build-tag convention (`_darwin.go`/`_linux.go`/`_windows.go`/`_unix.go`).
- Trusted resolution of `sandbox-exec`/`bwrap` (absolute known paths, not arbitrary `$PATH`).
- Preserve existing shell semantics through the refactor: parent-death monitoring (`WrapWithParentMonitor`), `SysProcAttr`/process-group, pipes, context cancellation, Start-before-Register ordering, and process registration.
- Session workdir (`cmd.Dir`) must be wired through foreground, background, sandbox root resolution, and process hooks (currently unset; hooks use `os.Getwd()`).
- Targets: `darwin/{amd64,arm64}`, `linux/{amd64,arm64}`, `windows/amd64`.

## Shared Interfaces

- `PermissionModeSandbox PermissionMode = "sandbox"` — Part 01.
- Capability-classified roots: `AllowedRootsClassified() []RootSpec` where `RootSpec{ Path string; Writable bool }`, alongside the existing `AllowedRoots()` — Part 01.
- `RootSet{ WritableRoots []string; ReadRoots []string; NetworkEgress bool }` (ReadRoots empty ⇒ whole FS readable/executable) and `NewRootSet(specs []RootSpec) RootSet` — Part 01.
- `buildBashCmd(ctx, command, dir string, w sandbox.Wrapper, roots sandbox.RootSet, active bool) (*exec.Cmd, error)` — Part 01 creates the plain form, Part 02 finalises this signature (fail-closed).
- `sandbox.Wrapper{ Wrap(cmd *exec.Cmd, roots RootSet) (*exec.Cmd, error); Available() bool }`; package funcs `New() Wrapper`, `Supported() bool` — Part 02.
- `PUT /api/permissions/mode {"mode":"sandbox"}` — dedicated mode endpoint (Part 03); `POST /api/permissions` stays the tool-rule handler.

## Parts & Execution Order

| Part | File | Scope |
|------|------|-------|
| 01 | `01-core-mode-and-roots.md` | mode constant + SetMode case; capability-classified roots + `NewRootSet`; `~/.claude`; unified fg+bg builder + workdir wiring |
| 02 | `02-sandbox-backends.md` | Wrapper + no-op; fail-closed wiring (fg+bg, no phantom record) + Decide decision matrix; macOS Seatbelt; Linux Landlock+bwrap |
| 03 | `03-ui-toggles.md` | TUI click-cycle + `/sandbox` + state machine; dedicated mode endpoint + propagation; web dispatcher/API/PermissionsForm; cron guard |
| 04 | `04-docs-caveats.md` | docs, platform matrix, write-integrity framing, nvm/Windows/cron/Go-version notes |

## Self-Review

- **Coverage:** cross-platform ✅ (02); npm/python/tmp/extradirs writable ✅ (01 via classified roots — caches stay writable so `npm install`/`pip` work); auth/session dir kept read-only ✅ (01, resolves review point 5); `~/.claude` writable ✅ (01); UI toggle TUI+web ✅ (03); fail-closed fg+bg ✅ (02); cron guard ✅ (03); write-integrity framing ✅ (Decision 3 + 04).
- **Type consistency:** `PermissionModeSandbox`, `RootSpec{Path,Writable}`, `AllowedRootsClassified`, `RootSet{WritableRoots,ReadRoots,NetworkEgress}`, `NewRootSet`, `buildBashCmd(...)(*exec.Cmd,error)`, `Wrapper.Wrap(...)(*exec.Cmd,error)`, `Supported()`, `Available()`, `PUT /api/permissions/mode` — consistent across parts.
