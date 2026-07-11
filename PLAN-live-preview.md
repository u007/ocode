# Live Preview / Dev-Server Manager — Implementation Plan

> Companion to `docs/superpowers/specs/2026-07-11-live-preview-design.md` (approved).
> The `writing-plans` skill is not installed in this repo, so this plan is authored directly
> following the same structure (phased, file-level, with validation gates).

## Executive Summary

Implement a bolt.new / AI Studio-style **Live Preview** feature for ocode:
- A new `internal/preview` package spawns and supervises dev servers, auto-detecting the framework.
- The server reverse-proxies each running app under same-origin `/preview/{id}/` (HTTP + WebSocket
  for HMR), so the web UI and desktop app can embed it in an iframe.
- A new **Apps / Preview** tab in the SPA lists previews, lets you start/stop them, and shows a
  copyable Tailscale URL when available.
- Tailscale `serve` (portless, tailnet-private) exposes previews on the user's tailnet; Funnel is an
  explicit per-preview opt-in.
- Auto-start: when the agent edits app code under `apps/<name>/`, ocode auto-starts a preview.

Phases are ordered so each is independently buildable and testable. Validation gates use
`go build`, `go vet`, `go test`, and a manual Vite HMR + Tailscale check.

---

## Current State Analysis (verified)

- **Server mux** (`internal/server/server.go`): `registerRoutes()` registers `/api/*` behind
  `authMiddleware` (HTTP Basic/Bearer, `checkAuth` at line 228). The SPA catch-all
  `s.mux.Handle("/", spaHandler(s.webFS))` (line 214) is **not** behind auth. A new
  `/preview/{id}/{rest...}` route must be registered **before** line 214.
- **Server construction**: `New(addr, username, password, webFS)` (line 60). Desktop calls
  `server.New("127.0.0.1:0", "ocode", token, webFS)` (`internal/desktop/boot.go`). The server's own
  bound port is needed for Tailscale (`tailscale serve ... https://localhost:<ocodePort>`).
- **SPA tabs**: `web/src/components/Layout/TopTabs.tsx` `mainTabs` array. Router is react-router-dom
  v7 (`web/src/App.tsx`). API client in `web/src/api/`.
- **Agent file mutation**: tools call `internal/snapshot.Backup` before edits; tool results flow
  through `internal/agent`. The auto-start hook attaches at the agent turn/file-mutation completion
  boundary (NOT the snapshot layer).
- **No** existing process supervisor or Tailscale integration.

---

## Phase 0 — Scaffolding & types

**New files:**
- `internal/preview/preview.go` — package doc, `Session`, `StartRequest`, `Status` constants,
  `Manager` struct + constructor `New(workDir string) *Manager`, registry map + `sync.Mutex`,
  `List`/`Get`/`StopAll`.

**Validation:** `go build ./internal/preview/...` compiles; `go vet` clean.

---

## Phase 1 — Framework auto-detection (`internal/preview/detect.go`)

- `Detect(path string) (Framework string, Cmd []string, err error)` implementing the §4.3 matrix:
  - `package.json` present → parse for `scripts.dev`/`scripts.start`.
    - Vite/CRA/Vue (dev script) → `[]string{"npm","run","dev","--","--port",fmt.Sprint(p),
      "--host","127.0.0.1","--base","/preview/<id>/"}`. **Callers pass `p` and `id`** — so signature
      is `Detect(path, id string, port int)`.
    - Next → `[]string{"npx","next","dev","-p",fmt.Sprint(port)}` **plus** write a sidecar
      `next.config.<id>.mjs` (see Phase 1a).
    - generic `start` → `[]string{"npm","start","--","--port",...}` (best-effort).
  - `index.html` (no package.json) → `[]string{"npx","serve","-l",fmt.Sprint(port),"."}` (static).
  - `requirements.txt`/`main.py` → `[]string{"python3","-m","http.server",fmt.Sprint(port)}`.
  - `Dockerfile` → `[]string{"docker","build","-t","ocode-prev-<id>",".",
    "&&","docker","run","-p",fmt.Sprint(port)+":<expose>",...}` (use `sh -c` ONLY with our own
    fixed args, no user interpolation — better: two-step in supervisor).
  - none → return `("custom", nil, ErrUnsupportedApp)`.
- Unit test `detect_test.go`: table test with temp dirs for each signal; assert framework + command
  shape; assert `ErrUnsupportedApp` for an empty dir.

