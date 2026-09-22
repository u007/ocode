# Persistent per-session shell for `!` commands (design)

Date: 2026-09-21
Status: approved (Approach B — pty-backed)

## Problem

`!command` in the web/desktop composer POSTs to `/api/shell`, which runs
`<shell> -l -c <command>` in a fresh process (`internal/shell.Run`) and returns
combined output plus an exit code. That has two consequences:

1. **The user's env and functions are missing.** The shell is a *login* shell:
   it sources `/etc/zprofile` + `~/.zprofile`, but never `~/.zshrc`. Everything
   an interactive shell sets up is absent — `~/.local/bin` (added by
   `~/.local/bin/env`), nvm/pyenv/mise, and every shell **function/alias** such
   as `cc`. Verified on this machine: `whence -w cc` → `cc: command` (the C
   compiler) under `-lc`, `cc: function` under `-ilc`.
2. **The rc load is paid per command.** Switching to `$SHELL -ilc` costs
   ~0.4–0.5s warm (3.1s cold) *every* command.

The terminal panel already behaves correctly for interactive use — it is a
persistent, pty-backed `-l` shell (interactive because it has a tty) — but `!`
commands do not go through it.

## Goal

A **persistent, per-chat-session shell** for `!` commands so that:

- the user's env, aliases and functions (`cc`) are present,
- the rc load is paid once per session,
- shell state (cwd, exported vars, functions) persists across `!` commands,
- every result reports the shell's current directory, so a leaked `cd` is
  visible in the transcript.

## Non-goals

- **Not the agent bash tool** (`internal/tool`). It relies on per-command
  `cmd.Dir`, per-command sandbox wrap (`w.Wrap(cmd, roots)`), per-command
  process-group cancel via `setProcGroup`/`CommandContext`, and foreground→
  background promotion. A shared shell breaks all four. Agent commands keep the
  non-interactive shell; `EnsureUserBinPath` already covers `~/.local/bin`.
- **Not remote `!` commands** (`host` set). Those stay one-shot over ssh; a
  persistent remote shell is a separate project.
- **Not Windows.** No pty; one-shot `cmd /C` as today.

## Current behavior (verified)

- Route `POST /api/shell` is `authMiddleware`-only — there is **no** agent
  permission gate. `!` is user-initiated, the same trust level as the terminal.
- Body `{command, workDir, host}`; `workDir` defaults to the project path.
- Local: `shellpkg.Run(command, workDir)` → `Build` →
  `<resolved shell> -l -c command`, `setProcGroup` (Setsid), stdout+stderr
  combined, `DefaultTimeout = 600s`.
- Remote: `handleRemoteShellCommand` → ssh, executed by the remote login shell.

## Design

### Keying and lifecycle

- `useChat(sessionId)`'s first argument is the **tab id** — "a real session id, a
  `new-*` draft id, or a temp tab id" (see its doc comment). `executeShell`
  passes it as a new body field `session`.
- The tab id is **not** stable: `rekeySession(old, new)` (`web/src/App.tsx`,
  `chatStore` `REKEY_SESSION`) renames it on `/reset-id` **and** on the
  `new-*` → real-session rename after the first send. The registry therefore
  exposes `Rekey(oldKey, newKey)`, and `HandleResetSessionID`
  (`internal/server/handler_reset_id.go`, right after `session.RekeyForDir`
  succeeds) moves the shell to the new key so `cd`/env survive a `/reset-id`.
- The `new-*` → real-id rename happens client-side only (the server never sees
  the draft id), so a shell created by a `!` in a draft tab **before the first
  message** stays keyed under `new-<ts>`, is not reused after the rename, and
  is not closed by tab close (`closeSessionBackend` skips `new-*` ids). It is
  reaped by the idle timeout. This is an accepted v1 limitation; the cost is
  one extra rc load for that tab.
- The server keeps `map[key]*shell.Session`, created lazily on the first `!` for
  that key.
- Closed on `POST /api/sessions/{id}/close` (`HandleCloseSession` in
  `internal/server/handler_close.go`) — already called by
  `api.closeSession(tabId)` on tab close. The shell has no in-flight-turn
  dependency, so it is closed **unconditionally and immediately** in that
  handler (before the agent release / close-pending branching), not threaded
  through `drainPendingClose`. Backstops: an idle timeout (30 min, matching
  `defaultSessionIdleTimeout`) and server shutdown.
- One command at a time per key (mutex + FIFO queue). Concurrent `!` commands
  from two tabs of the same session serialize rather than interleave.

### `internal/shell.Session`

