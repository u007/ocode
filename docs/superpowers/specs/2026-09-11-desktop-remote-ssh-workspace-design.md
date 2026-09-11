# Desktop Remote SSH Workspace — Design Spec

Date: 2026-09-11
Status: Draft

## Problem

User wants ocode-desktop running locally on their Mac, but all agent work
(chat, files, git, cron, LSP, bash) executing on a remote SSH host — like
VS Code Remote-SSH.

## Principle

**Remote server is the execution authority.** Do NOT add remote execution
to individual tools (BashTool, file tools, etc.). Instead, the remote
`ocode serve` instance owns everything: agent, LSP, git, files, cron.
The desktop app is a thin UI/control plane that connects to the remote
server through an SSH tunnel.

## Pre-implementation fixes to shared infrastructure

These must be fixed in `internal/remote/serve.go` (or wrapped at launch)
BEFORE workspace code can depend on them. They affect existing `ocode remote`
users too, so they are correct-first, not speculative.

### Fix 1: Workspace-aware server launch (`serve.go:120-123`)

Current `launchServerCmd` starts `ocode serve --remote --host 127.0.0.1 --port 0`
from the SSH session's default directory. The `--path` flag (or a `cd` to
RemotePath) must precede the serve invocation so the remote server's
workDir matches the workspace project root. Required before `EnsureRemoteServer`
can be workspace-specific.

### Fix 2: Per-workspace state file (`serve.go:32`)

Current path is a single shared `~/.ocode/remote/serve.json`. A second
remote project (or stale state from a disconnected project) can cause the
desktop to attach to the wrong server. The state path must include a
workspace identifier, e.g. `~/.ocode/remote/workspaces/<workspaceID>/serve.json`.
`DiscoverServer` and `StartFreshServer` must accept a workspace ID parameter.
`EnsureRemoteServer` must pass it through. This requires a workspace ID
type (UUID or slug) established in the design.

### Fix 3: Dual-port tunnel (API + browse)

`ServeState` carries `BrowsePort` (`serve.go:22`). The tunnel must forward
**two** local ports: one for the API, one for the browse origin (same port
number on each side per `tunnelArgs`). The spec's `tunnelPort` field is
insufficient — `RemoteWorkspace` must track both:
- `APIPort` (local) → `remoteAPIPort` (remote)
- `BrowsePort` (local) → `remoteBrowsePort` (remote)

`LocalURL()` returns the API URL; browse URL is derived separately.

## Architecture

```
Desktop (Mac):                              Remote (SSH host):
┌──────────────────────┐                    ┌──────────────────────────┐
│ Embedded SPA (web)   │                    │ ocode serve --remote     │
│  (served locally)    │                    │  (runs agent, LSP, git)  │
├──────────────────────┤                    │  WorkDir = remote path   │
│ Local HTTP server    │  SSH tunnel        │  State file:             │
│  (binds 127.0.0.1   │◄── apiPort ──────► │  ~/.ocode/remote/        │
│  only in remote     │◄── browsePort ─────►│  workspaces/<wsID>/      │
│   mode)              │    →remotePorts    │  serve.json              │
│  / → serve SPA       │                    │                          │
│  /api/* ──proxy──► ──┼─► localhost:      │  Token/auth from ocode   │
│  localhost:API_PORT  │                    │  config on remote host   │
└──────────────────────┘                    └──────────────────────────┘
```

### Local mode (unchanged)
Desktop runs full ocode server locally on `0.0.0.0:0`. No behavioral change.

### Remote mode (new)
- Local server binds `127.0.0.1:0` (NOT `0.0.0.0`) — a remote workspace
  must not expose a LAN listener. LAN/tailscale sharing is intentionally
  unavailable for remote workspaces (the remote server IS the LAN host).
- Desktop runs embedded SPA + reverse proxy server.
- All `/api/*` calls tunnel through SSH to the remote server.
- Browse origin (embedded Chrome panel) tunnels on its own port.
- The remote server owns all execution: agent, LSP, git, files, cron.

