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

- [ ] **Step 1:** Write failing tests `TestNoopWrapPassthrough` (cmd returned unchanged, nil error) and `TestSupportedMatchesGOOS` (true on darwin/linux, false on windows — table by build tag or a runtime check).
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Define interface, no-op, `New()`, `Supported()`. `New()` returns no-op where no real backend exists so all targets compile before Tasks 3–4.
- [ ] **Step 4:** Run tests; `GOOS=windows go build ./...` and `GOOS=linux go build ./...`.
- [ ] **Step 5:** Commit: `feat(shell/sandbox): Wrapper interface + no-op backend`.

---

### Task 2: Fail-closed wiring (fg + bg) + Decide decision matrix

**Files:**
- Modify: `internal/tool/bash_build.go` → finalise `buildBashCmd(ctx, command, dir, w sandbox.Wrapper, roots sandbox.RootSet, active bool) (*exec.Cmd, error)`. When `active`: if `!w.Available()` return an error (fail-closed); else return `w.Wrap(cmd, roots)`. When `!active`: return the plain cmd, nil.
- Modify: foreground `internal/tool/exec.go` and **background** `internal/tool/process.go:257-287` — call the new signature. **The wrap/error must occur before `cmd.Start()` and before any `ProcessRegistry`/`StartBackgroundDisplay` record is created**, so a wrap failure leaves no phantom process record.
- Modify: `BashTool` (`internal/tool/exec.go:45`) + construction (`internal/agent/agent.go:934`) to receive the `Wrapper` and a roots+mode provider from the agent's `PermissionManager` (dynamic — see Part 03 subagent note; resolve `NewRootSet(pm.AllowedRootsClassified())` and `pm.Mode()` fresh per command so `extra_allowed_paths` and mode changes take effect immediately).
- Modify: `internal/agent/permissions.go` `Decide()` bash branch (`:1179-1260`) — insert the sandbox auto-allow at the **same position as the YOLO shortcut (`:1204`)**: after `isHardBlockedCommand`/Claude-deny/dangerous-`rm`, before interpreter/heredoc/compound/prefix/sensitive-path checks. Decision matrix below.
- Test: `internal/tool/bash_build_test.go`, `internal/agent/permissions_test.go`

**Decision matrix (sandbox mode, supported OS):**

| Check (in `Decide` order) | Normal | YOLO | **Sandbox** |
|---|---|---|---|
| `isHardBlockedCommand` (`rm -rf /`, forkbomb, `| sh`, `; sudo`, `dd if=`, `mkfs`) | HardDeny | HardDeny | **HardDeny** |
| Claude-settings deny | HardDeny | HardDeny | **HardDeny** |
| dangerous-`rm` heuristic | Ask | Ask | **Ask** |
| interpreter/heredoc, sensitive-path READ, redirection, prefix scope | Ask/prompt | bypass | **bypass** (OS confines writes; reads are intentionally open) |
| everything else | scope-checked | Allow | **Allow, OS-wrapped** |

Rationale: sandbox = YOLO's prompt-bypass **plus** OS write-confinement. Hard blocks and dangerous-`rm` remain authoritative as belt-and-suspenders. Sensitive-path *read* prompts are intentionally bypassed (write-integrity only).

- [ ] **Step 1:** Write failing builder tests: `TestBuildBashCmdFailsClosedForeground` (active + stub `Available()==false` ⇒ error, no cmd); `TestBuildBashCmdFailsClosedBackgroundNoRecord` (bg path: wrap failure ⇒ error **and** `ProcessRegistry` has no record for it); `TestBuildBashCmdWrapsWhenActive` / `TestBuildBashCmdSkipsWhenInactive`.
- [ ] **Step 2:** Write failing `Decide` tests: `TestDecideSandboxAutoAllowsPlainCommand`; `TestDecideSandboxHardDenyStillWins` (`rm -rf /`); `TestDecideSandboxDangerousRmStillAsks`; `TestDecideSandboxBypassesInterpreterPrompt` (a `python -c` heredoc auto-allows in sandbox but the same in normal asks); `TestDecideSandboxDegradesOnUnsupportedOS` (mode sandbox, `Supported()==false` ⇒ behaves like normal).
- [ ] **Step 3:** Run; expect FAIL.
- [ ] **Step 4:** Implement the fail-closed builder (fg+bg, pre-Start/pre-register), the `BashTool` provider wiring, and the `Decide` matrix branch.
- [ ] **Step 5:** Run all + full `internal/tool` + `internal/agent`; expect PASS.
- [ ] **Step 6:** Commit: `feat(shell/sandbox): fail-closed wrap (fg+bg) + Decide sandbox matrix`.

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