- Spawn once: `pty.Start(exec.Command(resolvedShell, spawnArgs...))` with
  `cmd.Dir = workDir` and `cmd.Env = sessionEnv()`.
  - `spawnArgs`: `-il`, plus `-o nozle` when the resolved shell is zsh
    (disables ZLE line editing/redraw).
  - `sessionEnv()` (process env plus): `HISTFILE=` (empty, so zsh never opens,
    locks, or writes history), `TERM=dumb`, `NO_COLOR=1`, `PAGER=cat`,
    `GIT_PAGER=cat`, `GIT_TERMINAL_PROMPT=0`. (`PROMPT`, `PROMPT2`, `RPROMPT`
    and `PROMPT_EOL_MARK` are shell *parameters*, not environment variables, so
    they are set in the prelude below, not here.)
  - Wide window (`pty.Setsize`, e.g. 400×50) so long commands do not wrap.
- **Prelude** sent once after start and discarded:
  - `stty -echo -onlcr 2>/dev/null` — kill tty echo and CRLF translation;
  - `unsetopt zle` (zsh; redundant with `-o nozle`);
  - `PROMPT='' RPROMPT='' PROMPT2='' PROMPT_EOL_MARK=''`;
  - `HISTFILE=; unset HISTFILE; unsetopt inc_append_history share_history`
    (zsh) / `set +o history` (bash);
  - neutralise rc-installed hooks, which defining `precmd()` alone does not
    remove: `precmd_functions=() preexec_functions=(); unset -f preexec
    2>/dev/null` (zsh; `add-zsh-hook` users such as starship, powerlevel10k,
    iTerm2 shell integration and direnv would otherwise print into every
    result) / `trap - DEBUG` (bash). `chpwd_functions` are left alone: a `cd`
    hook is user-intended output;
  - install the completion-marker hook:
    - zsh: `precmd() { local st=$?; print -r -- "\n__OCODE_DONE_<nonce>__ $st $PWD" }`
      (capture `$?` first — entering the function is itself a command);
    - bash: `PROMPT_COMMAND='printf "\n__OCODE_DONE_<nonce>__ %s %s\n" "$?" "$PWD"'`
  - then one synchronizing sentinel, drained before the session is usable.
- **`Run(ctx, command) -> (Result, cwd, error)`**:
  1. write the command **wrapped as a single shell unit** to the master:
     `eval "$(cat <<'__OCODE_CMD_<nonce>__'` + newline + `command` + newline +
     `__OCODE_CMD_<nonce>__` + newline + `)"` + newline. The composer allows
     Shift+Enter, so `command` may hold several complete lines; unwrapped, each
     line would fire `precmd` and emit its own marker, and `Run` would return
     at the first one while the rest leaked into the next `Run`. The heredoc
     makes the shell consume the whole text as one command, so exactly one
     marker follows, and `$?` is `eval`'s status, i.e. the last line's. The
     quoted delimiter means no expansion happens in the heredoc itself; `eval`
     then parses the text exactly as typed. A command whose text contains the
     delimiter line is rejected with an error before anything is written;
  2. read until the marker line (or ctx deadline);
  3. parse exit status and cwd — the marker line is `<marker> <status> <rest>`
     and **everything after the status is the cwd**, so paths with spaces
     survive; strip the marker, residual ANSI, and one leading/trailing blank
     line;
  4. return combined output + exit code + cwd.
- Emitting the marker from the shell's own prompt hook (rather than appending an
  epilogue to the command) is what reports the shell's real `$?` and keeps the
  framing independent of the command's syntax.

### Why the marker must come from the prompt hook, and why the heredoc wrap

Appending `; printf …` to the user's command breaks on trailing `&&`, an open
quote, a heredoc, or a line continuation. The prompt hook fires whenever the
shell finishes a command and is ready for the next, so framing is independent of
the command's syntax.

The hook alone does not make incomplete input safe: a trailing `&&` or an open
quote leaves the shell waiting for a continuation line, no marker arrives, and
the command only ends when the 600s timeout fires. The heredoc/`eval` wrap
turns that into an immediate `eval` syntax error with a non-zero `$?` and a
prompt (marker) right after, and it is also what collapses a multi-line
command into one marker (see `Run` step 1).

### Pty-cleanliness matrix (the cost of Approach B)

| Hazard | Mitigation |
|---|---|
| Input echo / ZLE redraw | `-o nozle` + `stty -echo` + discard the echoed command line |
| Prompt noise | `PROMPT`/`PROMPT2`/`RPROMPT`/`PROMPT_EOL_MARK` emptied |
| History pollution / lock | `HISTFILE=` in env before start, re-cleared in the prelude |
| CRLF | `stty -onlcr` + normalize `\r\n`→`\n` (and lone `\r`) |
| ANSI in the transcript | `TERM=dumb` + `NO_COLOR=1`, plus an ANSI stripper as a safety net |
| Pagers/editors (`git log`, `git commit`) | `PAGER`/`GIT_PAGER=cat`, `GIT_TERMINAL_PROMPT=0`; anything else hits the timeout |
| Commands that read stdin (`cat`, `sudo`) | no stdin is available, so they block — the timeout fires |

