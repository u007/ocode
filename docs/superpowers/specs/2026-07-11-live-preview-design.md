# Live Preview / Dev-Server Manager Design

- **Date:** 2026-07-11
- **Status:** Approved (design) — pending implementation plan
- **Goal:** Let ocode build/edit web apps on the fly (bolt.new / AI Studio style) and
  preview them live inside the ocode web UI and desktop app, with hot reload, supporting
  local same-origin access and Tailscale exposure (portless via `tailscale serve`, Funnel opt-in).
  Multiple simultaneous previews, each a separate app/preview session.

---

## 1. User-facing summary

A user (or the ocode agent) creates a web app under `<workspace>/apps/<name>/` (or any path).
ocode detects how to run it, spawns a dev server on a free loopback port, and reverse-proxies it
under a same-origin path `/preview/<id>/`. The ocode web UI and desktop app gain an **Apps /
Preview** tab that lists running previews and opens each in an iframe; Vite/Next hot-reload (HMR)
flows through the proxied WebSocket automatically.

When Tailscale is available and logged in, ocode additionally exposes each preview on the tailnet
via `tailscale serve` (no open firewall port). The Preview tab shows a copyable `*.ts.net` URL so
the user can open the running app on any of their devices. Funnel (public internet) is an explicit
per-preview opt-in with a warning.

---

## 2. Decisions (confirmed with user + advisor)

| Topic | Decision |
|-------|----------|
| Trigger | **Both** — manual (`/preview <path>` / API) **and** auto-start when the agent edits app code |
| Multi-preview | **Yes** — multiple simultaneous preview sessions, one per app |
| Run command | **Auto-detect** framework (package.json, index.html, python, Dockerfile) |
| App location | Default `<workspace>/apps/<name>/`; arbitrary paths allowed (manual) |
| Tailscale | Shell out to **`tailscale serve` CLI** (not embedded tsnet) |
| Exposure | **Tailnet-private by default**; **Funnel opt-in** per preview |
| Core mechanics | **Same-origin reverse proxy** under `/preview/<id}/...` + SPA iframe tab (Approach A) |
| Auth posture | Preview proxy mirrors the SPA catch-all — **not** behind `authMiddleware` (see §7) |
| Persistence | **None in v1** (in-memory registry) |
| tsnet | **Out of scope v1** |

---

## 3. Architecture overview

```
┌──────────────────────────────────────────────────────────────────────┐
│ ocode server (internal/server)  — single http.ServeMux                │
│                                                                        │
│  /api/preview ...            manager API (behind authMiddleware)       │
│  /api/preview/tailscale      tailscale availability probe             │
│  /preview/{id}/{rest...}     upgrade-aware reverse proxy  (NO auth) ◀──┐│
│  /                            SPA catch-all (spaHandler, NO auth)      ││
└──────────────────────────────────────────────────────────────────────┘│
            │ reverse-proxy (strip /preview/{id}, ws upgrade)            │
            ▼                                                              │
┌───────────────────────────┐     ┌──────────────────────────────────┐  │
│ internal/preview.Manager   │     │ Dev server child process          │  │
│  registry: id → Session   │────▶│  (Vite/Next/static/py/docker)     │  │
│  framework auto-detect     │     │  bound to 127.0.0.1:<port>        │  │
│  port alloc, supervisor    │     └──────────────────────────────────┘  │
│  tailscale serve/funnel    │                                            │
└───────────────────────────┘                                            │
            │ tailscale serve --https=<id> https://localhost:<ocodePort>  │
            ▼                                                              │
   https://<id>.ts.net/preview/<id}/  (path preserved → hits proxy above)─┘
```

Desktop: the Wails v3 webview points at the same server origin, so the Preview tab works unchanged.
The dev-server child is a child of the ocode-desktop process and is reaped on quit. On a tailnet
device, the user opens the `*.ts.net` URL directly in their own browser.

---

## 4. `internal/preview` package (new)

### 4.1 Types

