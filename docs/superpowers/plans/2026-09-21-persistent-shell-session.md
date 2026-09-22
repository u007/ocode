# Persistent per-session shell for `!` commands — Implementation Plan

> **For agentic workers:** implement task-by-task, ticking the `- [ ]` steps.
> Spec: `docs/superpowers/specs/2026-09-21-persistent-shell-session-design.md`
> (committed `c4205553`). This plan names files and functions — it contains no
> code; the executor writes the code.

**Goal:** One persistent, pty-backed interactive shell per chat tab for `!`
composer commands, so the user's env, aliases and functions (`cc`) are present,
the rc load is paid once per session, and shell state (cwd, exported vars,
functions) persists — with the current directory echoed in every result.

**Architecture:** A new `internal/shell.Session` owns a single `creack/pty`
shell (`$SHELL -il`, plus `-o nozle` for zsh) and frames commands with a marker
emitted by the shell's own prompt hook (`precmd` / `PROMPT_COMMAND`). The server
keeps a lazy `map[sessionKey]*shell.Session` (moved on `/reset-id`, since the
tab id is rekeyed) and routes LOCAL `POST /api/shell` requests through it; remote (`host`) requests and any pty
failure keep today's one-shot `shellpkg.Run`. The agent bash tool
(`internal/tool`) is untouched.

**Tech Stack:** Go (`github.com/creack/pty` v1.1.24, `golang.org/x/sys` v0.47.0 —
both already direct deps); `internal/shell`; `internal/server` handlers;
React/TypeScript web client (`web/src`).

**Spec:** `docs/superpowers/specs/2026-09-21-persistent-shell-session-design.md`.

## Global Constraints