## Flow

1. User selects "New Remote SSH Workspace" from workspace picker
2. User enters: SSH target (user@host), remote project path
3. Desktop generates a workspace ID (UUID) for this remote workspace
4. Desktop establishes SSH connection via `internal/remote.SSHTransport`
5. Desktop provisions remote binary via `internal/remote.PrepareLocalBuild` + `Copy`
6. Desktop starts remote server via `internal/remote.EnsureRemoteServer`
   (reuse if alive+matched version+healthy, else fresh start)
   — workspace ID passed through to per-workspace state file
7. Desktop establishes SSH tunnel:
   `-N -L API_PORT:127.0.0.1:REMOTE_API_PORT -L BROWSE_PORT:127.0.0.1:REMOTE_BROWSE_PORT target`
   via `internal/remote.StartTunnel`
8. Desktop starts local HTTP server on `127.0.0.1:0`; `/api/*` routes proxy to
   `http://127.0.0.1:API_PORT`; `/api/browse/config` routes to browse port
9. Webview talks to local server as usual; `/api/*` transparently reaches remote

## Failure & Reconnect Lifecycle

| Event | Behavior |
|-------|----------|
| Tunnel process exits | Desktop detects via ProcessSupervisor; web UI shows "Remote connection lost — reconnecting"; desktop attempts reconnect once |
| Remote server dies | Tunnel stays up but health probe fails; desktop reconnects to remote server via `EnsureRemoteServer`, re-establishes tunnel |
| Both dead | Fresh connect from step 4 |
| Version mismatch | New server starts; stale server PID reported to user via web UI; old server left running |
| SSH auth failure | Error shown inline; no retry (user must fix ssh config/agent) |
| Tunnel port collision | `FreeLocalPort` + retry (existing behavior) |
| Workspace disconnect | `RemoteWorkspace.Disconnect()`: kill tunnel process, leave remote server running for resume |
| Desktop quit | Shutdown supervisor → tunnel dies; remote server survives (nohup + disown) |

## New Files

### `internal/remote/workspace.go`
`RemoteWorkspace` struct managing the full remote session lifecycle:
- Fields: `WorkspaceID` (string UUID), `Target`, `RemotePath`, `Transport`, `ServeState`, `APIPort`, `BrowsePort`, `tunnelProcess`, `localAPIURL`
- `Connect()`: runs provision → `EnsureRemoteServer` (with workspace ID) → dual-port tunnel establishment
- `Disconnect()`: tears down tunnel process, leaves remote server running (resumeable)
- `Reconnect()`: stops tunnel, re-provisions server, re-establishes tunnel
- `APIURL()`: returns `http://127.0.0.1:APIPort` for desktop server to proxy to
- `BrowseURL()`: returns `http://127.0.0.1:BrowsePort` for browse origin
- Reuses `provision.go`, `serve.go`, `StartTunnel`, `FreeLocalPort` from `internal/remote`

### `internal/desktop/proxy.go`
Reverse proxy integrated with desktop boot (NO import cycle with `internal/server`):
- Uses `net/http/httputil.ReverseProxy` to forward `/api/*` to remote server
- **Authentication**:
  - Accepts browser's local desktop token (validates locally)
  - Extracts remote bearer token from `ServeState.Token`
  - Injects remote token into `Authorization` header of proxied requests
  - Strips `?token=` query param before forwarding (never exposes remote token)
  - Remote token NEVER reaches the browser, is never logged, never stored
- **Request handling**:
  - Regular requests: straightforward reverse proxy
  - SSE/streaming: httputil.ReverseProxy handles streaming via Flush
  - File uploads: streamed, not buffered in memory
  - `/api/browse/config`: routes to browse port (different upstream)
  - WebSocket/browse-origin traffic: routes to browse port
- **Error handling**: 502 Bad Gateway with human-readable message on tunnel/proxy failure; web UI shows "Remote server unreachable" banner
- Created in `internal/desktop` (NOT `internal/server`) because it needs to import `internal/remote` for workspace state, and `internal/server` cannot import `internal/remote` (cycle per `serve.go:17-19`)