**Phase 1a — Next sidecar config:** helper `writeNextConfigSidecar(path, id string) error` that, if
`next.config.*` exists, copies/extends it (re-export with `basePath`/`assetPrefix`); else writes a
minimal `next.config.<id>.mjs`. Returns error if it cannot safely extend → caller falls back to
manual command. The sidecar is written under the app dir; `Stop` removes it.

**Validation:** `go test ./internal/preview/ -run Detect`.

---

## Phase 2 — Port allocation & child supervision (`internal/preview/ports.go`, `supervisor.go`)

- `freePort() (int, error)` — `net.Listen("tcp","127.0.0.1:0")`, read port, close.
- `supervisor` fields: `cmd *exec.Cmd`, `cancel context.CancelFunc`, `logBuf *ring.Buffer`
  (bounded ~500 lines, thread-safe). Spawn with `Dir=path`, `Env` incl. `PORT`, `BASE_PATH`,
  `HOST=127.0.0.1`; capture stdout/stderr via `cmd.StdoutPipe`/`StderrPipe` + `bufio.Scanner`
  (raise the token-size limit or read in chunks so long lines don't drop). **Never inherit the
  terminal fd** (capture to buffer; surface via `Session.Logs`).
- On Unix set process group (`SysProcAttr.Setpgid`) so `Stop` kills the whole tree.
- Health probe: poll `net.DialTimeout("tcp","127.0.0.1:port",...)` up to ~30s; flip `Status`
  `starting`→`running` (or `error` with tail of logs on timeout/exit).
- `Stop`: `cancel()`/kill process group; drain; remove Next sidecar if present.

**Validation:** `go test ./internal/preview/ -run Supervisor` with a stub child (`python3 -m
http.server`); assert `running` after probe, `Stop` reaps the process (pgrep returns none), logs
captured.

---

## Phase 3 — Manager start/stop wiring (`internal/preview/preview.go`)

- `Start(req StartRequest) (*Session, error)`:
  1. resolve `req.Path` against `workDir` (abs).
  2. `detect` → framework + base cmd (or use explicit `req.Cmd`).
  3. `freePort()` (or `req.Port`); retry on bind collision (bounded).
  4. create `Session{ID: slug(name)+rand, ...}`, register, spawn supervisor (background goroutine
     updates status + triggers Tailscale after `running`).
  5. return session (status `starting`).
- `Stop(id)` → supervisor.Stop + `tailscale serve --rm` / `funnel --off` (Phase 4) + deregister.
- `StopAll()` → stop every session (call on server shutdown).

**Validation:** `go test ./internal/preview/ -run Manager` end-to-end with `python3 -m http.server`.

---

## Phase 4 — Tailscale integration (`internal/preview/tailscale.go`)

- `Probe() (avail, loggedIn bool, version string)`: `tailscale status --json` (or text parse);
  missing binary / not-logged-in → `avail=false`.
- `Serve(id string, ocodePort int, funnel bool) (url string, err error)`:
  - `exec.Command("tailscale","serve","--https="+id,"https://localhost:"+fmt.Sprint(ocodePort))`
    (args only; validate `id` slug + numeric port). Parse the `https://<id>.ts.net` base from
    stdout/stderr; return `<base>/preview/<id>/`.
  - if `funnel`: `exec.Command("tailscale","funnel","--bg",fmt.Sprint(ocodePort))`.
- `Unserve(id string, ocodePort int)`: `tailscale serve --rm`; `tailscale funnel --off
  <ocodePort>` (best-effort).
- `Manager` stores `ocodePort` (set via `New(workDir, ocodePort)` or a `SetServerPort` method) and
  calls `Serve` once a session is `running` (and `Probe()` says available+logged in); stores
  `TailscaleURL` on the session.
- Unit test `tailscale_test.go`: mock `exec` (e.g. via an injectable command runner interface) to
  assert exact args and injection-safety; assert URL parsing.

**Validation:** `go test ./internal/preview/ -run Tailscale` (mocked); manual Tailscale check in
Phase 8.

---

## Phase 5 — Server routes & proxy (`internal/server/handler_preview.go` + `server.go`)

- Add `preview *preview.Manager` field to `Server`; construct in `New` (and `desktop.StartServer`
  passes `workDir`; set `ocodePort` after `Listen`).
- In `registerRoutes()`, **before** the SPA catch-all (line 214):
  - `POST /api/preview` (auth) → parse `StartRequest`, `manager.Start`, 200 JSON session.
  - `GET /api/preview` (auth) → JSON list.
  - `DELETE /api/preview/{id}` (auth) → `manager.Stop`, 200.
  - `GET /api/preview/tailscale` (auth) → `preview.Probe()` JSON.
  - `GET /preview/{id}/{rest...}` (**no auth**, mirrors SPA) → `handlePreviewProxy`.
- `handlePreviewProxy`:
  - look up session by `id`; 404 if missing.
  - strip `/preview/<id>` prefix; build upstream `http://127.0.0.1:<port><rest>?<query>`.
  - `httputil.ReverseProxy` with `Director` setting `Host`, `X-Forwarded-Host/Proto/Prefix`.
  - **WebSocket upgrade**: if `Upgrade: websocket`, hijack both ends and pipe (manual round-trip)
    so Vite/Next HMR works.
  - **Static HTML `<base>` injection** (HTML content-type only, buffer + scan for `<head>`, skip
    binary/streaming): inject `<base href="/preview/<id>/">` when absent.
- Register `s.preview.StopAll()` in the server's shutdown path (where `Listen`/`Serve` currently
  returns/tears down).

