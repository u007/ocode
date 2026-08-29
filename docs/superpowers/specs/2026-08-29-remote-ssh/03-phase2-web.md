# Part 03 — Phase 2: Web Mode

Self-contained. Goal: **browser UI on a remote backend through an SSH
tunnel.** Success metric: kill the tunnel, rerun the command, the same
session resumes. Builds on the connect/provision/sync flow shipped in
Phase 1 (stages 1–4 are identical; only the launch stage differs).

## Command

```
ocode remote --web <[user@]host> [path] [--no-sync]
```

Flow after the shared ensure-binary + sync stages:

1. **Discover or start server** (see server-reuse below). Result:
   remote loopback port + API token.
2. **Tunnel** — `ssh -N -L <localport>:127.0.0.1:<remoteport> <host>`,
   registered with the process supervisor. `<localport>` is an ephemeral
   free local port.
3. **Open** — print and open
   `http://localhost:<localport>/#token=<token>`. All stages render the
   same staged progress as Phase 1; a failure means the browser never
   opens and the terminal shows the failing stage + verbatim stderr.
4. The CLI stays in the foreground supervising the tunnel. Ctrl-C closes
   the tunnel and exits; the remote server keeps running (sessions are
   server-side). If the tunnel process dies, the CLI reports it and
   exits non-zero; reconnect is rerunning the command (auto-retry is out
   of scope for this phase).

## Server reuse (session resume)

The remote server persists its identity so reconnects resume rather than
proliferate:

- On launch in remote mode, `ocode serve` writes
  `~/.ocode/remote/serve.json` on the **remote**: `{pid, port, token,
  version, startedAt}`, `0600`, temp+rename.
- Discovery on connect: read the file over ssh; if present, verify the
  pid is alive and the version matches the client; probe
  `GET /api/health` through a short-lived exec check. Live + matching →
  reuse port and token. Dead/stale/mismatched version → delete the file,
  start fresh.
- Start fresh: launch `<remote-ocode> serve --remote --host 127.0.0.1
  --port 0` detached from the ssh session (survives disconnect —
  setsid/nohup semantics), wait for the state file to appear (bounded
  wait), read port + token from it.
- The server is the sole writer of its state file; the token is
  generated server-side at startup and reaches the client only by
  reading the `0600` file over the authenticated ssh channel.

## Server hardening (changes to `internal/server`)

- New remote launch mode (`--remote`): **always** binds `127.0.0.1`,
  **always** generates and requires an API token; refuses to start with
  auth disabled.
- Token auth middleware: token accepted only via the `Authorization:
  Bearer` header (plus the WebSocket subprotocol/first-message pattern
  for `/api/terminal` and any other WS endpoints, which cannot set
  headers from the browser). Never accepted via query string — query
  strings reach logs.
- Existing local/basic-auth behavior is unchanged when `--remote` is not
  set.

## Token conveyance (browser)

Token travels in the **URL fragment** (`/#token=…`). Fragments are never
sent in HTTP requests, so they cannot reach server or proxy logs. The
web UI on boot: reads the fragment, stores the token in
`sessionStorage`, strips the fragment via `history.replaceState`, and
attaches `Authorization: Bearer` to every API/SSE call (and the WS
handshake pattern above). Missing/invalid token → a minimal "reconnect
from your terminal" page, no API access.

## Web UI changes

- API client (`web/src/api/client.ts`): token bootstrap from fragment →
  sessionStorage → header injection; 401 handling → reconnect page.
- Everything else is unchanged — the web UI already speaks the server
  API exclusively; the tunnel is transparent to it.

## WSL note

When the target is a WSL distro (Phase 3), the tunnel stage is skipped
entirely: Windows shares localhost with WSL, so the browser opens
`http://localhost:<remoteport>/#token=…` directly.

## Error handling

- Port collision / tunnel bind failure → retry once with a new ephemeral
  port, then fail with ssh's stderr.
- State file present but unreadable/corrupt → treat as stale, start
  fresh.
- Server fails to start → bounded wait expires; surface the server's
  startup log tail (captured to `~/.ocode/remote/serve.log` remotely).
- Version mismatch on reuse → old server is left running but ignored;
  a fresh matching-version server starts on a new port. (Stopping the
  old one is the user's call; the CLI prints a notice with its pid.)

## Testing

- Unit: state-file read/verify/stale logic, token middleware (header
  accepted, query string rejected, WS pattern), fragment bootstrap
  (web unit test), tunnel arg construction, reuse-vs-fresh decision
  table.
- Integration (flag-gated): start serve in remote mode in a container,
  tunnel, hit `/api/chat` + SSE through it; kill tunnel, reconnect,
  assert same session list.
