# Part 04 — Docs and End-to-End Verification

Self-contained. Spec: `docs/superpowers/specs/2026-09-17-remote-project-agent-on-host-design.md`.

Constraints: surgical doc edits, no code in this plan, report verification results faithfully (a failed manual check is reported as failed, not glossed), do not commit unless the user asks.

State after the earlier parts: a sidebar remote SSH/WSL project's chat agent runs on an `ocode serve --remote` process on that host; the local server proxies `/api/remote/{host}/api/*` to it; the SPA derives the host per session tab and prefixes chat, session, event, permission, model, and agent-run calls; the remote server expands `~` in project paths against its own home. Terminal, Files, git, `!`, and port forwards still use their per-request ssh/wsl.exe paths.

---

### Task 1: Update prompt wording, agent briefing, architecture doc, changelog; verify on real hosts

**Files:**
- Modify: `internal/agent/prompt.go` line 284 — the `Project host:` environment line. It currently says the config/session/runtime paths belong to the machine running the agent, which is now the same machine as the project. Reword to state that the agent runs on that host and that the project files, shell, home, and config paths below are all on it. Update the assertion in `internal/agent/context_test.go` or `prompt_test.go` that pins this string.
- Modify: `AGENTS.md` — the "Web/Desktop Server: project dirs are per-session" section (around line 755) and the environment-prompt section (around lines 937–943) both say per-project remote projects execute their chat agent on the local server. Replace with the new model: chat/session traffic for a remote project is proxied to the host's `ocode serve --remote`; terminal/files/git/`!`/port forwards remain ssh-per-request; `~` is expanded only by the server owning that home. Add the trust rule: only saved project hosts are proxied, and the remote token never reaches the browser.
- Modify: `docs/architecture/terminal-detach-reattach.md` — the sentence stating the agent runs locally for remote projects.
- Modify: `docs/gotchas/remote-project-path-trust-boundary.md` — add one paragraph under "Required invariant" noting the proxy route admits only saved `(host, path)` pairs and never consults the local path allowlist.
- Modify: `docs/index.md` — ensure the spec entry from 2026-09-17 is present and add this plan directory under the plans list.
- Modify: `CHANGES.md` — one entry under the unreleased section: remote SSH/WSL projects now run their chat agent on the host; bash uses the remote login shell and home; `~` project paths expand on the remote.
- Modify: `TODO.md` — add entries for the spec's out-of-scope items that are real follow-ups: moving terminal/files/git to the remote server's native endpoints, cross-host session aggregation in global views, re-homing open tabs when a remote project's host is edited.

- [ ] Update `prompt.go` wording and its test; run `go test ./internal/agent/...`.
- [ ] Edit `AGENTS.md`, the architecture doc, the gotcha, `docs/index.md`, `CHANGES.md`, `TODO.md`.
- [ ] Run the full suites: `go test ./...` and `cd web && pnpm test && pnpm build`.
- [ ] Manual verification on a real SSH project (a saved project whose path starts with `~/`): open a chat tab on it, ask the agent to run `echo $HOME && pwd && ls`; expected output shows the remote home and the expanded project path. Ask it to read a file that exists only on the remote; expected the file content. Switch the sidebar to a local project and send another message in the remote tab; expected it still answers from the remote. Open a second tab on a local project at the same time; expected both work. Kill the ssh tunnel process; expected the next message shows the remote-connect error and the one after reconnects.
- [ ] Manual verification on a WSL project (Windows): same `echo $HOME && pwd` check; expected WSL home and path, with no ssh process spawned.
- [ ] Record the outcome of each manual step in the final report, including any step that could not be run and why.
- [ ] Ready to commit: `docs: remote project chat agent runs on the host`.