**Validation:** `go test ./internal/server/ -run Preview` — httptest upstream asserting prefix
strip, query preservation, unknown-id 404, and a WebSocket upgrade round-trip against a tiny ws
echo server. `go build ./...` + `go vet ./...`.

---

## Phase 6 — Auto-start on agent edits (`internal/agent`)

- Add a post-tool / end-of-turn hook: collect mutated file paths for the turn, group by app root
  (`apps/<name>` or any dir whose `preview.Detect` succeeds), and for each root with no existing
  preview and auto-mode on, call `manager.Start`.
- New config flag `preview.auto` (default **on**), toggled via `/preview auto` slash command.
- Gating: default auto-starts **only** `apps/<name>/` subdirs; arbitrary paths require the manual
  command. Debounce: ≤1 auto-start per app root per turn; never restart a running preview.
- Guard against runaway spawns (cap concurrent auto-started previews; log skips).
- **No fs watcher, no snapshot-layer hook** (per design).

**Validation:** unit test simulating a turn that writes `apps/demo/index.html` → asserts a preview
session is created; a turn that writes unrelated project files → no preview. Manual check in Phase 8.

---

## Phase 7 — SPA Apps / Preview tab (`web/src`)

- `web/src/components/Layout/TopTabs.tsx`: add `{ id:"preview", label:"Apps", icon: Boxes }`.
- `web/src/components/Preview/PreviewPanel.tsx` (new): list previews (name, framework badge, status,
  local URL `/preview/<id>/`, Tailscale URL copyable + "open externally", Funnel toggle, Stop,
  expandable logs); "+ New preview" form (path, optional cmd, optional port, Funnel checkbox, Start);
  click a preview → `<iframe src={"/preview/"+id+"/"}>` in the panel.
- `web/src/api/preview.ts` (new): `listPreviews()`, `startPreview(req)`, `stopPreview(id)`,
  `tailscaleStatus()` mirroring existing `api` client patterns (`authHeaders`, `apiPath`).
- Wire `PreviewPanel` into `App.tsx` route/tab switch alongside the other panels.
- Add a `/preview` slash command (manual start) reusing `startPreview`.

**Validation:** `cd web && pnpm build` (or `pnpm tsc --noEmit`) passes; manual UI check in Phase 8.

---

## Phase 8 — Manual end-to-end validation (macOS + Tailscale)

1. `npx create-vite` a small app under `apps/demo`; open the Apps tab → app renders in the iframe.
2. Edit a component → confirm HMR reloads the iframe automatically.
3. Confirm the Tailscale URL appears and opens the same app on a phone on the tailnet.
4. Toggle Funnel → confirm public URL works and the warning is shown.
5. Stop the preview → confirm `tailscale serve --rm` / `funnel --off` cleanup; `StopAll` on quit.

**Gate:** all five steps pass before marking the feature complete.

---

## Out of scope (v1, per spec §11)

- Persistence of preview definitions across restarts.
- Embedded `tsnet` node.
- Generic response-body rewriter for frameworks without a URL-prefix option.
- Authenticated remote previews (scoped cookie) — only if auth-gated remote access is later wanted.

---

## Risk register (top 3, de-risked first)

1. **Base-path / HMR correctness** — mitigated by prefix injection (Vite `--base`, Next sidecar
   `basePath`), no body rewriting, supported-framework matrix. Primary supported = Vite.
2. **Iframe subresource / WS auth** — mitigated by mirroring the SPA's unauthenticated proxy
   posture (no cookie/bootstrap needed). Tradeoff documented in spec §7.
3. **Tailscale must target the proxied ocode path, not the raw dev port** — mitigated by serving the
   ocode port with path preserved (`tailscale serve --https=<id> https://localhost:<ocodePort>`).