```go
type Status string // starting | running | error | stopped

type Session struct {
    ID           string    // stable id, e.g. slug of app name + short random
    Name         string    // human label
    Path         string    // absolute path to the app dir (relative to workDir when given)
    Framework    string    // vite | next | static | python | docker | custom
    Cmd          []string  // resolved command + args (incl. injected --base)
    Port         int       // loopback port the dev server listens on
    Status       Status
    Logs         []string  // ring buffer of recent stdout/stderr lines
    StartedAt    time.Time
    TailscaleURL string    // "" until tailscale serve succeeds
    Funnel       bool      // whether funnel is enabled
    Err          string    // last error if Status==error
}

type StartRequest struct {
    Path   string   // required (relative resolved against workDir)
    Name   string   // optional; derived from dir name if empty
    Cmd    []string // optional explicit command (overrides auto-detect)
    Port   int      // optional explicit port (else auto-allocated)
    Funnel bool     // optional; default false
}
```

### 4.2 Manager

- `New(workDir string) *Manager`
- `Start(req StartRequest) (*Session, error)` — detect framework, allocate port, build command,
  spawn child with captured stdout/stderr (ring buffer, bounded e.g. 500 lines), health-probe the
  port, then (if Tailscale available) `tailscale serve`. Returns the session immediately in
  `starting`; a background goroutine flips it to `running` after the health probe passes.
- `Stop(id string) error` — kill child (and any process group), `tailscale serve --rm` /
  `tailscale funnel --off`, remove from registry.
- `List() []Session` / `Get(id string) (*Session, bool)`
- `StopAll()` — called on server shutdown to reap children and clean tailscale.
- Thread-safe via a `sync.Mutex` around the registry map.

### 4.3 Framework auto-detection (`detect.go`)

Scan `Path` (in order, first match wins):

| Signal | Framework | Start command (port p, id) | Notes |
|--------|-----------|----------------------------|-------|
| `package.json` with `dev` script (Vite/CRA/Vue) | `vite` | `npm run dev -- --port p --host 127.0.0.1 --base /preview/<id>/` | base injected so absolute asset URLs resolve through proxy |
| `package.json` with `dev`/`start` (Next) | `next` | `npx next dev -p p` with a derived `next.config` that adds `basePath:'/preview/<id>'` + `assetPrefix` | Next has **no** env/CLI for basePath — auto-detect writes a sidecar `next.config.<id>.mjs` that re-exports the user's config (or a fresh one) with `basePath` set; if the existing config can't be safely extended, fall back to requiring a manual command |
| `package.json` `start` only (generic node) | `node` | `npm start -- --port p` (+ best-effort base) | unsupported base → requires manual cmd |
| `index.html` present, no build | `static` | `npx serve -l p .` or `python3 -m http.server p` | plain HTML: inject `<base href="/preview/<id>/">` by buffering HTML responses (HTML only, never JS/CSS/binary) |
| `requirements.txt` / `main.py` | `python` | `python3 -m http.server p` or `uvicorn main:app --port p` | heuristic |
| `Dockerfile` | `docker` | `docker build -t ocode-prev-<id> . && docker run -p p:<exposed> ...` | honor `EXPOSE`; map to p |
| none of the above | `custom` | use explicit `Cmd` from request | if no `Cmd` → return error "unsupported app; provide a command" |

Detection returns `{Framework, Cmd, SuggestedPort}`. The caller fills the port (auto or explicit).

**Primary supported framework: Vite** (the bolt.new default — `--base` makes sub-path previewing
and HMR correct with zero extra work). Next, static, python, and docker are supported with the
caveats in the table; Next in particular needs a sidecar config. **Anything we cannot launch with a
real URL prefix falls back to requiring an explicit manual command** — there is no generic
response-body rewriter. This is the deliberate tradeoff that keeps HMR and absolute URLs correct both
locally and remotely.

### 4.4 Port allocation (`ports.go`)

```go
func freePort() (int, error) {
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { return 0, err }
    defer ln.Close()
    return ln.Addr().(*net.TCPAddr).Port, nil
}
```
Allocate, close, then spawn immediately; if the child fails to bind (collision), retry with a new
port (bounded attempts). Mark `running` only after a TCP health probe to `127.0.0.1:<port>`
succeeds (with a short timeout, e.g. 30s, polling).

### 4.5 Child supervision (`supervisor.go`)

- Spawn via `exec.Command` with `Cmd[0]` + `Cmd[1:]`, `Dir = Path`, `Env` includes `PORT`/`PORT_<id>`
  and any framework env (BASE_PATH etc.).
