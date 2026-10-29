---
name: ocode-remote-ssh
description: How ocode's remote SSH projects are wired — connection identity, ssh argv construction, the per-connection exec slot pool, the project trust boundary, provisioning/launch, and profile routing. Use this whenever working on internal/remote, the remote host registry, remote git, remote terminals, or any /api/remote/{host}/ route.
when_to_use: When the user asks about remote projects, SSH hosts, the remote registry, remote git/terminals, connection hangs, 502s from a remote route, port or profile mismatches on a remote project, or anything under internal/remote or internal/server/remote_*.go.
---

# ocode Remote SSH Field Guide

A short, dense map of the remote-SSH subsystem so you don't re-discover it from
scratch. Every invariant below was read out of the current tree; every line
anchor here was verified. The bundle pages listed in §9 own the narratives —
this file is the index plus the rules the pages only imply.

## 1. File map

| Path | Role |
|---|---|
| `internal/remote/target.go` | `Target` (user/host/port/distro), `Validate`, `SSHArgs`, `ParseTarget`, `String` |
| `internal/remote/ssh.go` | `SSHTransport`, `commandArgs` (the single argv builder), `Exec`/`ExecStdin` |
| `internal/remote/shell.go` | `sshFailFastArgs`, keepalives, WSL/`wsl.exe` shell selection |
| `internal/remote/execcmd.go` | Chooses ssh vs `wsl.exe` per target kind |
| `internal/remote/wsl.go` | `wslExecArgs` / `wslInteractiveArgs`; WSL runs via `exec.Command("wsl.exe", …)` |
| `internal/remote/connect.go`, `serve.go`, `workspace.go`, `provision.go` | Connect flow, server launch, binary upload/activate |
| `internal/remote/sync.go` | `BuildSyncPayload` — credentials/config pushed to the host |
| `internal/server/remote_hosts.go` | Connection registry: `remoteConnectionKey`, per-entry connect `sync.Cond` |
| `internal/server/handler_remote_work.go` | `remoteGitCommand`, `remoteSafeSpec`, the exec slot pool |
| `internal/server/handler_remote_proxy.go` | `HandleRemoteProxy` — the `/api/remote/{host}/…` entry |

## 2. The one invariant that matters most: connection identity

**Identity is `user + host + port`. Never `Target.String()`** — `String()` is
port-free, so keying anything on it silently merges two connections to the same
host on different ports.

| Site | Helper | Anchor |
|---|---|---|
| Registry / per-entry connect state | `remoteConnectionKey(host, port)` | `internal/server/remote_hosts.go:64` |
| Exec slot pool | `remoteSlotKey(t)` → `remoteConnectionKey(t.String(), t.Port)` | `internal/server/handler_remote_work.go:405` |
| Cross-package pin | `TestRemoteConnectionKeyAgreesWithMuxIdentity` | `internal/server/remote_hosts_test.go:1037` |

- `port <= 0` means "unspecified" and keys as the **bare host**. An unspecified
  port is deliberately **not** equal to an explicit `22` — the safe direction is
  more keys, never fewer.
- WSL targets carry no port (`Validate` rejects one), so they also key as the
  bare host.
- **Two implementations exist on purpose:** `internal/server` imports
  `internal/remote`, never the reverse. Change **both** or neither; the test
  above is what stops them drifting.
- Anything caching a workspace, socket, mutex, or "same connection?" decision
  keys on this. Getting it wrong corrupts data *across* connections.

## 3. ssh argv: two independent barriers

All non-interactive ssh goes through one builder, `commandArgs`
(`internal/remote/ssh.go:114`):

```go
args := append(sshFailFastArgs(), s.Target.SSHArgs()...)
return append(args, command)
```

- `sshFailFastArgs()` (`internal/remote/shell.go:84`) = `-o BatchMode=yes` plus
  keepalives. `BatchMode` stops ssh blocking on an invisible password/passphrase
  prompt — without it a cold connect hangs the request goroutine forever,
  because ssh falls back to `/dev/tty` even with a closed stdin.