### `internal/desktop/remote.go`
Desktop workspace state for remote sessions:
- `Workspace` struct: `Mode` (Local/RemoteSSH), `WorkspaceID`, `Target`, `RemotePath`, `*remote.RemoteWorkspace`, `*desktop.RemoteProxy`
- `OpenRemoteWorkspace(target, path)`: generates ID → connects → creates proxy → returns Workspace
- `Close()`: disconnects proxy, disconnects remote workspace

## Modified Files

### `internal/remote/serve.go` (pre-implementation fix)
- `launchServerCmd`: add `cd RemotePath &&` prefix OR `--path RemotePath` flag so remote server starts in workspace directory
- State file path: change from `~/.ocode/remote/serve.json` to `~/.ocode/remote/workspaces/<workspaceID>/serve.json`
- `DiscoverServer`/`StartFreshServer`/`EnsureRemoteServer`: accept workspace ID parameter

### `internal/desktop/boot.go`
- `StartServer` gets a new parameter: `workspace *Workspace` (nil = local mode)
- In remote mode: bind `127.0.0.1:0`, start reverse proxy instead of full ocode server
- SPA still served from embedded assets (unchanged)

### `cmd/ocode-desktop/main.go`
- Workspace picker UI flow: "Open Local Folder" (existing) + "Connect to Remote Host" (new)
- Remote path input: SSH target (user@host) + remote project path
- Boot flow branches on workspace mode (local → `StartServer(webFS, workDir)`, remote → `StartServer(webFS, nil)` + proxy setup)

## Tests

### `internal/remote/workspace_test.go`
- `TestWorkspace_Connect`: mock Transport → provision → server start → tunnel; verify `APIURL()` and `BrowseURL()` point to local ports
- `TestWorkspace_Disconnect`: connect then disconnect; verify tunnel process killed; remote server still running
- `TestWorkspace_Reconnect`: connect, kill remote server, reconnect; verify new server + new ports
- `TestWorkspace_PerWorkspaceState`: two workspaces on same host; verify separate state files

### `internal/desktop/proxy_test.go`
- `TestProxy_ForwardsRequest`: mock remote server; verify request reaches it with injected auth
- `TestProxy_StripsToken`: request has `?token=local`; verify forwarded request has no query token
- `TestProxy_InjectsRemoteToken`: verify `Authorization: Bearer <remote>` header on proxied request
- `TestProxy_SSEFlush`: SSE stream reaches client without buffering
- `TestProxy_ErrorOnDisconnect`: remote down; verify 502 with readable message

### `internal/desktop/remote_test.go`
- `TestWorkspace_Open`: open remote workspace; verify workspace ID, connected state
- `TestWorkspace_Close`: open then close; verify cleanup

