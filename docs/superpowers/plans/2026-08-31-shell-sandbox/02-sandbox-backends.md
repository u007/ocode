# Part 02 — Backends + fail-closed enforcement

**Self-contained constraints recap:** Fail-closed on macOS/Linux (never run unconfined; error before `cmd.Start()` and before any registry record — fg **and** bg). Windows no-op degrades to `normal`. Boundary = classified roots → `RootSet`. Egress open, reads global (write-integrity only). Trusted absolute paths for `sandbox-exec`/`bwrap`. Log every backend error. Per-GOOS build tags. No code snippets.

**Consumes from Part 01:** `PermissionModeSandbox`, `RootSet`, `NewRootSet`, `AllowedRootsClassified`, `buildBashCmd`.

---

### Task 1: Wrapper interface + no-op (Windows) backend

**Files:**
- Create: `internal/shell/sandbox/sandbox.go` (interface + `New()` + `Supported()`), `internal/shell/sandbox/noop.go`
- Test: `internal/shell/sandbox/sandbox_test.go`

**Interfaces:**
- Produces: `Wrapper{ Wrap(cmd *exec.Cmd, roots RootSet) (*exec.Cmd, error); Available() bool }`. `New() Wrapper` selects by GOOS. `Supported() bool` = current GOOS has a real backend (compile-time: darwin/linux true, windows false). Distinction: **`Supported()`** = a backend exists for this OS; **`Available()`** = that backend can actually run now (binary present / kernel ABI ok). No-op: `Wrap` returns cmd unchanged, `Available()` false.

- [x] **Step 1:** Write failing tests `TestNoopWrapPassthrough` (cmd returned unchanged, nil error) and `TestSupportedMatchesGOOS` (true on darwin/linux, false on windows — table by build tag or a runtime check).
- [x] **Step 2:** Run; expect FAIL.
- [x] **Step 3:** Define interface, no-op, `New()`, `Supported()`. `New()` returns no-op where no real backend exists so all targets compile before Tasks 3–4.
- [x] **Step 4:** Run tests; `GOOS=windows go build ./...` and `GOOS=linux go build ./...`.
- [x] **Step 5:** Commit: `feat(shell/sandbox): Wrapper interface + no-op backend`.

---

### Task 2: Fail-closed wiring (fg + bg) + Decide decision matrix

**Files:**
- Modify: `internal/tool/bash_build.go` → finalise `buildBashCmd(ctx, command, dir, w sandbox.Wrapper, roots sandbox.RootSet, active bool) (*exec.Cmd, error)`. When `active`: if `!w.Available()` return an error (fail-closed); else return `w.Wrap(cmd, roots)`. When `!active`: return the plain cmd, nil.
- Modify: foreground `internal/tool/exec.go` and **background** `internal/tool/process.go:257-287` — call the new signature. **The wrap/error must occur before `cmd.Start()` and before any `ProcessRegistry`/`StartBackgroundDisplay` record is created**, so a wrap failure leaves no phantom process record.
- Modify: `BashTool` (`internal/tool/exec.go:45`) + construction (`internal/agent/agent.go:934`) to receive the `Wrapper` and a roots+mode provider from the agent's `PermissionManager` (dynamic — see Part 03 subagent note; resolve `NewRootSet(pm.AllowedRootsClassified())` and `pm.Mode()` fresh per command so `extra_allowed_paths` and mode changes take effect immediately).
- Modify: `internal/agent/permissions.go` `Decide()` bash branch (`:1179-1260`) — insert the sandbox auto-allow at the **same position as the YOLO shortcut (`:1204`)**: after `isHardBlockedCommand`/Claude-deny/dangerous-`rm`, before interpreter/heredoc/compound/prefix/sensitive-path checks. Decision matrix below.
- Test: `internal/tool/bash_build_test.go`, `internal/agent/permissions_test.go`

> **⚠️ REVISION — this task's matrix was executed with the old "bypass sensitive-path" behavior and must be reworked.** Sandbox mode must **keep the sensitive-path + ocode-self-config checks authoritative** (they were previously bypassed). Re-open the `Decide` change and the tests below.

**Decision matrix (sandbox mode, supported OS):**

| Check (in `Decide` order) | Normal | YOLO | **Sandbox** |
|---|---|---|---|
| `isHardBlockedCommand` (`rm -rf /`, forkbomb, `| sh`, `; sudo`, `dd if=`, `mkfs`) | HardDeny | HardDeny | **HardDeny** |
| Claude-settings deny | HardDeny | HardDeny | **HardDeny** |
| `auth.json` read or write | Ask/redact | Ask | **HardDeny** (agent never touches it) |
| ocode config-dir / data-dir **write** | (auto today) | bypass | **Ask** (prevents self-granting a bypass) |
| `~/.ssh`, `.env` read or write | Ask | bypass | **Ask** |
| dangerous-`rm` heuristic | Ask | Ask | **Ask** |
| interpreter/heredoc, redirection, prefix scope | Ask/prompt | bypass | **bypass** |
| everything else | scope-checked | Allow | **Allow, OS-wrapped** |