- `Target.SSHArgs()` (`internal/remote/target.go:83`) emits `-p <port>` **only
  when `Port > 0`**, then `--`, then `t.String()`. The `--` lands **after**
  every option, so a hostile host can never be read as an ssh flag.

Two independent barriers, and you need both:

| Barrier | Where | Rejects |
|---|---|---|
| Leading-`-` on host/user | `Target.Validate` (`target.go:64`) **and** `ParseTarget` (`target.go:139`) | option injection at parse **and** at validate time |
| `--` separator | `SSHArgs` (`target.go:83`) | anything that slips past the checks |

`ExecInteractive` / `ShellCommand` intentionally do **not** use the fail-fast
block — a real terminal must be able to prompt. Never add `BatchMode` there.

## 4. The exec slot pool: two-phase admission

Per connection, from `remoteExecSlots` (`handler_remote_work.go:351`):

| Constant | Value | Meaning |
|---|---|---|
| `remoteExecSlotsPerHost` | 8 | Total concurrency (also the `sshd MaxSessions` budget) |
| `remoteExecLongRunningSlotsPerHost` | 6 | Sub-cap for long-running work |

Admission is an **all-or-nothing pair**, and the order is the whole point
(`acquireRemoteExecSlot`, `handler_remote_work.go:417`):

1. **Long-running** takes the `long` token **first, non-blocking**. If the
   sub-cap is full it is **refused immediately** — not queued. Queueing would
   let N long commands reclaim the whole pool the instant foreground work
   drained. The refusal is a normal outcome, not a transport fault.
2. **Then both kinds block** for the whole-pool `all` token until one frees or
   `ctx` ends. On `ctx` end the long-running path **hands the `long` token
   back** before returning.
3. The release closure drains `all`, and for long-running also `long`.

So "refuse" and "queue" are both true, at different layers: the **sub-cap
refuses**, the **pool queues**. Reserve is a sub-cap on the *same* pool, never
a second pool. The reserve exists because the `git_status` emitter polls every
10s per viewed project — a pool held entirely by long commands makes the whole
Git panel go stale while every starved poll burns its own 30s bound.

## 5. Project trust boundary

**A remote project's path must never enter a path-only local allowlist.**

- `allowedProjectRoots` (`internal/server/handler.go:662`) contains only the
  workdir plus projects with `Host == ""`. Project identity is
  `(host, path)` (+ port).
- `resolveRegisteredProjectRoot` (`handler_git.go:136`) matches **verbatim
  first**, then falls back to `ExpandHome`. The `?host=` branch keeps the exact
  `(host, verbatim path)` match and **never expands** — a remote path is another
  machine's filesystem.
- Local and remote views of the same directory string stay distinct
  (`TestSharedPathLocalAndRemoteStayDistinct`).
- The `host` value is validated as ssh **destination syntax**; a leading `-`
  would be a persisted local-command-execution primitive.
- The remote token never reaches the browser; the proxy never consults
  `allowedProjectRoots`.
- Any new project-scoped endpoint must pick local-vs-remote mode **explicitly**.

Host-side `~` expansion belongs to the `host == ""` branch only
(`handler_terminal.go:226`, `:728`) — expanding on the host side is a
cross-machine path bug.

## 6. Git over a remote connection

Two complementary layers of the same function — don't merge them:

| Layer | Owner | Rule |
|---|---|---|
| Lock/env | **`CLAUDE.md` §"Git subprocesses: `gitexec`"** | leading `GIT_OPTIONAL_LOCKS=0` in `remoteGitCommand`; mutations via `remoteGitMutation` |
| Quoting | `docs/gotchas/remote-git-shell-quoting.md` | see below |

- `remoteGitCommand` (`handler_remote_work.go:527`) builds a **shell string**,
  so quoting is the caller's job.
- Any free-form argument (commit message, stash message, ref, branch) **must** be
  wrapped in `remote.ShellQuote` by the caller.
- A pathspec from `remoteSafeSpec` (`handler_remote_work.go:547`) **must not** be
  pre-quoted — double-quoting breaks `^{commit}` rev resolution.
- **Never** quote for the local `gitRunInDir` path: it's argv, no shell, so
  quoting embeds literal `"` characters.

