---
name: ocode-remote-ssh
description: How ocode Remote (SSH/WSL) is wired — connection identity, the ssh argv and ControlPath rules, the exec-slot pool, the path-spelling mismatch class, host-side provisioning, and where the remote agent actually runs. Use this when working on internal/remote, remote projects, remote git/terminal/files behaviour, remote SSH 502s, or anything under internal/server/remote_*.go.
when_to_use: When the user asks about remote projects, SSH/WSL hosts, remote terminals, remote git or file operations, remote chat, 502s or hangs on a remote host, the host registry, SSH ports, or anything under internal/remote or internal/server/remote_hosts.go + handler_remote_*.go. Also triggered by: "remote ssh", "remote project", "connection refused remote", "wrong port remote", "remote 502", "remote agent runs where".
---

# ocode Remote SSH Field Guide

A dense map of the remote subsystem. **This skill is an index and an invariant
list — the `docs/` pages and `AGENTS.md` own the detail.** Never copy their
prose; link it.

## 0. File map

| Path | Role |
|---|---|
| `internal/remote/target.go` | `Target` (Kind/User/Host/Port/Distro, `Raw`), `ParseTarget`, `Validate`, `SSHArgs`, `String` |
| `internal/remote/execcmd.go` | `ExecCommand` (non-interactive ssh builder), `ShellQuote`/`ShellQuotePath`, `sshControlSocket` |
| `internal/remote/ssh.go` | `SSHTransport` (Exec/ExecStdin/scp upload), `commandArgs` |
| `internal/remote/shell.go` | Interactive `ShellCommand` + the ssh option blocks (`sshFailFastArgs`, `sshKeepaliveArgs`) |
| `internal/remote/connect.go`, `serve.go`, `provision.go` | Connect stages, detached server launch + tunnel, binary provisioning |
| `internal/remote/portmap.go`, `workspace.go`, `sync.go` | `-L` forwards, remote workspace, credential sync payload |
| `internal/server/remote_hosts.go` | The per-connection **registry** (workspaces + `registeredPaths`) |
| `internal/server/handler_remote_work.go` | `remoteWorkFor` admission, `remoteGitCommand`, the **exec-slot pool** |
| `internal/server/handler_remote_proxy.go` | `/api/remote/{host}/api/*` reverse proxy, profile stamping, project registration |
| `internal/server/handler_remote_lifecycle.go` | status / connect / restart endpoints |
| `internal/server/handler_remote_git*.go`, `handler_remote_fs.go`, `handler_remote_files.go` | Remote git + file endpoints |

Also: `web/src/hooks/useSessionHost.ts` (`resolveSessionHost`) and
`web/src/api/client.ts` (`remoteApiBase`) on the client side.

## 1. Connection identity is user + host + port — never `Target.String()`

**The rule.** `remote.Target.String()` returns `[user@]host` (or `wsl:<distro>`)
and **deliberately omits the port**. That is correct for the *project identity*
the store persists (`Project.Host` + a separate `RemotePort` field). It is the
**wrong key for anything that caches a live connection.**

Two projects on one host at different ports are two connections, two tunnels,
two bearer tokens, two sshd `MaxSessions` budgets, two remote registrations.
Keying any of them by `String()` silently makes one serve the other.

The connection key appears in **three** places, in two packages:

| Site | Key | Failure if it regresses |
|---|---|---|
| `remoteConnectionKey(host, port)` — `internal/server/remote_hosts.go` | user+host+port | registry returns the wrong port's workspace (wrong token, wrong server) |
| `sshControlSocket(t)` — `internal/remote/execcmd.go` | user+host+port | two ports share one `ControlPath`; ssh reuses a master **without verifying the destination**, so port 2222's commands run over port 22's connection |
| `remoteSlotKey(t)` — `internal/server/handler_remote_work.go` | delegates to `remoteConnectionKey` | unrelated projects throttle each other's slots |

`remoteSlotKey` **delegates** to `remoteConnectionKey` so the two cannot drift.
`internal/remote` cannot import `internal/server`, so it exposes
`SSHControlSocketIdentity`, and the agreement is pinned by
`TestRemoteConnectionKeyAgreesWithMuxIdentity` in `internal/server/remote_hosts_test.go`.

**An unspecified port is deliberately NOT the same key as an explicit `:22`.**
With no `-p`, ssh resolves the port from `~/.ssh/config`, which may remap the
host's default away from 22. Folding "unspecified" into "22" would let a
config-dialed connection share a master with an explicit `-p 22` command. More
keys is the safe direction: splitting duplicates a master; merging runs commands
on the wrong connection.

Anything that asks *"have I already connected to this target?"* wants the
connection key. Anything that asks *"is this the same project?"* wants
`String()`.