Rationale: sandbox = YOLO's ordinary-prompt-bypass **plus** OS write-confinement **minus** the sensitive set, which stays authoritative. Hard blocks and dangerous-`rm` remain.

**"Ask" in sandbox ≠ human prompt (INDEX Decision 9).** Because sandbox keeps `auto` enabled, every **Ask** cell above routes through the auto-permission LLM judge when enabled (judge decides allow/deny), and only reaches a human prompt when `auto` is disabled. **HardDeny** cells (`auth.json`, hard-blocks) are absolute — they never reach the judge. The `config-dir/data-dir write` row is **Ask** per the owner's stated choice (so it too is judge-decided when auto is on); the open knob is whether to promote it to HardDeny — see INDEX Decision 8. The sensitive rows are also enforced as defense-in-depth by OS read-deny in the backends (Seatbelt/bwrap; Landlock reads are permission-layer-only — see INDEX Decision 3).

- [x] **Step 1:** Write failing builder tests: `TestBuildBashCmdFailsClosedForeground` (active + stub `Available()==false` ⇒ error, no cmd); `TestBuildBashCmdFailsClosedBackgroundNoRecord` (bg path: wrap failure ⇒ error **and** `ProcessRegistry` has no record for it); `TestBuildBashCmdWrapsWhenActive` / `TestBuildBashCmdSkipsWhenInactive`.
- [x] **Step 2:** Write failing `Decide` tests: `TestDecideSandboxAutoAllowsPlainCommand`; `TestDecideSandboxHardDenyStillWins` (`rm -rf /`); `TestDecideSandboxDangerousRmStillAsks`; `TestDecideSandboxBypassesInterpreterPrompt` (a `python -c` heredoc auto-allows in sandbox but the same in normal asks); `TestDecideSandboxDegradesOnUnsupportedOS` (mode sandbox, `Supported()==false` ⇒ behaves like normal).
- [x] **Step 3:** Run; expect FAIL.
- [x] **Step 4:** Implement the fail-closed builder (fg+bg, pre-Start/pre-register), the `BashTool` provider wiring, and the `Decide` matrix branch.
- [x] **Step 5:** Run all + full `internal/tool` + `internal/agent`; expect PASS.
- [x] **Step 6:** Commit: `feat(shell/sandbox): fail-closed wrap (fg+bg) + Decide sandbox matrix`.

---

### Task 3: macOS Seatbelt backend

**Files:**
- Create: `internal/shell/sandbox/seatbelt_darwin.go`, `internal/shell/sandbox/profile_darwin.go`
- Test: `internal/shell/sandbox/seatbelt_darwin_test.go`

**Interfaces:**
- Produces: darwin `Wrapper`. `Wrap` generates an SBPL profile from `RootSet` and rewrites the cmd to invoke `sandbox-exec` (resolved to the absolute trusted path `/usr/bin/sandbox-exec`, not `$PATH`) with that profile wrapping the original `bash -c …`. `Available()` = that binary exists and is executable.

