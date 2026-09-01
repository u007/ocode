# Part 04 — Docs, framing, caveats, TODO

**Self-contained constraints recap:** Document shipped behavior, no parity claims. The write-integrity-only framing is a first-class label (mode help text + docs), not a footnote. No code snippets.

---

### Task 1: Mode model, platform matrix, and honest framing

**Files:**
- Modify: `AGENTS.md` (new "Sandbox permission mode" section); the permission-mode help/command reference; the user-facing help text for the `/sandbox` command (TUI `commands.go`, web `commands.ts`) and the mode-pill tooltip.

- [ ] **Step 1:** In the `/sandbox` help text and the mode pill/tooltip, state in one line: **"Sandbox: runs shell commands without prompts but blocks writes outside the workspace/allowed dirs. Does NOT hide files or block network — a command can still read secrets and send them out."** The label must not imply containment it doesn't provide.
- [ ] **Step 2:** Document what the mode confines: agent shell tool only; filesystem **writes** restricted to the writable roots (workspace, `extra_allowed_paths`, language dep caches — npm/pip/cargo/go/maven/gradle —, `~/.claude`, `/tmp`/`/var/tmp`/`os.TempDir()`); reads + exec global; egress open. Note the global data dir (`auth.json`, sessions) and config dir are **read-only** in sandbox.
- [ ] **Step 3:** Document the platform matrix: macOS = real (Seatbelt/`sandbox-exec`); Linux = real (Landlock ≥5.13 w/ `PR_SET_NO_NEW_PRIVS`, else `bubblewrap`); Windows = no backend → selecting sandbox behaves like `normal` (prompts), no confinement.
- [ ] **Step 4:** Document toggling: TUI permission-mode click cycle or `/sandbox`; web mode selector or `/sandbox`. Note sandbox is **per-session and not persisted** — restarts return to `normal`.
- [ ] **Step 5:** Explicitly label out-of-scope surfaces: the interactive PTY terminal (`handler_terminal.go`) and the web `!shell` path run **unsandboxed**.
- [ ] **Step 6:** Commit: `docs: sandbox mode model, platform matrix, honest framing`.

---

### Task 2: Caveats + TODO + doc-version alignment

**Files:**
- Modify: `docs/` sandbox page; `TODO.md`; `AGENTS.md` (Go version)

- [ ] **Step 1:** Document the **exfiltration residual risk** as a named limitation (not a soft caveat): sandbox protects write integrity, not confidentiality; reads and network are open by design so toolchains work.
- [ ] **Step 2:** `TODO.md` entry — **nvm**: `nvm` is a shell function, not a binary; under `bash -c` it is unavailable unless `$NVM_DIR/nvm.sh` is sourced. Orthogonal to sandboxing and **not** solved here. TODO: "Source `$NVM_DIR/nvm.sh` in the agent shell command context so `nvm` works (independent of sandbox)."
- [ ] **Step 3:** `TODO.md` entry — **Windows**: confinement intentionally degraded (sandbox = normal prompts); rationale = AppContainer needs per-dir capability-SID ACLs and breaks Node/Python toolchains. Revisit if a viable Windows FS-confinement path appears.
- [ ] **Step 4:** `TODO.md` entry — **cron sandbox**: scheduled jobs cannot select sandbox from the UI (`CronPermissionMode` left as normal/yolo/locked); blank jobs pinned to normal (Part 03 Task 4). Decide later whether to expose explicit cron sandbox.
- [ ] **Step 5:** **Go version alignment**: `go.mod` says `go 1.26.1` but `AGENTS.md` says Go 1.23 (and an embedded asset says 1.23). Update `AGENTS.md` to 1.26.1 to match `go.mod`. (Embedded model-briefing assets are model prompts — leave unless separately requested.)
- [ ] **Step 6:** On completion, explicitly tell the user which items are left as TODO: nvm sourcing, Windows confinement, cron sandbox exposure.
- [ ] **Step 7:** Commit: `docs: sandbox caveats, TODO (nvm/windows/cron), Go version alignment`.