## 7. Provisioning and launch (the three 502 causes)

Fixed in-tree; each has a regression test (page in §9):

1. **Upload must activate.** `ensureBinary` delegates to `EnsureBinary`
   (upload → chmod/mv → `--version`). An uploaded-but-inactive binary leaves
   `.ocode.partial` and the next stage exits 127.
2. **Launch must detach in a subshell**:
   `mkdir DIR && (nohup BIN … </dev/null >LOG 2>&1 &); echo launched`.
   `A && nohup … &` backgrounds the *whole list*, so the ssh channel stays open
   until the server dies and the connect times out.
3. **Registration failure must call `remoteHosts.drop(host)`** — otherwise a
   dead cached workspace makes every later request 502 until restart.

### A WEDGED connect needs an escape hatch (stall guard)

The connect runs in the background for up to `remoteConnectTimeout` (10 min) and
every read gets `503 stage=remote-connecting`, which the client retries forever
by design. Nothing used to escalate, so a connect that hung mid-provision left
the UI saying "connecting…" for ten minutes. The pieces:

- **`remoteHostStatus` carries `connecting` + `connect_started_at`** (unix ms).
  Without them the client cannot tell "3s in" from "wedged", because 503 is
  deliberately indistinguishable from progress.
- **`remoteHosts.dropForce(host, port)`** evicts a connection EVEN IF a connect
  is in flight. Plain `drop` refuses that on purpose (a superseded attempt can
  relaunch the remote server and orphan the first), so only an explicit user
  retry may force it. The abandoned `runConnect` keeps running detached; its
  completion path compares `reg.byConn[key]` with its own entry and disconnects
  any late workspace, so nothing leaks.
- **`POST /api/remote/{host}/connect?force=1`** clears the failed-entry cooldown,
  force-drops, and starts a fresh attempt. `restart` also force-drops: mid-connect
  it has no pid to kill, so before this a restart was a silent no-op that
  re-joined the same wedged attempt.
- **Client**: `useRemoteHostStatus` arms a 10s stall timer
  (`REMOTE_STALL_AFTER_MS`) while an attempt is in flight, offering **Reconnect**
  (forced) and, if a retry ALSO stalls or fails, **Restart host** (kills the
  host's turns and terminals). Every explicit action calls
  `resetRemoteHostGate(host)`, or the client's 2-minute read backoff would skip
  the very reads the retry needs.
- **The health probe must produce an ANSWER, not an error.** `probeServer`
  (`internal/remote/serve.go`) runs through `Transport.Exec`, which returns an
  error for EVERY non-zero *remote* exit — so curl's exit 7 (connection
  refused: the ordinary "live pid, nothing listening") used to be reported as a
  transport failure, making the actionable "running but not answering
  /api/health; restart it explicitly" unreachable. Classify with
  `transportFailed` (exit 255 or `<= 0`, i.e. no verdict) and treat any other
  non-200 as the wedged ANSWER. The command carries `-m 5 --connect-timeout 3`
  so a server that accepts TCP and never replies cannot hold the probe until the
  10-minute backstop, and prints `no-curl` when the host has no usable curl —
  that is UNMEASURABLE, not wedged, so the live server is reused; otherwise
  every reconnect on a curl-less host fails forever (pid alive ⇒ never
  relaunched, health never passes).
- Regression: `remote_hosts_stall_test.go` (`TestStatusReportsInFlightConnect`,
  `TestDropForceSupersedesInFlightConnect`, `TestRestartSupersedesInFlightConnect`,
  the `?force=1` lifecycle test), `useRemoteHostStatus.test.tsx`,
  `RemoteProjectStatus.test.tsx`; and `serve_test.go`'s
  `TestEnsureRemoteServerHealthProbeRemoteExitIsAnAnswer`,
  `TestEnsureRemoteServerWithoutCurlReusesLiveServer`,
  `TestEnsureRemoteServerSignaledPidProbeDoesNotLaunch`,
  `TestHealthProbeCmdBoundsTheProbeAndDetectsMissingCurl`.

