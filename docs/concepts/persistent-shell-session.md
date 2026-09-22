---
type: Concept
title: Persistent per-session shell for exclamation-mark commands
description: How the web/desktop composer's exclamation-mark commands run in a persistent pty-backed interactive shell per chat tab, with prompt-hook marker framing, pty cleanliness, lifecycle, and the one-shot fallback.
tags:
  - shell
  - persistent-shell
  - pty
  - exclamation-commands
  - web
  - server
timestamp: 2026-09-21T08:40:00Z
---
# Why

`!command` in the web/desktop composer sent `POST /api/shell`, which ran
`<resolved shell> -l -c command` in a fresh process (`internal/shell.Run` →
`shell.Build`). A *login* shell sources `/etc/zprofile` and `~/.zprofile` but
never `~/.zshrc`, so the interactive environment was absent:

- `~/.local/bin` (added by `~/.local/bin/env`), nvm/pyenv/mise, and every shell
  function/alias such as `cc` (which resolved to the C compiler, not the user's
  launcher).
- The rc load was paid on **every** command (~0.4–0.5s warm, ~3.1s cold).

Spec: `superpowers/specs/2026-09-21-persistent-shell-session-design.md`.
Plan: `superpowers/plans/2026-09-21-persistent-shell-session.md`.

# Design

One persistent, pty-backed interactive shell per chat tab, created lazily on the
first `!`.

## Packages

- `internal/shell/session.go` — platform-neutral `SessionOptions{Shell, Dir,
  Env, Timeout, Grace, Cols, Rows}`, the `ErrUnsupported`/`ErrClosed` sentinels,
  and the pure framing/parsing helpers (`frameCommand`, `findMarker`,
  `cleanOutput`, `stripANSI`, `normalizeNewlines`, `Quote`).
- `internal/shell/session_unix.go` (`//go:build !windows`) — `Session` on
  `github.com/creack/pty`: `NewSession`, `Run(ctx, command) (Result, cwd,
  error)`, `Close`, `Alive`.
- `internal/shell/session_other.go` (`//go:build windows`) — a stub whose
  `NewSession` returns `ErrUnsupported`, so callers stay platform-neutral and
  Windows keeps the one-shot path.

## Spawn

`pty.Start(exec.Command(shell, "-il"[, "-o", "nozle" if zsh]))`, `cmd.Dir =
workDir`, wide winsize (400×50 default). Environment is the process env plus:

- `HISTFILE=` (empty, so zsh never opens/locks/writes history),
- `TERM=dumb`, `NO_COLOR=1`, `PAGER=cat`, `GIT_PAGER=cat`,
  `GIT_TERMINAL_PROMPT=0`.

`SessionOptions.Env` is appended **last**, so a caller (or test) can override
`HOME`/`ZDOTDIR` — Go's `exec.Cmd` keeps the last duplicate key.

## Prelude (sent once, drained)

- `stty -echo -onlcr` — kill tty echo and CRLF translation.
- Blank the prompts (`PROMPT`/`PROMPT2`/`RPROMPT`/`PROMPT_EOL_MARK` for zsh;
  `PS1`/`PS2` for bash).
- `HISTFILE=; unset HISTFILE` and history options off.
- `unsetopt zle` (redundant with `-o nozle`).
- Clear rc-installed hooks: `precmd_functions=() preexec_functions=();
  unset -f preexec` (zsh) / `trap - DEBUG` (bash). Defining `precmd()` alone
  does **not** remove `add-zsh-hook` registrations, so starship / powerlevel10k
  / direnv / iTerm2 integration would otherwise print into every result.
- Install the marker hook (below).

A `:` sentinel run through the same framing then synchronises startup: it drains
the rc/prelude output and proves the hook works before the session is handed
out.

## Marker framing

The boundary marker is printed by the shell's **own prompt hook**, not appended
to the user's command — so it is correct for a trailing `&&`, an open quote, a
heredoc or a line continuation, and it reports the shell's real `$?`:

- zsh: `precmd() { local st=$?; printf '\n<marker> %s %s\n' "$st" "$PWD" }`
  (`$?` is captured first: entering the function is itself a command).
- bash: `PROMPT_COMMAND='printf "\n<marker> %s %s\n" "$?" "$PWD"'`.

The marker line is `<marker> <status> <rest>`, and **everything after the status
field is the cwd** — so a path containing spaces survives intact.

Note: the design spec's `print -r -- "\n…"` example would emit a literal `\n`
(`-r` disables escape interpretation); the implementation uses `printf`.

## Command framing

Each command is wrapped as a single shell unit:

```
eval "$(cat <<'<delim>'
<command>
<delim>
)"
```

The quoted heredoc delimiter passes the text through verbatim, and `eval` then
parses it exactly as typed — including several complete lines (the composer
allows Shift+Enter). That collapses a multi-line command into **one** prompt-hook
firing, so exactly one marker follows and `Run` returns the last line's exit
status; an unwrapped command would fire the hook once per line and leak the tail
into the next `Run`. An unterminated quote becomes an immediate `eval` syntax
error (non-zero, no hang). A command that already contains the delimiter as a
whole line is rejected **before anything is written**.

## Run

