# Part 04 — Docs, framing, caveats, TODO

**Self-contained constraints recap:** Document shipped behavior, no parity claims. The write-integrity-only framing is a first-class label (mode help text + docs), not a footnote. No code snippets.

---

### Task 1: Mode model, platform matrix, and honest framing

**Files:**
- Modify: `AGENTS.md` (new "Sandbox permission mode" section); the permission-mode help/command reference; the user-facing help text for the `/sandbox` command (TUI `commands.go`, web `commands.ts`) and the mode-pill tooltip.

- [x] **Step 1:** `/sandbox` help text (TUI `commands.go:146`, web `commands.ts:77`) + CoworkSidebar mode-pill tooltip now state the one-line framing verbatim (write protection, not full containment; secrets/config still ask).
- [x] **Step 2:** AGENTS.md "Sandbox permission mode" section documents what's confined (shell tool only, OS write-walls to writable roots, global reads/exec, egress open), the sensitive set (auth.json, config/data-dir writes, ~/.ssh, .env — all Ask → judge when auto on), the self-escalation guard, and the interpreter-hidden residual plainly.
- [x] **Step 3:** Platform matrix documented in AGENTS.md (macOS Seatbelt, Linux Landlock+bwrap, Windows normal-prompts degrade).
- [x] **Step 4:** Toggling documented (TUI cycle / `/sandbox`, web selector / `/sandbox`; per-session, never persisted).
- [x] **Step 5:** Out-of-scope surfaces labeled (PTY terminal + web `!shell` unsandboxed) in AGENTS.md.
- [x] **Step 6:** Commit: `docs: sandbox mode model, platform matrix, honest framing`.

---

### Task 2: Caveats + TODO + doc-version alignment

**Files:**
- Modify: `docs/` sandbox page; `TODO.md`; `AGENTS.md` (Go version)

- [x] **Step 1:** Exfiltration residual documented as a named limitation (AGENTS.md: "write-integrity confinement only — reads and network are open by design").
- [x] **Step 2:** `TODO.md` entry — **nvm**: shell function, not binary; `$NVM_DIR/nvm.sh` sourcing left as future work (independent of sandbox).
- [x] **Step 3:** `TODO.md` entry — **Windows**: confinement degraded to normal prompts (AppContainer per-dir capability-SID ACLs break Node/Python toolchains); revisit if a viable path appears; web surfaces "degraded_normal".
- [x] **Step 4:** `TODO.md` entry — **cron sandbox**: `CronPermissionMode` left normal/yolo/locked; blank jobs pinned to normal (`resolveCronPermissionMode`); decide later on explicit cron sandbox.
- [x] **Step 5:** **Go version alignment**: `AGENTS.md` Tech Stack updated to Go 1.26.1 (matches `go.mod`). Embedded model-briefing assets left untouched (model prompts, out of scope per plan).
- [x] **Step 6:** On completion, explicitly tell the user which items are left as TODO: nvm sourcing, Windows confinement, cron sandbox exposure.
- [x] **Step 7:** Commit: `docs: sandbox caveats, TODO (nvm/windows/cron), Go version alignment`.