- [ ] **Step 1:** Write failing profile-generation tests (any OS): `TestSeatbeltProfileGrantsWritableRoots` (a `file-write*` rule per writable root + global `file-read*`); `TestSeatbeltProfileRejectsMaliciousPath` (a root containing a quote/newline is rejected, not emitted raw).
- [ ] **Step 2:** Write failing darwin-only integration tests (guarded to darwin + binary present): `TestSeatbeltConfinesWrites` (write inside a writable root succeeds; write to a sibling outside all roots fails); `TestSeatbeltConfinesMutations` (unlink/rename/symlink targeting a read-only root fails; the same inside a writable root succeeds); `TestSeatbeltAllowsExec` (`npm --version`, `python --version` succeed wrapped).
- [ ] **Step 3:** Run; expect FAIL.
- [ ] **Step 4:** Implement profile generator (with escaping/rejection) + darwin `Wrap` (absolute `sandbox-exec`); register in `New()`.
- [ ] **Step 5:** Run on macOS; expect PASS.
- [ ] **Step 6:** Commit: `feat(shell/sandbox): macOS Seatbelt backend`.

---

### Task 4: Linux Landlock backend + bubblewrap fallback

**Files:**
- Create: `internal/shell/sandbox/landlock_linux.go`, `internal/shell/sandbox/bwrap_linux.go`
- Test: `internal/shell/sandbox/landlock_linux_test.go`

**Interfaces:**
- Produces: linux `Wrapper` preferring Landlock, falling back to `bwrap`. `Available()` = Landlock usable **or** `bwrap` usable.

**Landlock path:** re-exec ocode as a tiny confiner subcommand that sets `PR_SET_NO_NEW_PRIVS`, applies a Landlock ruleset (read+exec broad; write/mutation restricted to `WritableRoots`, covering all `LANDLOCK_ACCESS_FS_*` mutation rights — write, create, remove-file, remove-dir, make-reg, make-sym, make-dir, rename/`refer` where the ABI supports cross-root reference control), then `execve`s `bash -c …`. Probe the Landlock ABI level; if too low, degrade to `bwrap`.

**bwrap path:** build a `bwrap` argv (absolute trusted `/usr/bin/bwrap`): bind `WritableRoots` read-write, bind the rest of `/` read-only (`--ro-bind / /` then rw binds), share the network namespace (egress open), and run `bash -c …`. Handle non-existent roots (skip or create-as-needed, never widen). If neither Landlock nor `bwrap` is usable while active, `Wrap` errors (fail-closed).

- [ ] **Step 1:** Write failing test `TestLinuxBackendSelectsLandlockOrBwrap` (selection reflects ABI probe / `bwrap` lookup; neither available ⇒ `Wrap` errors and `Available()==false`).
- [ ] **Step 2:** Write failing linux-only integration tests (guarded to where a backend is available): `TestLinuxConfinesWrites` (inside writable ok, outside fails); `TestLinuxConfinesMutations` (unlink/rename/symlink across into a read-only root fails); `TestLinuxAllowsExec` (`npm`/`python` run wrapped).
- [ ] **Step 3:** Run; expect FAIL.
- [ ] **Step 4:** Implement the confiner re-exec subcommand (`PR_SET_NO_NEW_PRIVS` + Landlock ruleset), the `bwrap` fallback argv, ABI/binary detection; register in `New()`. Route the re-exec through existing spawn conventions where applicable.
- [ ] **Step 5:** Run on Linux (Landlock kernel, and a `bwrap`-only env if available); expect PASS.
- [ ] **Step 6:** Commit: `feat(shell/sandbox): Linux Landlock backend + bwrap fallback`.