- Serialised on one mutex: one command at a time per session.
- Output is normalised (CRLF/CR → LF), ANSI-stripped, the echoed command
  stripped, and boundary blank lines trimmed.

# Timeout and recovery

Per-command timeout defaults to `shell.DefaultTimeout` (600s). On expiry:

1. Write `\x03` (SIGINT to the foreground process group).
2. Wait a grace window (default 2s) for the marker.
   - Marker arrives → the session is kept alive ("interrupted; session shell
     preserved").
   - No marker → close the pty, SIGHUP then SIGKILL the process group, mark the
     session dead; the next `Run` respawns it ("session shell restarted").

A shell that exits on its own is detected on the next `Run` and respawned.

# Server registry and lifecycle

`internal/server/shell_sessions.go` — `shellSessionRegistry`:

- Lazy `map[tabId]*shell.Session`, behind the `shellSession` interface so tests
  can inject a pty-free fake.
- Per-key mutex; injected factory, idle timeout and clock.
- `run`, `close`, `rekey`, `closeAll`, `reap`, `startReaper`.
- **Project switch**: a request whose `workDir` differs from the directory the
  shell was spawned in gets a `cd <Quoted workDir>` rebase first, so switching
  projects rebases instead of running in the previous project's directory.

`POST /api/shell`:

- Request body gains `session` (opaque tab id) and a server-only `reset: true`.
- Response gains `cwd` on **every** path — the persistent shell's `$PWD`, the
  `workDir` the one-shot fallback ran in, and the remote path for `host`
  requests — so the client never defaults a missing field.
- An empty `session` keeps the historical one-shot path (client back-compat).

Lifecycle:

- Tab close: `HandleCloseSession` closes the shell for the id **first and
  unconditionally**, before the `sessions.Resolve` 404 and before the
  close-pending branch — the shell has no in-flight-turn dependency.
- `/reset-id`: `HandleResetSessionID` calls `registry.rekey(old, new)` after
  `session.RekeyForDir` succeeds, so `!cd`/`!export` survive the rekey.
- Shutdown: `Handler.Shutdown` calls `closeAll`.
- Idle reaper: a ticker at `defaultSessionIdleTimeout/4` with a 30-minute
  deadline drops unused shells.

# Fallback

Any failure to provide a persistent shell — pty unavailable, Windows
(`ErrUnsupported`), a racing close — is logged and degrades to the one-shot
`shellpkg.Run`. `!` never regresses, and a `cwd` is still reported.

# Web

- `api.shellCommand(command, workDir, host, session)` with `cwd` in the response
  type.
- `useChat.executeShell` forwards the tab id as `session`; when the request never
  reaches the server it reports `cwd = projectPath`.
- `ChatInput` renders `# cwd: <path>` **inside** the fenced code block, because
  the message is rendered as markdown and a bare `# cwd:` line would become a
  heading.

# Accepted limitations

- Commands that read stdin (`cat`, `sudo`) or open a pager/editor block until the
  600s timeout; pagers are pinned to `cat` via `PAGER`/`GIT_PAGER`.
- `TERM=dumb` + `NO_COLOR=1` mean no colour in `!` output; the terminal panel
  remains the full-colour session.
- A shell created by a `!` in a `new-*` draft tab **before the first message**
  stays keyed under `new-<ts>` and is not reused after the client-side rename to
  the real session id (the server never sees the draft id). It is reaped by the
  idle timeout — one extra rc load is the accepted v1 cost.
- Remote (`host`) `!` commands stay one-shot over ssh.
- The agent bash tool (`internal/tool`) is deliberately untouched: it needs
  per-command `cmd.Dir`, per-command sandbox wrap, per-command process-group
  cancel and foreground→background promotion, all of which a shared shell would
  break.
- Windows has no pty here → one-shot `cmd /C`.

# Testing model

- `internal/shell/session_test.go` — pure framing/parsing tests; no pty, runs
  everywhere.
- `internal/shell/session_fake_unix_test.go` — drives `Run` against an `os.Pipe`
  fake shell, so the run state machine (framing on the wire, marker waiting,
  timeout recovery, close, serialisation, dead-shell detection) is exercised even
  on a host that denies `/dev/ptmx`.
- `internal/shell/session_unix_test.go` — real-pty integration tests. Each starts
  with `requirePTY(t)`, which **skips** when `pty.Open()` fails — notably when the
  process is confined by ocode's own Seatbelt sandbox (`/dev/ptmx` → EPERM),
  mirroring `internal/server/handler_terminal_test.go`.
- `internal/server/handler_shell_session_test.go` — registry/handler contracts
  against a pty-free fake `shellSession`.

Mutation-verified behaviours: marker parse, delimiter rejection, timeout
recovery, `Close`, run serialisation, dead-shell detection, close/reaper,
ignoring the session key, dropping `cwd`, dropping the `/reset-id` rekey,
dropping the project rebase, and the frontend session argument + cwd line.

## Related

- [Persistent per-session shell for exclamation-mark commands — Design](superpowers/specs/2026-09-21-persistent-shell-session-design.md)
- [Persistent per-session shell for exclamation-mark commands — Plan](superpowers/plans/2026-09-21-persistent-shell-session.md)