- Capture stdout/stderr line-by-line into the session ring buffer (use `bufio.Scanner` over a pipe;
  drop the scanner's 64KB-line limit by reading in chunks if needed). Never inherit the terminal fd
  (per AGENTS.md TUI-output-safety rule — capture to buffer, surface via logs, not stdout).
- Reap on exit; on non-zero exit set `Status=error` with the tail of logs.
- Track the process (and, on Unix, the process group via `Setpgid`) so `Stop` can kill the whole tree.

### 4.6 Tailscale (`tailscale.go`)

- `Probe() (avail bool, loggedIn bool, version string)`: run `tailscale status --json` (or parse
  text); treat missing binary / not-logged-in as `avail=false`.
- After a session is `running` and `Probe()` says available+logged in:
  - `tailscale serve --https=<id> https://localhost:<ocodePort>` — serves the **ocode server** at
    the tailnet root with the request path preserved, so remote
    `https://<id>.ts.net/preview/<id}/` reaches ocode's proxy. Parse the printed
    `https://<id>.ts.net` base from output; store `TailscaleURL = <base>/preview/<id}/`.
  - If `Funnel`: `tailscale funnel --bg <ocodePort>` (or `tailscale funnel --https=<id>`) on top,
    after the user explicitly opted in. Record the funnel URL.
- On `Stop`: `tailscale serve --rm` and `tailscale funnel --off <port>` (best-effort, log errors).
- **Commands are built with `exec.Command(args...)` — NO shell, NO string interpolation of user
  input.** Validate `id` (slug charset) and `ocodePort` (numeric) before use.

---

## 5. Server routes (`internal/server`, extend `registerRoutes`)

Registered **before** the SPA catch-all (`s.mux.Handle("/", spaHandler(...))` at line 214):

| Method & path | Handler | Auth |
|---------------|---------|------|
| `POST /api/preview` | start: body `StartRequest` → `manager.Start` → 200 `{session}` | `authMiddleware` |
| `GET /api/preview` | list all sessions | `authMiddleware` |
| `DELETE /api/preview/{id}` | stop session | `authMiddleware` |
| `GET /api/preview/tailscale` | `{available, logged_in, version}` | `authMiddleware` |
| `GET /preview/{id}/{rest...}` | reverse proxy to `127.0.0.1:<port>` | **none** (mirrors SPA) |

The proxy handler (`handler_preview.go`):

- Strip the `/preview/{id}` prefix, rewrite the upstream request path, preserve query string.
- Set `X-Forwarded-Host/Proto/Prefix` and `Host: 127.0.0.1:<port>`.
- Use `httputil.ReverseProxy`. **WebSocket upgrade:** detect `Connection: Upgrade` /
  `Upgrade: websocket`; perform a manual hijack round-trip (dial upstream ws, pipe both directions)
  so Vite/Next HMR works. `httputil.ReverseProxy` alone handles ws only if Host/path are correct;
  an explicit upgrade path is more robust — implement the hijack for `Upgrade` requests.
- Validate `id` against the registry; unknown id → 404. Only proxy to the loopback port we spawned
  (never an arbitrary external host).
- For `static` framework HTML responses only, inject `<base href="/preview/<id}/">` if absent
  (scan the buffered body for `<head>`; skip non-HTML / streaming / binary content types).

The `Manager` is created in `server.New` / `desktop.StartServer` and stored on the `Server`
(`s.preview = preview.New(workDir)`). On `StopAll` at shutdown, `s.preview.StopAll()`.

---

## 6. Auto-start on agent edits

- Hook point: the **agent file-mutation completion boundary** (post-tool result aggregation / end
  of an agent turn), NOT the snapshot/backup layer and NOT a filesystem watcher.
- Mechanism: collect the set of file paths mutated during the turn; group by app root
  (`apps/<name>` or any dir with a detectable framework); for each app root with no existing preview
  and with auto-mode on, call `manager.Start`.
- Gating:
  - Auto-mode toggle `/preview auto` (default **on**).
  - By default auto-starts **only** for `apps/<name>/` subdirs (conservative — avoids runaway
    spawns when the agent edits unrelated project files). Arbitrary paths require the manual command.
  - Debounce: at most one auto-start per app root per turn; never restart an already-running preview.
- The agent (and the `/preview` slash command) share the same `manager.Start` path.

---

## 7. Security

- **Preview proxy routes are intentionally NOT behind `authMiddleware`**, mirroring the existing SPA
  catch-all (`s.mux.Handle("/", spaHandler(...))`, which is also unauthenticated). Rationale:
  - The SPA and its assets are already served unauthenticated; previews follow the same posture for
    consistency.
  - HMR subresources (JS/CSS) and the HMR WebSocket are loaded by the browser without credentials;
    putting auth here would require a fragile cookie/bootstrap flow that the SPA itself does not use.
  - In the desktop app the server is bound to `127.0.0.1` (loopback only), so only the local machine
    can reach previews. In `ocode-server` (potentially bound wider) the posture matches the SPA.
  - Tailscale **tailnet-private** exposure means only the user's own devices can reach it; this is
    the same trust boundary as the SPA.
- **Funnel (public) is explicit opt-in only**, never default, and the UI shows a clear warning that
  the preview becomes reachable by anyone on the internet.
- **Tailscale commands use `exec.Command` with validated args** (slug id, numeric port). No shell,
  no interpolation of user-supplied paths into the command string.
- **The proxy only forwards to `127.0.0.1:<port>` of a session we spawned** — never to an arbitrary
  external URL supplied by the client.
- `id` is server-generated (slug + random), preventing path traversal / injection in the proxy and
  tailscale serve name.

*Future hardening (out of scope):* scoped preview cookie/token so previews can be auth-protected
independently of the SPA; this is only needed if we later want authenticated remote previews.

---

## 8. SPA — Apps / Preview tab (`web/src`)

- `web/src/components/Layout/TopTabs.tsx`: add `{ id: "preview", label: "Apps", icon: ... }` to
  `mainTabs`.
- New `web/src/components/Preview/PreviewPanel.tsx`:
  - Lists previews: name, framework badge, status (starting/running/error/stopped), local URL
    (`/preview/{id}/`), Tailscale URL (copyable + "open externally"), Funnel toggle, Stop button,
    expandable recent logs.
  - "+ New preview" form: path (text), optional cmd (text), optional port, Funnel checkbox, Start.
  - Clicking a preview opens an **iframe** (`<iframe src="/preview/{id}/">`) in the panel (split or
    modal). HMR flows through the proxied WebSocket automatically.
- New API client methods in `web/src/api/` (mirror existing `api` patterns):
  `listPreviews()`, `startPreview(req)`, `stopPreview(id)`, `tailscaleStatus()`.
- Desktop parity: none required — the desktop webview is the same origin; the tab works unchanged.
  The `*.ts.net` URL is opened in the user's own browser on the other device.

---

## 9. Config & storage

- v1: registry is **in-memory**; generated apps default to `<workspace>/apps/<name>/` (the preview
  `Path` is resolved against `Server.workDir`). 
- The auto-mode flag lives in server/runtime config (default on).
- **Persistence of preview definitions is out of scope v1** (noted as a future enhancement: persist
  `{path, cmd, funnel}` so they auto-restart on server boot).

---

## 10. Testing & validation

- `internal/preview` unit tests:
  - framework detection for each signal (table test with temp dirs).
  - port allocation returns a free port; retry-on-collision path.
  - proxy handler via `httptest` upstream: verifies prefix strip, query preservation, and WebSocket
    upgrade (use a tiny ws echo server).
  - tailscale command building with a mocked `exec` (assert exact args; assert injection-safe).
  - Manager `Start`/`Stop` lifecycle with a stub child (e.g. `python3 -m http.server`) — verifies
    status transitions and `StopAll` cleanup.
- `internal/server`: route registration order test (proxy registered before SPA catch-all);
  proxy 404 on unknown id.
- `go build ./...` and `go vet ./...` must pass.
- Manual end-to-end (macOS + Tailscale installed):
  1. `npx create-vite` a small app under `apps/demo`; open the Preview tab → app renders in iframe.
  2. Edit a component file → confirm HMR reloads the iframe automatically.
  3. Confirm the Tailscale URL appears and opens the same app on a phone on the tailnet.
  4. Toggle Funnel → confirm public URL works and warning shown.
  5. Stop the preview → tailscale serve cleaned up.

---

## 11. Out of scope (v1)

- Persisting preview definitions across restarts.
- Embedded `tsnet` node.
- Generic response-body rewriting for frameworks that cannot take a base path (those require a
  manual command).
- Authenticated remote previews (scoped cookie) — only relevant if we later want non-public
  auth-gated remote access.
- Multi-tenant / shared-hosting of previews.
