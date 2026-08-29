# ocode Remote (SSH + WSL) — Design Spec

**Date:** 2026-08-29
**Status:** Approved design, pre-implementation
**Owner:** james

## Problem

ocode operates only on the local filesystem. Users want to work on projects
that live on remote machines (dev servers, build boxes) and, on Windows,
inside WSL distros — the way VS Code Remote does.

## Decision: remote-server architecture

The **entire ocode backend runs on the remote host**; the local machine is
display only. This was chosen over a local-process-with-SSH/SFTP-tool-layer
design because codebase analysis showed:

- `internal/server` is already a complete IDE-grade API over the machine it
  runs on (chat + SSE, files, git, shell, pty terminal, changes, undo/redo).
- The web UI and the desktop shell are already pure thin clients over that
  API (desktop boots the same server on loopback).
- The alternative would require threading an FS/Runner abstraction through
  ~700 direct `os.*` / `exec.Command` call sites, three independent path
  resolvers, and process-global allowed-path state.

The TUI needs **no client/server rewrite**: a TUI is a terminal program, so
remote TUI = run the remote ocode TUI over `ssh -t`. Only the web UI uses
the client/server split, and it already speaks it.

## Scope

In scope (this spec, phased):

1. TUI passthrough over SSH with binary auto-install
2. Web mode: remote `ocode serve` + SSH tunnel + token auth
3. Credential/config sync to the remote
4. WSL backend (Windows → local WSL distro)

Out of scope: full TUI thin client (local TUI rendering a remote session),
desktop-shell remote UI, SSH-to-Windows-then-WSL chaining, settings sync
beyond auth profiles + core model config, session sync.

## Architecture

### Backends

A remote backend = a host where the ocode binary runs, reached by a
transport. One abstraction, two transports:

- **ssh** — any host the user's system `ssh` can reach. Host definitions
  come from `~/.ssh/config`; ocode keeps **no parallel host registry**.
- **wsl** — on Windows, a local distro launched via `wsl.exe -d <distro>`.
  Addressed as `wsl:<distro>`. No SSH involved.

New package `internal/remote` owns: backend definition/parsing, platform
detection, provisioning, launch (TUI passthrough and serve), tunneling, and
config-sync framing.

### Transport: system ssh, not x/crypto/ssh

Shell out to the system `ssh`/`scp` binaries (as VS Code Remote-SSH does).
Rationale: inherits ssh_config, agent auth, hardware keys, ProxyJump,
ControlMaster, and known_hosts verification for free; reimplementing that
in-process with `x/crypto/ssh` is pure liability. All spawns route through
the existing process supervisor registry (`internal/tool/process_supervisor.go`
lifecycle registration) — no new raw uncoordinated spawn sites.

### CLI surface

- `ocode remote <host> [path]` — TUI passthrough. Ensure remote binary,
  then exec `ssh -t <host> <remote-ocode> [path]`
  (WSL: `wsl.exe -d <distro> -- <remote-ocode> [path]`). The remote TUI
  runs in the local terminal; config, keys, and sessions are the remote's.
- `ocode remote --web <host> [path]` — web mode. Ensure remote binary,
  start `ocode serve` remotely bound to `127.0.0.1` with a generated auth
  token, open an `ssh -L` local port forward, print and open
  `http://localhost:<port>` (token conveyed to the browser, not via argv).
  WSL: no tunnel — Windows shares localhost with WSL.
- `ocode remote-receive-config` — hidden remote-side command; reads a
  config-sync payload from stdin and writes it with `0600` (see below).

### Provisioning (auto-install)

On connect:

1. `ssh <host> 'uname -sm'` → resolve GOOS/GOARCH.
2. Check `~/.ocode/bin/ocode-<version>` exists and is executable remotely.
3. If missing: attempt remote-side download of the matching release
   artifact for the **local client's exact version** (no drift);
   fall back to `scp` of the local binary when OS/arch match;
   otherwise fail with a clear error naming the missing artifact.

WSL distros are always linux; provisioning reuses the same path with
`wsl.exe` as the exec transport.

### Credential & config sync

At connect time (first connect, and re-pushed on later connects when the
local payload hash differs from the last push): push selected
auth profiles + core model config over the SSH channel — piped to
`ocode remote-receive-config` on stdin as a framed JSON payload. Secrets
never appear in argv, logs, or LLM traffic; the remote writes with `0600`
using the same protections as the local profile store
(`internal/auth/profile_store.go`).

Sessions are **not** synced. They live on the remote under the remote
`$HOME`, which makes per-host session storage automatic — no path-collision
handling needed.

Local `internal/projects` recent-projects entries gain an optional `host`
field so remote projects appear in pickers and reconnect in one action.
Absent/empty `host` means local (backward compatible).

### Server hardening (forced by web mode)

- `ocode serve` gains **token auth** (in addition to existing basic auth):
  a launch-time token required on API requests when set.
- The remote launch always binds `127.0.0.1`, so the SSH tunnel is the
  only ingress.
- Host authenticity = system ssh known_hosts behavior, unmodified.

## Error handling

- Unreachable host / auth failure: surface ssh's own stderr verbatim.
- No matching release artifact and arch mismatch for upload: named error,
  no partial install left behind (install to temp name, atomic rename).
- Tunnel drop in web mode: the local supervisor notices the ssh process
  exit and reports it; reconnect is a user action in v1 (no auto-retry).
- `remote-receive-config` validates payload framing and rejects partial
  writes (write temp + rename).

## Testing

- Unit: platform-string → GOOS/GOARCH mapping, install command
  construction, tunnel argument building, token generation, config-sync
  payload framing/validation, `wsl:` target parsing.
- Integration (behind a flag/tag): full connect → provision → passthrough
  and web-mode flows against a local sshd container.
- WSL transport unit-tested via command construction; manual verification
  on a Windows machine.

## Phasing

Single spec, implemented in order:

1. **TUI passthrough + auto-install** — `internal/remote`, `ocode remote`,
   provisioning.
2. **Web tunnel + token auth** — `--web`, server token auth, tunnel
   lifecycle under the supervisor.
3. **Credential sync** — `remote-receive-config`, push-on-connect,
   projects `host` field.
4. **WSL backend** — transport swap (`wsl.exe` for `ssh`/`scp`) in
   `internal/remote`; localhost sharing instead of tunnel.
