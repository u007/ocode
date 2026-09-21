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
  `new-*` draft id, or a temp tab id" (see its doc comment). It is stable across
  `/reset-id` rekeys, so the shell survives a rekey without migration.
  `executeShell` passes it as a new body field `session`.
- The server keeps `map[key]*shell.Session`, created lazily on the first `!` for
  that key.
- Closed on `POST /api/sessions/{id}/close` (`HandleCloseSession` in
  `internal/server/handler_close.go`) — already called by
  `api.closeSession(tabId)` on tab close. That handler also has a
  close-pending path when a turn is in flight, so shell teardown must run on
  every path that actually releases the session. Backstops: an idle timeout
  (30 min, matching `defaultSessionIdleTimeout`) and server shutdown.
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
  - install the completion-marker hook:
    - zsh: `precmd() { local st=$?; print -r -- "\n__OCODE_DONE_<nonce>__ $st $PWD" }`
      (capture `$?` first — entering the function is itself a command);
    - bash: `PROMPT_COMMAND='printf "\n__OCODE_DONE_<nonce>__ %s %s\n" "$?" "$PWD"'`
  - then one synchronizing sentinel, drained before the session is usable.
- **`Run(ctx, command) -> (Result, cwd, error)`**:
  1. write `command` + `\n` to the master;
  2. read until the marker line (or ctx deadline);
  3. parse exit status and cwd; strip the marker, residual ANSI, and one
     leading/trailing blank line;
  4. return combined output + exit code + cwd.
- Emitting the marker from the shell's own prompt hook (rather than appending an
  epilogue to the command) is what makes multi-line commands and continuations
  terminate correctly, and it reports the shell's real `$?`.

### Why the marker must come from the prompt hook

Appending `; printf …` to the user's command breaks on trailing `&&`, an open
quote, a heredoc, or a line continuation. The prompt hook fires whenever the
shell finishes a command and is ready for the next, so framing is independent of
the command's syntax.

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
  the command; response gains `cwd`.
- `api.shellCommand(command, workDir, host, session)`; `useChat.executeShell`
  passes the tab id.
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
