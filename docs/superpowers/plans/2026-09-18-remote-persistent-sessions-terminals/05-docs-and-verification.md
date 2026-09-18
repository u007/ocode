# Part 5: Docs and Verification

**Spec:** `docs/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md`.

**Context for this part.** By now: a remote project's terminal websocket and terminal HTTP calls go through `/api/remote/{host}/api/terminal/*` and the pty is a child of `ocode serve --remote` on the host; the proxy rewrites the `ocode.bearer.<token>` websocket subprotocol both ways; a version-mismatched alive remote server is reused with `ServeState.Outdated`; the local server exposes `GET /api/remote/{host}/status`, `POST /api/remote/{host}/connect`, `POST /api/remote/{host}/restart`, and any server exposes `GET /api/terminal`; the sidebar's remote project row shows version, chat and terminal counts, Connect, Restart, and an expandable reattach list; the event bus and terminal panel reconnect immediately on wake.

Docs are the source of truth in this repo. The terminal section of `AGENTS.md` ("Remote projects: chat runs on the host, terminal/files stay per-request") is now wrong and must be corrected.

---

### Task 12: Update docs and verify manually

**Files:**
- Modify: `AGENTS.md` (the "Remote projects: chat runs on the host, terminal/files stay per-request" section: terminals now also run on the host for sidebar remote projects; the Files tab, git, `!` commands, and port forwards still use per-request ssh/wsl.exe; add the version policy: mismatched alive server is reused and flagged, restart is explicit and unguarded; add the rule that the proxy must restore the browser's websocket subprotocol on 101)
- Create: `docs/concepts/remote-persistent-sessions-terminals.md` (front matter matching `docs/concepts/server-auto-continue.md`: how terminals persist on the host, the 24 h detach TTL, reuse-on-mismatch, the three lifecycle endpoints, the sidebar inventory, wake reconnect, and what does not survive: host reboot or remote server crash)
- Modify: `docs/index.md` (link the new concept doc where the other concept docs are listed)
- Modify: `docs/log.md` (dated entry, same format as existing entries)
- Modify: `CHANGES.md` (user-facing entry under the unreleased heading)
- Modify: `skills/ocode-usage/SKILL.md` (a short paragraph on remote terminal persistence and the sidebar Restart / Connect actions, where remote projects are described)
- Modify: `web/src/lib/trustedProject.ts` only if the manual run shows the trusted-project gate blocking the proxied terminal path; otherwise untouched

- [ ] **Step 1: Write the docs** listed above. Every statement must match the implemented behaviour; read the handlers and components before writing.
- [ ] **Step 2: Run** `go test ./internal/remote/... ./internal/server/...` and `cd web && pnpm test`. Expected: all PASS. Then `cd web && pnpm build` and the Go build for the desktop binary as `Makefile` describes. Expected: no errors.
- [ ] **Step 3: Manual verification (SSH)**, recording the outcome of each in the commit message body:
  1. Add or select a remote SSH project. Open a terminal, run `top`.
  2. Sleep the laptop for at least three minutes, wake it. Within a few seconds the terminal reconnects and `top` is still running.
  3. Quit and relaunch the desktop app. The terminal tab restores and attaches to the same shell.
  4. Turn Wi-Fi off for one minute and on again. Chat stream and terminal reconnect without a manual reload.
  5. Clear the browser's localStorage for the app. The sidebar row for the project, expanded, lists the terminal; clicking it reattaches.
  6. On the host, note the pid from the status line. Bump `internal/version/version.go` locally, rebuild, relaunch. The row shows the amber outdated marker. Click Restart. The row shows the new version and a new pid; the old terminal reports exited; a chat opened before the restart resumes.
- [ ] **Step 4: Manual verification (WSL)**, on a Windows machine with a `wsl:<distro>` project: repeat items 1, 3, and 6. Sleep/wake is item 2 there as well.
- [ ] **Step 5: Commit** `docs: remote persistent sessions and terminals` with the verification notes in the body.