- **No code in this plan.** Names of files/functions only.
- **TDD.** Every task starts with a failing test and ends with it passing.
  Mutation-verify each new test: break the behaviour, confirm the test fails,
  restore (the repo's established habit).
- **`use-modern-go` before editing Go.** Run
  `sh "skills/use-modern-go/scripts/run-tool.sh" list --file-path <file.go>` and
  apply the returned guidelines. Then `gofmt` + `go vet` on every touched
  package.
- **PTY TESTS WILL FAIL IN THIS AGENT SANDBOX.** The in-session Seatbelt sandbox
  denies `/dev/ptmx` (`posix_openpt` → EPERM), so every pty-dependent test
  (`TestTerminalWS*` and anything added here) fails in-session **at pristine
  HEAD too**. Run pty tests outside the sandbox, or first prove a failure exists
  against a `git worktree add .worktrees/pristine HEAD` baseline. Worktree
  builds need the gitignored embeds copied in:
  `cp internal/agent/models-snapshot.json` and
  `cp internal/browse/cdp/htr-assets.zip`.
- **Unix-only code must be build-tagged.** `creack/pty` is Unix-only (see the
  existing `internal/server/handler_terminal_windows.go` pattern). Split the
  session into `session_unix.go` (pty) and a `session_other.go` stub so
  `GOOS=windows go build ./...` still compiles; the server then uses the
  one-shot fallback.
- **Errors:** every caught error is logged with what was attempted before being
  returned or rethrown (the project rule); an empty `catch` is banned. The one
  deliberate degradation (pty unavailable → one-shot) must be logged, not
  silent.
- **No git hazards:** never `git reset`/`git restore --staged .`/`git stash`.
  The tree carries concurrent work from other processes — stage specific paths
  only.
- **Commit policy:** do not commit unless the user asks; the spec is already
  committed.
- **Docs in the same change:** CHANGES.md + a concept page via the context agent
  (docs/ is bundle-owned); do not hand-edit `docs/index.md`.

---

### Task 1: `internal/shell.Session` — the pty-backed persistent shell

**Files:**
- Add: `internal/shell/session_unix.go` (`//go:build !windows`)
- Add: `internal/shell/session_other.go` (`//go:build windows`) — returns a
  typed "unsupported" error
- Add: `internal/shell/session.go` — platform-neutral types/options
- Test: `internal/shell/session_unix_test.go`

**Interfaces (produces):**
- `type SessionOptions struct { Shell, Dir string; Env []string; Timeout, Grace time.Duration; Cols, Rows uint16 }`
  — `Env` is **appended** to the process environment after the session's own
  overrides (`HISTFILE`, `TERM`, …); Go's `exec.Cmd` keeps the last value for a
  duplicate key, so a test can point `ZDOTDIR`/`HOME` at a temp dir through it.
- `func NewSession(opts SessionOptions) (*Session, error)`
- `func (s *Session) Run(ctx context.Context, command string) (Result, string, error)`
  — returns the existing `Result` plus the shell's cwd after the command
- `func (s *Session) Close() error`
- `func (s *Session) Alive() bool`

**Interfaces (consumes):** `shell.Resolve("")` and `shell.DefaultTimeout`
(`internal/shell/shell.go`, `Result` at :126), `pty.Start`/`pty.Setsize`.

- [x] Write failing tests in `session_unix_test.go` covering, in order:
      (a) env/functions present — `whence -w cc`-style probe resolves a function
      defined by a **test-owned rc file** (never the developer's real rc): the
      test writes `.zshrc` (and `.zshenv`) into a temp dir and passes
      `ZDOTDIR=<tmp>` and `HOME=<tmp>` via `SessionOptions.Env`; for bash the
      test writes `.bash_profile` and passes `HOME=<tmp>`;
      (b) exit code + cwd returned for success and failure;
      (c) state persists — `cd` then `pwd`, `export` then `echo`;
      (d) output contains no prompt text and no ANSI escapes;
      (e) the history file is never created/written (point `HISTFILE` at a temp
      path in the test rc and assert it stays absent);
      (f) timeout → SIGINT → the shell is either recovered or respawned, and the
      next `Run` succeeds;
      (g) two concurrent `Run` calls on one session serialize and both return
      correct results;
      (h) multi-line — a command holding two complete lines returns both lines'
      output in one result with the last line's exit code, and the next `Run`
      returns only its own output; a command with an unterminated quote returns
      a non-zero exit well inside a short test timeout (no hang);
      (i) rc hooks silenced — the test rc registers `precmd`/`preexec` hooks via
      `add-zsh-hook` that print sentinel text, and the result contains none of
      it;
      (j) cwd with spaces — `cd` into a temp dir whose name contains a space,
      and the returned cwd is the full path;
      (k) a command whose text contains the heredoc delimiter line is rejected
      with an error and nothing is written to the shell.
- [x] Run them; confirm they fail for the expected reason (no `Session` type).
- [x] Implement `NewSession`: resolve the shell, spawn on a pty with
      `Dir`/`Env`, set a wide winsize, send the prelude once (echo/prompt/history
      suppression, `precmd_functions=() preexec_functions=()` / `trap - DEBUG`
      hook reset, then the completion-marker hook), then the synchronizing
      sentinel; drain everything up to it.
- [x] Implement `Run`: serialise on a mutex, reject a command containing the
      delimiter line, write the command wrapped in the
      `eval "$(cat <<'__OCODE_CMD_<nonce>__' … )"` unit from the spec, read until
      the marker (or `ctx` deadline), parse exit status + cwd (**everything after
      the status field is the cwd**), strip the marker, residual ANSI, the echoed
      command line, and boundary blank lines.
- [x] Implement timeout/recovery per the spec: `\x03`, grace window, then
      SIGHUP + respawn; detect an exited shell and respawn on next `Run`.
- [x] Run the tests; green. Then `gofmt` + `go vet ./internal/shell/...` and
      `GOOS=windows go build ./internal/shell/`.
- [x] Mutation-verify: break marker parsing (never match the marker) → (b),(c)
      fail; drop the timeout path → (f) fails; drop history suppression → (e)
      fails; drop the heredoc wrap → (h) fails; drop the hook reset → (i)
      fails. Restore each time.
- [ ] Ready to commit: `feat(shell): pty-backed persistent interactive shell session`.

---

### Task 2: Server registry, `/api/shell` wiring, and lifecycle

**Files:**
- Add: `internal/server/shell_sessions.go` — the per-key registry/reaper
- Modify: `internal/server/handler.go` — `HandleShellCommand` (:2062, local
  branch at `shellpkg.Run` :2090) and the `Handler` struct next to
  `terminalProcs` (:132); `handleRemoteShellCommand` (:2109) adds `cwd` (the
  remote path) to its response
- Modify: `internal/server/handler_close.go` — `HandleCloseSession` (:28):
  close the shell for `id` **first and unconditionally**, before the
  `sessions.Resolve` 404 and before the `ReleaseAgent`/close-pending branch.
  The shell has no in-flight-turn dependency, so it does not go through
  `drainPendingClose`.
- Modify: `internal/server/handler_reset_id.go` — `HandleResetSessionID`
  (:34): after `session.RekeyForDir` succeeds, call the registry's
  `Rekey(oldID, newID)`
- Modify: `internal/server/handler_shutdown.go` — close every shell alongside
  `(*Handler).shutdownTerminals` (:149, called from `:24`)
- Test: `internal/server/handler_shell_session_test.go`

**Interfaces:**
- Consumes: `shellpkg.Session` from Task 1; `defaultSessionIdleTimeout`
  (`internal/server/session_manager.go:322`); the existing route
  `POST /api/shell` (`internal/server/server.go:331`).
- Produces: request body `{command, workDir, host, session, reset}`; response
  `{output, exitCode, error, cwd}` on **every** path — persistent shell
  (`$PWD`), one-shot fallback (the `workDir` it ran in), remote (the remote
  path) — so the client never defaults a missing field. `session` is opaque;
  when empty the handler keeps today's one-shot path (back-compat for any
  client that has not been updated). `reset: true` closes any shell for the
  key first; it is server-only in v1 (no client sends it).
- Registry constructor takes the pieces the tests need injected, in the
  existing `newTerminalRegistry` style: the session factory (`func(SessionOptions)
  (*shell.Session, error)`, so a failing factory exercises the fallback), the
  idle timeout, and a `now func() time.Time` for the reaper.
- Project switch (spec "Resolved decisions"): the registry records the
  `workDir` each shell was spawned with; a request whose `workDir` differs
  `cd`s there before running and records the new value.

- [x] Write failing tests: (a) two sequential local requests with the same
      `session` share one shell (a `cd` in the first is visible in the second);
      (b) a different `session` gets an independent shell; (c) a request without
      `session` still works one-shot; (d) `reset: true` gives a fresh cwd;
      (e) `host` requests are untouched (still `handleRemoteShellCommand`);
      (f) the response carries `cwd`; (g) `HandleCloseSession` drops the shell,
      including for an id `sessions.Resolve` does not know; (h) the idle reaper
      drops an unused shell (drive it with the injected clock, no sleeping);
      (i) fallback — a session factory that returns an error makes the handler
      run one-shot, log the reason, and still return `cwd` = `workDir`;
      (j) the one-shot path without `session` and the `host` path both return
      `cwd`; (k) project switch — a second request with a different `workDir`
      runs there and reports it as `cwd`, and a `cd` inside the same `workDir`
      persists; (l) rekey — after `HandleResetSessionID`, a request under the
      new id sees the `cd` made under the old id, and the old key is gone.
- [x] Run them; confirm they fail (unknown fields are ignored today, so (a)
      fails on the missing `cwd`/shared state).
- [x] Implement the registry in `shell_sessions.go`: lazy create via the
      injected factory, per-key mutex, `workDir` tracking + rebase `cd`, idle
      reaper on the injected clock, `Rekey`, `CloseAll`, and per-key `Close`.
- [x] Wire `HandleShellCommand`: parse `session`/`reset`, route local requests
      through the registry, fall back to `shellpkg.Run` — logging the reason —
      when the factory fails or the platform is Windows; add `cwd` to the
      one-shot and remote responses.
- [x] Wire teardown at the top of `HandleCloseSession` (unconditional, before
      the 404 and the close-pending branch), `Rekey` in `HandleResetSessionID`,
      and `CloseAll` in `handler_shutdown.go`.
- [x] Run `go test ./internal/server/ -run 'Shell' -v` plus
      `go test ./internal/shell/...`; green (remember the pty sandbox caveat).
- [x] Mutation-verify: remove the teardown call → (g)/(h) fail; ignore `session`
      → (a)/(b) fail; drop `cwd` → (f)/(j) fail; drop the `Rekey` call → (l)
      fails; drop the rebase `cd` → (k) fails.
- [ ] Ready to commit: `feat(server): persistent per-session shell for ! commands`.

---

### Task 3: Frontend — pass the session key and surface the cwd

**Files:**
- Modify: `web/src/api/client.ts` — `shellCommand` (:1415 adds a fourth
  `session` argument and `cwd` to the response type; **no** `reset` — nothing
  in the client sends it in v1)
- Modify: `web/src/hooks/useChat.ts` — `executeShell` (:291) forwards the tab id
  (`useChat`'s first argument)
- Modify: `web/src/components/Chat/ChatInput.tsx` — the `!` result message
  (:275-277) renders the returned cwd
- Test: `web/src/hooks/useChat.shellHost.test.tsx` (extend),
  `web/src/components/Chat/ChatInput.test.tsx` (extend)

**Interfaces:**
- Consumes: the Task 2 body/response shape. `cwd` is present on every server
  path, so the client types it as required and renders it without a default;
  the `catch` branch in `executeShell` (network failure, no server response)
  sets `cwd` to the `projectPath` it would have run in.
- Produces: no new exports; `executeShell` keeps its
  `{output, exitCode, error}` shape plus `cwd`.

- [x] Write failing tests: `executeShell` sends the tab id as `session`;
      the rendered `!` message contains the cwd line on success and on failure.
- [x] Run them; confirm they fail.
- [x] Implement: extend `shellCommand`/`executeShell`, render `# cwd: <path>`.
      **Arity trap:** any `toHaveBeenCalledWith` assertion must be updated to the
      new explicit argument list — vitest distinguishes 3 args from 4 with an
      `undefined` tail, and this also pins that the key is threaded.
- [x] Run `pnpm vitest run src/hooks/useChat.shellHost.test.tsx src/components/Chat/ChatInput.test.tsx`,
      then `pnpm typecheck` and `pnpm build`.
- [x] Mutation-verify: drop the `session` argument → the host/threading test
      fails.
- [ ] Ready to commit: `feat(web): run ! commands in the session's persistent shell`.

---

### Task 4: Docs, changelog, and an end-to-end verification pass

**Files:**
- Modify: `CHANGES.md`
- Add: concept page via the **context agent** (docs/ is bundle-owned)
- Modify: `TODO.md` if any spec item is deferred

- [x] Dispatch the context agent to write a concept page for the persistent `!`
  shell (marker framing, pty cleanliness, lifecycle, fallback) and update the
  index; do not hand-edit `docs/index.md`.
- [x] Add a `CHANGES.md` entry naming the files, the behaviour change, the
  fallback, and the known limits (pagers/editors/stdin-reading commands time
  out; `TERM=dumb` means no colour in `!` output).
- [x] End-to-end verification **outside the sandbox** (PTY): start the server,
  `POST /api/shell` with a `session` key — assert `cd`/`export` persist, `cc`
  resolves as a function, `cwd` is echoed, a second key is independent, and
  tab-close drops the shell.
  > **DONE 2026-09-21.** The implementing session was initially confined by
  > ocode's Seatbelt sandbox (no pty), so the real-pty tests only skipped. After
  > fixing the sandbox device grants (Seatbelt `/dev/ptmx` + `/dev/ttys*`;
  > Landlock/bwrap `/dev/ptmx` + `/dev/pts` — see CHANGES.md), all 11
  > `internal/shell/session_unix_test.go` tests run and pass, covering rc
  > functions, exit code + cwd, state persistence, clean output, history
  > suppression, timeout recovery, serialisation, multi-line/incomplete input,
  > rc-hook silencing, spaces in cwd, and delimiter rejection. That first real
  > run also surfaced two bugs the pty-free fake could not: a startup desync and
  > a `PROMPT_SP` space blob, both now fixed and mutation-verified.
- [x] Regression: `go build ./...`, `go vet`, `gofmt -l`, the touched Go suites,
  the web suite, `pnpm typecheck`, `pnpm build`. Compare any failure against the
  pristine worktree before blaming this change (the pty sandbox and
  `PreviewSurface.integration` are known environmental failures).

## Explicitly out of scope (do not implement here)

- Making the **agent bash tool** (`internal/tool`) use a persistent/interactive
  shell — it depends on per-command `cmd.Dir`, sandbox wrap, process-group
  cancel, and background promotion.
- Persistent shells for **remote** (`host`) `!` commands.
- A `/shell-reset` UI affordance — v1 ships the `reset` request flag only.