`HandleRemoteProxy` resolves the saved project's `RemotePort` **server-side** and
connects via `workspaceForPort(host, projectPath, remotePort)`. The web client
sends **no** `host=`/`port=` query params for terminal history or the WS.

### Terminal TAB LIST is local; terminal SHELLS are on the host

These are two different things and conflating them caused a real bug:

- The **shell** for a remote project's terminal is a pty child of the HOST's
  `serve --remote`, reached through `/api/remote/{host}/api/terminal/*`. Shared
  across every client, deliberately (it survives a laptop sleep or a desktop
  restart).
- The open-terminal **tab list** is state on the LOCAL server
  (`GET/PUT /api/terminal-tabs` → `internal/termtabs`), keyed by the client's
  `<host>::<path>` composite. It is NOT proxied — it describes what THIS server's
  clients have open, so proxying it would be meaningless.

A remote project's tab list was `localStorage`-only until 2026-09-30, so a
terminal started in the desktop app showed an empty strip in a second browser
even though the shell was running on the host the whole time. Session tabs had
already been migrated server-side for the same reason; see `internal/tabs`' package
doc. `GET /api/terminal?project_path=…` (the host's live-shell inventory) backs the
sidebar reattach list and is a DIFFERENT question from the tab list — a shell that
outlived its tab appears in the inventory but not in the strip.

A terminal has ONE attachment slot even across clients: attaching the same
`terminal_id` from a second client sends `{"type":"detached","reason":"superseded"}`
on the first socket and closes it, and that client's panel parks with a **Take
over** button rather than reconnecting. See `skills/ocode-web` (terminalStore) and
`docs/concepts/web-server-project-scoping.md`.

## 8. Profile and credential routing

Remote chat runs **on the host** (`ocode serve --remote`) — never locally.

- `resolveSessionProfile` order: `OCODE_PROFILE` > window profile > global fallback.
- The active profile is authoritative **only** when the local proxy stamps
  `X-Ocode-Active-Profile` + `X-Ocode-Profile-Authoritative: 1`
  (`injectProxiedActiveProfile`, `handler_remote_proxy.go:187`).
  `applyProxiedActiveProfile` (`handler_profiles.go:218`) consumes it. A bare
  client-supplied profile header is **ignored** — no forge. Nothing is persisted
  on the remote.
- `BuildSyncPayload` (`sync.go:57`) ships `auth.profiles.json` + `opencode.json`
  + `ocodeconfig.json`, but **not** `window-state.json` — which is why the
  profile must travel per-request rather than as synced state.
- `BuildSyncPayload` also mirrors an ALLOWLIST under the local `$HOME` into the remote `$HOME`
  (`sync_home.go`, `SyncDirHome`): `~/.claude/{CLAUDE.md,settings.json}`,
  `~/.config/opencode/{AGENTS.md,CLAUDE.md}` and the `skills`/`agents` trees under `~/.claude`,
  `~/.config/opencode`, `~/.agents/skills`. `settings.json` keeps only `permissions`, `model`,
  `enabledPlugins` (no `env`/helpers/hooks). Symlinked files and dirs are followed and hashed by
  target content; links resolving outside `$HOME` or into `.ssh`/`.aws`/`.gnupg`/`.kube`/
  `.docker`/`.config/gcloud`/`.config/gh` are skipped. `.env*`, key files, `.git`, `node_modules`,
  files over 1 MiB are skipped; over 16 MiB total is an error. `WritePayload` re-checks the
  allowlist on the remote, so a crafted frame cannot write elsewhere. Local always wins, deletions
  included: the remote records what it wrote in `home-sync-manifest.json` (ocode data dir) and
  removes only manifest paths the next payload no longer carries (re-checked against the allowlist),
  pruning dirs left empty inside a tree. Hand-made remote files are never touched.
- Credential files ride the same sync: `auth.json` and `mcp-auth.json` (opencode data dir,
  `SyncDirOpencodeData`), `auth.profiles.json`, and `~/.config/opencode/antigravity-accounts.json`.
  Also allowlisted: `~/.config/opencode/commands/` and `auto-permission-prompt{,.local}.md`.
  Credential files are **gated per file** (`gateCredentialFiles`, `sync_creds.go`): pushed only when
  their own content hash differs from the last successful push to that host
  (`remote-sync-creds.json`), because OAuth refresh tokens rotate and a blind re-push would
  overwrite a token the remote just refreshed. `force` pushes all. A held-back home credential is
  listed in `SyncPayload.KeepHome` so the stale-file sweep does not delete it.
- Re-push is lazy: remote proxy traffic calls `maybeSyncRemoteHostsLazily` (at most every 10 s);
  `PayloadHash` gates the ssh round trip, so an unchanged payload costs only a local read.
- Remote and local projects must be prevented from sharing config: a
  `web/src/api/client.ts` helper with an optional trailing `host` **must** be
  given the session's remote host, or it silently writes the local server's
  config and the toggle appears to do nothing.

## 9. Bundle pages (link, don't duplicate)

| Page | Owns |
|---|---|
| `docs/gotchas/remote-connection-identity-includes-port.md` | the identity invariant + its history (**untracked**; see §10) |
| `docs/gotchas/remote-git-shell-quoting.md` | the quoting layer of `remoteGitCommand` |
| `docs/gotchas/remote-project-path-trust-boundary.md` | allowlist boundary, ssh-destination validation |
| `docs/gotchas/remote-session-list-tilde-404.md` | `resolveRegisteredProjectRoot` in list endpoints (**untracked**) |
| `docs/gotchas/remote-ssh-chat-profile-not-applied.md` | profile stamping, `BuildSyncPayload` gap |
| `docs/gotchas/remote-session-config-host-routing.md` | `host` threading in the web API client |
| `docs/gotchas/remote-terminal-502-provisioning.md` | the three 502 causes above |
| `docs/gotchas/tui-clipboard-remote-ssh-osc52.md` | `copyToClipboard` / OSC 52 |
| `docs/concepts/remote-mcp-oauth-compat.md` | MCP credential bound to exact server URL |
| `CLAUDE.md` § Git subprocesses | `gitexec`, `GIT_OPTIONAL_LOCKS=0`, `WithLockRetry` |

**TUI clipboard (page above):** never call `clipboard.WriteAll` from a TUI view —
route every copy through `copyToClipboard` (`internal/tui/clipboard.go:26`), which
emits OSC 52 via `tea.SetClipboard` and falls back to the local utility. Direct
`clipboard.WriteAll` survives in exactly one place, `clipboard.go:39`. On a
headless SSH host, direct calls silently do nothing. OSC 52 has no ack, so
fallback errors are logged, never surfaced to the user.

## 10. Known-bad page — do not trust

**`docs/gotchas/remote-terminal-custom-port-omitted.md` is stale.** It describes
`TerminalPanel` passing `remotePort`, which no longer exists in `web/src` (removed
2026-09-18); its suggested fix would now be wrong, and its own test asserts the
WS URL contains **no** `port=`. Port routing moved server-side (§7). Rewrite the
page around proxy-side resolution or deprecate it — do not follow its guidance.

Also: the identity page's frontmatter still says the registry is "still-unfixed"
while its body and the code both document it as fixed, and it cites a test
(`TestSSHControlSocketIdentityMatchesServerKey`) that does not exist — the real
one is `TestRemoteConnectionKeyAgreesWithMuxIdentity` (§2).

## 11. Quick greps

- "How is a connection keyed?" → `grep -n "remoteConnectionKey\|remoteSlotKey" internal/server/`
- "Where is the ssh argv built?" → `grep -n "commandArgs\|SSHArgs\|sshFailFastArgs" internal/remote/`
- "Where is the pool cap?" → `grep -n "remoteExecSlotsPerHost\|acquireRemoteExecSlot" internal/server/handler_remote_work.go`
- "What is in the local allowlist?" → `grep -n "allowedProjectRoots" internal/server/handler.go`
- "Where does the proxy stamp the profile?" → `grep -n "injectProxiedActiveProfile" internal/server/handler_remote_proxy.go`
- "Which routes are remote-proxied?" → `grep -n "api/remote" internal/server/server.go`
- "Is this a remote or local path?" → check whether `host` is empty **before** touching any path allowlist