## 2. ssh argv invariants

- **`--` goes after `-p` and immediately before the destination.** ssh reads the
  first non-option element as the hostname, so the reverse order makes it read
  `-p` as the host. This lives inside `Target.SSHArgs()` so the ordering
  invariant exists in one place, not per caller.
- **A target beginning with `-` is rejected in BOTH `ParseTarget` and
  `Validate`** — on the host *after* the `user@` split, and on the user. A
  leading-dash token is parsed by ssh as an **OPTION**, which is a
  local-command-execution primitive: verified against OpenSSH, an
  `-oProxyCommand=…` host runs its command through a shell. Checking only the
  whole string would miss `user@-oProxyCommand=…`, whose token starts with `-`
  only after the user is prepended. `Validate` repeats the check because callers
  that build a `Target` field-by-field from a request body never reach
  `ParseTarget`.
- **`BatchMode=yes` for every non-interactive path** (`sshFailFastArgs`), so a
  cold connect fails fast instead of blocking on an invisible password prompt.
  Interactive paths (`ShellCommand`, `ExecInteractive`) keep `-t` **without**
  `BatchMode` so a real terminal can still prompt.
- `scp` uses `-P` for the port where ssh uses `-p` (`scpFailFastArgs`).

## 3. The exec-slot pool

- `remoteExecSlotsPerHost = 8` per **connection**: ssh execs share one
  ControlMaster connection and sshd's `MaxSessions` (default 10) refuses new
  sessions past it; 8 leaves headroom.
- `remoteExecLongRunningSlotsPerHost = 6` is a **sub-cap**, not a second pool:
  long-running work (remote shell commands, git network ops) is capped at 6 so
  short-lived **foreground** work (git status, diff, file reads, shell probe)
  always has room. Total concurrency still cannot exceed 8.
- A long-running command is **refused at the sub-cap, not queued** — queueing
  would let N long commands reclaim the whole pool as foreground work drains.
  The refusal is a normal outcome and its message reaches the client in the
  `error` field.
- Rationale for the reserve: the `git_status` emitter polls every 10s per viewed
  project, so a pool held entirely by long commands stales the whole Git panel
  for that host while each starved poll burns its own 30s bound.

## 4. Path spelling: the tilde / expanded mismatch class

The **most recurring** remote bug family. Two spellings of one project circulate
and silently disagree:

- `projects.Add` **expands** `~` → `/home/user/www/x`
- `projects.AddRemote` **keeps `~`** verbatim → `~/www/x` (the separator and `~`
  belong to the remote shell)

So the host's registry holds the expanded path while proxied requests arrive with
the tilde form. The fingerprint: **two responses naming the same project
differently in one round trip** (e.g. `/api/projects` reports
`/home/.../x` while `/api/projects/sessions?path=~/www/x` 404s).

The same root cause has surfaced as **three different HTTP failures on three
endpoints**, each fixed in its own place:

| Class | Endpoint | Resolution |
|---|---|---|
| 403 | terminal history / WS | host-side `projects.ExpandHome` in the `host == ""` branch |
| 400 "unknown project" | git / fs / uploads | `resolveRegisteredProjectRoot` (verbatim first, `ExpandHome` fallback) |
| 404 "project not found in saved list" | `/api/projects/sessions` | same helper, adopted by `HandleListProjectSessions` |

Rules:

- A handler that **lists by path** but never compares against a registry can
  answer 200 for the project and 404 for its sessions in the same instant.
- The **`?host=` branch must never expand** — a remote entry's path belongs to
  another machine.
- The fallback **narrows rather than widens**: unsaved paths and `~user` forms
  still 404. Resolution only ever returns a *saved* project root.
- **These fixes live on the HOST.** The failing comparison runs inside the
  host's `ocode serve --remote`, so the fix takes effect only after a version
  bump provisions the new binary (`EnsureBinary`). A newer local server cannot
  compensate.

## 5. Where the remote agent actually runs

**Remote-project chat runs on the HOST's `ocode serve --remote`**, not locally.
`useChat.ts` passes `projectHost` to `api.chat`/`api.sendMessage`, and
`fetchJSON` prefixes `/api/remote/{host}`. Only the proxy/tunnel is local.

- **Stale-comment trap:** comments on `projectHostFor`/`SetProjectHost` in
  `agent_session.go` once claimed remote chat runs on the **local** server. That
  is wrong. Ground truth is the client passing `projectHost` plus the proxy
  prefix. Do not trust old comments asserting local execution.