**Profile requirements (specify precisely — this is a security boundary):**
- Default deny writes; allow `file-write*` on each `WritableRoots` subpath (realpath'd); allow `file-read*` and `process-exec*` globally; allow `file-write*` covering all mutation kinds (create, truncate, unlink/delete, rename, `file-write-mode`/`file-write-owner`, symlink, hardlink) within writable roots — verify link/rename cannot escape a writable root to a read-only one.
- Allow process fork and signal delivery to the sandboxed subtree; allow `mach-lookup` needed for `bash`/toolchains to run; allow temp/runtime dirs the toolchains require (the `os.TempDir()` root is already writable).
- Allow network egress (documented open).
- **Escaping:** any path interpolated into SBPL must be rejected/escaped if it contains quotes, newlines, or null bytes; reject a writable root that resolves to `/` or to a non-existent path that would broaden scope.

- [x] **Step 1:** Write failing profile-generation tests (any OS): `TestSeatbeltProfileGrantsWritableRoots` (a `file-write*` rule per writable root + global `file-read*`); `TestSeatbeltProfileRejectsMaliciousPath` (a root containing a quote/newline is rejected, not emitted raw).
- [x] **Step 2:** Write failing darwin-only integration tests (guarded to darwin + binary present): `TestSeatbeltConfinesWrites` (write inside a writable root succeeds; write to a sibling outside all roots fails); `TestSeatbeltConfinesMutations` (unlink/rename/symlink targeting a read-only root fails; the same inside a writable root succeeds); `TestSeatbeltAllowsExec` (`npm --version`, `python --version` succeed wrapped).
- [x] **Step 3:** Run; expect FAIL.
- [x] **Step 4:** Implement profile generator (with escaping/rejection) + darwin `Wrap` (absolute `sandbox-exec`); register in `New()`.
- [x] **Step 5:** Run on macOS; expect PASS.
- [x] **Step 6:** Commit: `feat(shell/sandbox): macOS Seatbelt backend`.

---

### Task 4: Linux Landlock backend + bubblewrap fallback

**Files:**
- Create: `internal/shell/sandbox/landlock_linux.go`, `internal/shell/sandbox/bwrap_linux.go`
- Test: `internal/shell/sandbox/landlock_linux_test.go`

**Interfaces:**
- Produces: linux `Wrapper` preferring Landlock, falling back to `bwrap`. `Available()` = Landlock usable **or** `bwrap` usable.

**Landlock path:** re-exec ocode as a tiny confiner subcommand that sets `PR_SET_NO_NEW_PRIVS`, applies a Landlock ruleset (read+exec broad; write/mutation restricted to `WritableRoots`, covering all `LANDLOCK_ACCESS_FS_*` mutation rights — write, create, remove-file, remove-dir, make-reg, make-sym, make-dir, rename/`refer` where the ABI supports cross-root reference control), then `execve`s `bash -c …`. Probe the Landlock ABI level; if too low, degrade to `bwrap`.

**bwrap path:** build a `bwrap` argv (absolute trusted `/usr/bin/bwrap`): bind `WritableRoots` read-write, bind the rest of `/` read-only (`--ro-bind / /` then rw binds), **hide the sensitive read-set** (`auth.json`, `~/.ssh`, `.env` — mount `--tmpfs`/`--ro-bind /dev/null` over them so they can't be read; this is the Linux read-deny that Landlock can't provide), share the network namespace (egress open), and run `bash -c …`. Handle non-existent roots (skip or create-as-needed, never widen). If neither Landlock nor `bwrap` is usable while active, `Wrap` errors (fail-closed). **Landlock note:** Landlock cannot deny reads inside an allowed tree, so the Landlock path does not hide the sensitive read-set — it relies on the permission-layer check (Task 5). Prefer bwrap when sensitive read-hiding is required.

- [x] **Step 1:** Write failing test `TestLinuxBackendSelectsLandlockOrBwrap` (selection reflects ABI probe / `bwrap` lookup; neither available ⇒ `Wrap` errors and `Available()==false`).
- [x] **Step 2:** Write failing linux-only integration tests (guarded to where a backend is available): `TestLinuxConfinesWrites` (inside writable ok, outside fails); `TestLinuxConfinesMutations` (unlink/rename/symlink across into a read-only root fails); `TestLinuxAllowsExec` (`npm`/`python` run wrapped).
- [x] **Step 3:** Run; expect FAIL.
- [x] **Step 4:** Implement the confiner re-exec subcommand (`PR_SET_NO_NEW_PRIVS` + Landlock ruleset), the `bwrap` fallback argv, ABI/binary detection; register in `New()`. Route the re-exec through existing spawn conventions where applicable.
- [x] **Step 5:** Run on Linux (Landlock kernel, and a `bwrap`-only env if available); expect PASS.
- [x] **Step 6:** Commit: `feat(shell/sandbox): Linux Landlock backend + bwrap fallback`.

---

### Task 5: Sensitive-path protection (revision to Tasks 2–4)

> Tasks 2 and 3 were executed with the earlier "bypass sensitive-path" behavior. This task reworks them to the INDEX Decision 3 model. It is the security core — do not skip.

**Files:**
- Modify: `internal/agent/permissions.go` — a `sensitiveSandboxDecision(command)` helper reusing the existing `isSensitivePath`/`redact.IsSensitiveFile` machinery, called **inside** the sandbox branch of `Decide` (added in Task 2) so the sandbox auto-allow is skipped for the sensitive set: `auth.json` → HardDeny; ocode config-dir/data-dir **write** → Ask; `~/.ssh`/`.env` read or write → Ask. Applies to the statically-extractable paths of the command.
- Modify: `internal/shell/sandbox/profile_darwin.go` — add `(deny file-read* (subpath …))` / `(deny file-write* …)` rules for the sensitive set, emitted **after** the global allows so deny wins. auth.json denied read+write; config/data dir denied write; ssh/env denied read+write.
- Modify: `internal/shell/sandbox/bwrap_linux.go` — hide-mounts for the sensitive read-set (done in Task 4's bwrap path; verify here).
- Modify: `internal/shell/sandbox/roots.go` — `RootSet` gains `DenyRead []string` and `DenyWrite []string` (the resolved sensitive paths) so each backend has one source; `NewRootSet` populates them from a classified-sensitive accessor.
- Test: `internal/agent/permissions_test.go`, `internal/shell/sandbox/*_test.go`

**Interfaces:**
- Produces: `RootSet.DenyRead`, `RootSet.DenyWrite`; `sensitiveSandboxDecision`.

- [ ] **Step 1:** Write failing `Decide` tests: `TestDecideSandboxAuthJsonHardDenied` (`cat <authpath>` and a redirect-write to it → HardDeny in sandbox); `TestDecideSandboxConfigWriteAsks` (a write into the ocode config dir → Ask, not auto-allow); `TestDecideSandboxSshReadAsks` (`cat ~/.ssh/id_rsa` → Ask); `TestDecideSandboxNonSensitiveStillAutoAllows` (a plain workspace command still auto-allows — the earlier behavior is preserved for the non-sensitive case).
- [ ] **Step 2:** Write failing Seatbelt test `TestSeatbeltDeniesSensitiveRead` (generated profile contains deny rules for the sensitive set, ordered after the global allow) and a darwin integration test that a wrapped `cat <authpath>` fails.
- [ ] **Step 3:** Write failing bwrap test `TestBwrapHidesSensitiveRead` (argv contains the hide-mounts) and, where runnable, an integration test that a wrapped read of the auth path fails.
- [ ] **Step 4:** Run; expect FAIL.
- [ ] **Step 5:** Implement the `sensitiveSandboxDecision` carve-out, the `RootSet` deny fields, the Seatbelt deny rules, and confirm the bwrap hide-mounts. Document the Landlock read gap in code + docs (Part 04).
- [ ] **Step 6:** Run all + full `internal/agent` + `internal/shell/sandbox`; expect PASS.
- [ ] **Step 7:** Commit: `feat(shell/sandbox): sensitive-path protection (auth.json/ssh/env/config)`.

---

### Task 6: Self-escalation guard (agent can't shortcut its own permissions)

> Path-based confinement misses this: the files that *define* the boundary (`.ocode/settings.json`, `.claude/settings.json`) live **inside** the writable workspace, so a bare OS sandbox would auto-allow the agent rewriting them to widen its own access. This guard closes that. It is **mode-independent** — it applies in normal-auto, YOLO, and sandbox, and sits **above** every auto-allow shortcut (like the hard-block layer).

**Files:**
- Modify: `internal/agent/permissions.go` — a `isPermissionEscalation(command)` check placed near the top of the bash branch of `Decide` (alongside `isHardBlockedCommand`, **before** the YOLO/sandbox auto-allow). Returns Ask (never auto-allowable) when a command **writes** to a permission-defining target, resolved against the command's extracted write paths + redirections:
  - project `.ocode/settings.json`, `.claude/settings.json` (anywhere in the tree, not just workspace root)
  - ocode global config permissions (config dir — already write-Ask from Task 5; keep consistent)
  - any file matching the permission/allowlist config set (tool allow/deny, bash-prefix rules, `extra_allowed_paths`)
- Modify: the loopback-API exfil/escalation detectors (the existing `isExfiltrationRiskCurl/Wget/HTTPie/Netcat` family, `permissions.go:617-795`) — a loopback request whose path targets `/api/permissions*` (mode/yolo/rule endpoints) → Ask, so a shell command can't flip its own mode via the local server.
- Test: `internal/agent/permissions_test.go`

**Interfaces:**
- Produces: `isPermissionEscalation`, wired into `Decide` above the shortcuts.

- [ ] **Step 1:** Write failing tests: `TestWriteToProjectSettingsAsks` (a redirect/`tee`/`cp`/editor write to `.ocode/settings.json` inside the workspace → Ask in **sandbox and YOLO and normal-auto**, never auto-allow); `TestWriteToClaudeSettingsAsks`; `TestLoopbackPermissionApiAsks` (`curl` to `127.0.0.1:<port>/api/permissions/mode` → Ask); `TestOrdinaryWorkspaceWriteStillAutoAllows` (a normal file write in the workspace is unaffected).
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Implement `isPermissionEscalation` + the loopback-permission-endpoint check; wire both above the auto-allow shortcuts. Reuse `extractBashCommandPaths`/redirection extraction so it catches the common write forms.
- [ ] **Step 4:** Run tests + full `internal/agent`; expect PASS. Note the known limitation in code: static extraction can't catch a write hidden inside an interpreter (`python -c`) — the OS layer doesn't help here either since the file is in a writable root; document this residual (Part 04) as the reason config edits should ultimately be made outside sandbox.
- [ ] **Step 5:** Commit: `feat(permissions): self-escalation guard on permission-config writes`.