`!` output is inserted into the chat as plain text, which is why color is
deliberately off; the terminal panel remains the place for a full-color session.

### Timeout and recovery

- Per-command timeout defaults to `shell.DefaultTimeout` (600s). On expiry, write
  `\x03` (SIGINT to the foreground process group) and wait a short grace window
  for the next marker.
- If the marker does not arrive, close the pty (SIGHUP), mark the session dead,
  and let the next `!` respawn it. The timed-out command returns a clear error
  ("command timed out; session shell restarted").
- A shell that exits on its own (`exit`, killed) is detected on the next `Run`
  and respawned.

### API and frontend

- `POST /api/shell` body gains `session` (opaque key) and an optional
  `reset: true`, which closes any existing shell for that key before running
  the command; response gains `cwd` on **every** path: the persistent shell's
  `$PWD`, the `workDir` the one-shot fallback ran in, and the remote path for
  `host` requests — so the client never has to default a missing field.
- `api.shellCommand(command, workDir, host, session)`; `useChat.executeShell`
  passes the tab id. `reset` is a server-side flag only in v1: nothing in the
  web client sends it, so the client signature does not carry it.
- The `!` result message renders the returned cwd (e.g. a `# cwd: /tmp` line)
  so a leaked `cd` is visible rather than silent.

### Security

- Trust model unchanged: `!` is user-typed and auth-gated only; no agent
  permission involvement. The persistent shell is the same trust level as the
  terminal panel, which already sources the user's rc.
- rc aliases, functions and `PATH` apply to `!` commands — that is the point.
- The agent bash tool is untouched, so the `cc`-shadowing hazard (the user's
  `cc` is a Claude launcher, shadowing the C compiler) cannot reach agent
  commands.
- Known exposure: sourcing the rc brings the rc's exported credentials into the
  `!` shell's environment, exactly as in the user's terminal. No new secret
  surface beyond that, and none written to disk.

## Testing

- **State persists**: `!export FOO=1` then `!echo $FOO`; `!cd /tmp` then `!pwd`.
- **Env/functions present**: `!whence -w cc` reports `function`;
  `!command -v claude` resolves.
- **Result shape**: exit code and cwd returned for success and failure.
- **Clean output**: no prompt text in output; `~/.zsh_history` untouched
  (mtime/size assertion); no ANSI escapes.
- **Timeout**: a blocking command with a short test timeout → error, shell
  respawned, next command works.
- **Serialization**: two concurrent requests on one key run sequentially and
  both return correct results.
- **Multi-line**: a two-line command returns both lines' output in one result
  and the next `Run` is clean; an open-quote command returns a non-zero exit
  promptly (no timeout).
- **rc hooks**: a test rc that registers a `precmd`/`preexec` hook via
  `add-zsh-hook` produces no hook output in the result.
- **Project switch**: a request with a different `workDir` runs there and
  reports it as `cwd`.
- **Rekey**: after `/reset-id`, a `!` under the new id sees the old shell's
  state.
- **Lifecycle**: `POST /api/sessions/{id}/close` closes the shell; idle timeout
  reaps it; shutdown closes all.
- **Fallback**: pty creation failure falls back to one-shot `shellpkg.Run`.
- **Unchanged**: remote `!` one-shot; Windows one-shot.
- Mutation-verify the framing (break marker parsing → tests fail), the timeout
  path, and history suppression.

## Fallback

If pty session creation fails (no pty available, exotic shell), fall back to
today's `shellpkg.Run` one-shot so `!` never regresses.

## Resolved decisions

- **Wedged shell.** Automatic: on timeout write `\x03` (SIGINT to the foreground
  process group); if no marker arrives within the grace window, SIGHUP and
  respawn. Explicit escape hatch: `POST /api/shell` accepts `reset: true`,
  which drops any existing shell for the key before running the command. v1
  ships the flag (usable from the API / a future `/shell-reset` affordance); no
  new slash command.
- **Project switch.** The shell records the `workDir` it was spawned with. A
  request whose `workDir` differs `cd`s there first and records the new value,
  so `cd` persists within a project but switching projects rebases instead of
  running in the previous project's directory.
- **bash support level.** zsh is the primary, fully-supported target: `-o nozle`
  plus `stty -echo` gives clean framing. bash runs `-il` with the same prelude
  minus the zsh-only parts; without a `nozle` equivalent, readline may echo the
  command line, so the echoed command is stripped defensively and minor
  artifacts are accepted. bash is best-effort, not a shipping gate.