- **Profiles are stamped by the proxy, not synced.** `BuildSyncPayload` pushes
  `auth.profiles.json` + `opencode.json` + `ocodeconfig.json` but **not**
  `window-state.json`, and the remote launches without `OCODE_PROFILE`. So
  `HandleRemoteProxy` reads `X-Window-Id` → `injectProxiedActiveProfile` strips
  any client-supplied profile headers (a bare header is ignored — **no forge**)
  then stamps `X-Ocode-Active-Profile` + `X-Ocode-Profile-Authoritative: 1`, or
  `X-Ocode-Profile-Reset: 1` for Default. The remote's `applyProxiedActiveProfile`
  consumes it from `HandleChat`/`HandleSendMessage`. Nothing is persisted on the
  remote.
- `resolveSessionProfile` precedence: `OCODE_PROFILE` > window profile > global
  fallback. If a remote turn uses base keys, check the sync **and** whether the
  window→profile mapping exists on the host.

## 6. Remote command construction is an injection surface

`remoteGitCommand(dir, args...)` builds a **shell command line**
(`cd "<dir>" && GIT_OPTIONAL_LOCKS=0 git <args…>`). It does **not** quote its
arguments.

- **ANY free-form argument must be `remote.ShellQuote`d by the caller** — commit
  message, stash message, ref, branch name. Unquoted, a message with `;` or
  `$(...)` executes on the host.
- **Pathspecs from `remoteSafeSpec` must NOT be pre-quoted** — they are already
  metachar-free, and double-quoting breaks rev resolution.
- Local mutations are unaffected: `gitRunInDir` passes argv straight to
  `os/exec`, so quoting locally would embed literal quotes into the argument.
- **Detach long-lived remote processes in a subshell.** `mkdir && nohup … &`
  backgrounds the *whole* compound command, so the forked subshell waits on the
  server and the ssh channel never closes (a ~10-minute connect-backstop 502).
  The working shape is `mkdir DIR && (nohup BIN … </dev/null >LOG 2>&1 &); echo launched`.

## 7. Provisioning and versioning

- `EnsureBinary` = upload → `ActivateAndVerify` (chmod + mv + `--version`).
  Uploading without activating leaves `~/.ocode/bin/<ver>/.ocode.partial` and the
  next stage exits 127 → 502 at stage `remote-connect`.
- A fresh-server launch must return immediately; `StartFreshServer` blocks on
  the ssh exec otherwise.
- **Host-side behaviour only changes on a version bump.** Any fix whose failing
  code runs on the host (path resolution, terminal admission) needs the new
  binary provisioned before it takes effect. When debugging, check
  `~/.ocode/remote/serve.json` and `~/.ocode/bin/<ver>/` on the host.

## 8. Doc index — read these, do not restate them

| Page | Owns |
|---|---|
| `gotchas/remote-connection-identity-includes-port.md` | The full §1 story + the 6-of-8 reserve |
| `gotchas/remote-project-path-trust-boundary.md` | `(host, path)` identity, why remote paths are never local roots, the leading-`-` host rule |
| `gotchas/remote-session-list-tilde-404.md` | The 404 class, the fingerprint, host-side-only fix, spinner ownership |
| `gotchas/remote-session-config-host-routing.md` | Session-scoped config helpers must pass `host` ("toggle does nothing") |
| `gotchas/remote-terminal-502-provisioning.md` | The three independent 502 causes (activation, launch detachment, `~` expansion) |
| `gotchas/remote-terminal-custom-port-omitted.md` | **Frontend**: `TerminalPanel` omits `remotePort` from the live WS URL |
| `gotchas/remote-ssh-chat-profile-not-applied.md` | Profile stamping via proxy headers |
| `gotchas/remote-git-shell-quoting.md` | `ShellQuote` for remote git args + the pathspec exception |
| `gotchas/tui-clipboard-remote-ssh-osc52.md` | TUI clipboard over a remote ssh pty |
| `concepts/remote-mcp-oauth-compat.md` | MCP OAuth over a remote host (why the callback must run host-side) |
| `concepts/web-session-host-scoping.md` | The `?host=` threading rule, client + server |
| `AGENTS.md` §"Handler.mu is a map lock" | Locking rules for everything in `internal/server` |
| `AGENTS.md` §"gitexec, never a bare exec.Command" | Bounded subprocesses; async `/api/chat`; emitter fan-out |

## 9. Traps

- **Do not "tidy" the unspecified-port key** to equal 22 — see §1.
- **Do not compare remote project paths by string equality** without
  `resolveRegisteredProjectRoot`, and never expand in the `?host=` branch.
- **`Target.String()` as a cache key is the signature of this bug class.** Grep
  for it before adding any new map keyed on a target.
- Remote-project records are **persisted**, so an invalid target is a stored
  payload that re-fires on every later connect — validate at both
  `ParseTarget` and `Validate`.