### Local mode regression
- `internal/server/*_test.go`: unchanged (local mode doesn't construct proxy)
- `internal/tool/exec_test.go`: unchanged (BashTool still uses exec.Command in local mode)
- `internal/agent/*_test.go`: unchanged

## What Stays Untouched (Local mode)
- `internal/tool/exec.go` — BashTool uses `exec.Command` unchanged
- `internal/agent/agent.go` — agent construction unchanged
- `internal/remote/connect.go` — TUI mode unchanged
- `internal/remote/transport.go` — Transport interface unchanged
- `internal/remote/ssh.go` — SSHTransport unchanged (consumed, not modified)
- `internal/remote/provision.go` — reused as-is
- `internal/remote/target.go` — unchanged
- All local mode tests

## What Stays Untouched (Local mode)
- `internal/tool/exec.go` — BashTool uses `exec.Command` unchanged
- `internal/agent/agent.go` — agent construction unchanged
- `internal/remote/connect.go` — TUI mode unchanged
- `internal/remote/transport.go` — Transport interface unchanged (consumed as-is)
- `internal/remote/ssh.go` — SSHTransport unchanged (consumed as-is)
- `internal/remote/provision.go` — reused as-is
- `internal/remote/target.go` — unchanged
- All local mode tests

## Pre-Implementation Changes to `internal/remote/serve.go`
serve.go WILL be modified (minimal, additive) to support workspace context. These are corrections to existing infrastructure — not speculative redesign:

1. **`launchServerCmd`** (serve.go:120-123): Add a `--path` parameter or `cd RemotePath &&` prefix so the remote server starts in the workspace directory. This honors RemotePath. Existing `ocode remote --web` behavior is unchanged (it always launches into the current dir).
2. **State file path** (serve.go:32): Extend from `~/.ocode/remote/serve.json` to `~/.ocode/remote/workspaces/<workspaceID>/serve.json`. `DiscoverServer`, `StartFreshServer`, `EnsureRemoteServer` gain a `workspaceID string` parameter. Callers that don't pass a workspace ID continue to use the legacy single-file path (backward compatible).
3. **These changes are additive**: existing callers that pass empty workspace ID keep existing behavior.

## Resolved: State Path Convention
The spec uses `~/.ocode/remote/...` because serve.go runs on the REMOTE host (where it is its own process with its own path conventions). This is a remote-host state path, not a desktop-local path. It is separate from the desktop host's `internal/paths.GlobalDataDir()` (`~/.local/share/opencode` on macOS). The workspace ID is a remote-host identifier. This is justified because: (a) the remote server is a separate process, (b) changing serve.go to use GlobalDataDir would alter its existing behavior for `ocode remote --web` users, and (c) remote workspace state is remote-host concern. If desired, aligning remote state with GlobalDataDir is a follow-up.

## Reused Infrastructure
| Component | Package | Usage |
|-----------|---------|-------|
| `Transport` interface | `internal/remote` | SSH exec/copy for provisioning |
| `SSHTransport` | `internal/remote/ssh.go` | System ssh, ssh-agent, known_hosts |
| `PrepareLocalBuild` | `internal/remote/provision.go` | Cross-compile remote binary |
| `EnsureRemoteServer` | `internal/remote/serve.go` | Discover/start remote ocode (needs workspace ID param) |
| `DiscoverServer` | `internal/remote/serve.go` | Check for reusable server (needs workspace ID param) |
| `StartTunnel` | `internal/remote/serve.go` | SSH port forwarding (dual-port) |
| `FreeLocalPort` | `internal/remote/serve.go` | Find ephemeral local port |
| `Target` | `internal/remote/target.go` | SSH target (host, user, ssh-path) |
| `ProcessSupervisor` | `internal/tool` | Track tunnel process lifecycle |

## Security
- **No private keys/passwords stored** in ocode config or auth.json
- Uses system `ssh`, `~/.ssh/config`, `ssh-agent`, `known_hosts`
- Host key verification always enforced (never accept blindly)
- Auth token flow: browser → local token (validated locally) → proxy → remote token (from ServeState, injected in header, never exposed)
- Remote workspace listener binds `127.0.0.1` only (no LAN exposure)

## Credential Persistence
- Store workspace config (workspaceID, host, user, remote path) in desktop workspace config
- NOT in auth.json (that's for API keys/tokens, not SSH targets)
- Reuse existing `internal/projects` or `internal/config` for workspace list

## Resolved Open Questions
1. **Workspace config storage**: `internal/desktop/remote.go` manages workspace state; config persists via existing ocode config mechanisms (to be confirmed against `internal/config`).
2. **Proxy ownership**: `internal/desktop/proxy.go` (avoids server↔remote import cycle per `serve.go:17-19`).
3. **Failure handling**: See Failure & Reconnect Lifecycle table above.
4. **Error presentation**: Proxy returns 502 with human message; web UI shows "Remote server unreachable" banner (web UI changes out of scope for this spec — handled as a separate frontend task).
5. **LAN binding**: Local listener binds `127.0.0.1` ONLY in remote mode; LAN/tailscale sharing unavailable for remote workspaces by design.
