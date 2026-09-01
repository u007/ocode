# Headless Chrome (CDP) Browser Mode — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Plans in this repo are **high-level by policy** (files, functions, behaviours, verification — no code). TDD applies to every task: failing test → run → implement → run → commit.

**Goal:** Replace the HTML-rewriting external proxy of the embedded browser panel with a headless Chrome driven over CDP, keeping local mode (iframe reverse proxy) for private hosts.

**Architecture:** One headless Chrome per ocode server, spawned through the shared `tool.ProcessSupervisor`, spoken to over `--remote-debugging-pipe` by a small in-house CDP client. One browser context + target per `stateKey`. All Chrome egress goes through an in-process CONNECT proxy backed by the existing SSRF-safe dialer. The browse origin exposes a per-stateKey WebSocket that streams screencast frames + console/network telemetry and accepts input; navigation stays on the existing `browse_nav` SSE bus.

**Tech Stack:** Go 1.22+ (`net/http`, `gorilla/websocket`, `os/exec`), Chrome ≥ 112 (`--headless=new`), React 18 + TanStack Store + vitest.

**Spec:** `docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md` (rev 2). The 2026-08-30 spec still governs local mode, the security model, and the store contract.

## Global Constraints

- `--remote-debugging-pipe` only; never `--remote-debugging-port`.
- `--no-sandbox` forbidden.
- No `chromedp` / `rod` / any CDP dependency; no new Go module deps.
- Every process spawn goes through `tool.StartSupervised` (Part 01); no raw `exec.Command` in `internal/browse/cdp`.
- Loopback and all private ranges are **blocked** from Chrome contexts (no exception).
- Mode enum is `"local" | "chrome"`; `"proxied"` is removed everywhere.
- Nav events travel only on the `browse_nav` SSE bus (never on the CDP WebSocket).
- Windows: Chrome mode returns a "not supported on Windows yet" nav error; local mode untouched.
- Tests: Go `go test ./internal/browse/... ./internal/tool/ ./internal/server/ ./internal/config/`; web `cd web && bunx vitest run` + `bunx tsc --noEmit -p .`. Integration test gated on `OCODE_CHROME_PATH`.
- Commit after every task; never `git stash`; never touch another agent's in-flight files beyond what the task names.
- Docs updated in the same part that changes behaviour (CLAUDE.md "docs are source of truth").

## Execution order

Parts 01–03 are independent of each other and can run in parallel. 04 depends on 01+02+03. 05 depends on 04. 06 depends on 05 only for the wire format (can start against a fake WS). 07 last.

| Part | File | Deliverable | Depends on |
|---|---|---|---|
| 01 | `01-supervisor-and-config.md` | `tool.StartSupervised`, `ProcessKindBrowser`, server-owned supervisor, `browser` config section | — |
| 02 | `02-cdp-conn.md` | `cdp.Conn`: pipe framing, call/response, sessions, events | — |
| 03 | `03-egress-proxy.md` | `cdp.EgressProxy`: loopback CONNECT/forward proxy over `newSafeDialer(false)` | — |
| 04 | `04-chrome-manager.md` | `cdp.Manager`: binary discovery, launch, target-per-stateKey, screencast, input, telemetry, crash/idle lifecycle | 01, 02, 03 |
| 05 | `05-browse-ws-and-handoff.md` | `/b/{key}/__cdp` WebSocket, local→chrome hand-off, non-local 204 path, removal of external proxy code | 04 |
| 06 | `06-frontend.md` | store mode enum, `isPrivateHost`, `useCdpSocket`, `ChromeViewport`, `BrowserPanel`/`AddressBar` changes | 05 (wire format) |
| 07 | `07-integration-and-docs.md` | real-Chrome integration test, spec banners, `docs/index.md`, TODO.md entries | all |

## Shared interfaces (canonical names — every part repeats what it needs)

- `tool.StartSupervised(sup *ProcessSupervisor, cmd *exec.Cmd, reg ProcessRegistration) (ProcessRecord, error)`
- `tool.ProcessKindBrowser ProcessKind = "browser"`
- `server.Server.procSup *tool.ProcessSupervisor` (created in `server.New`, shut down in `Server.Shutdown`)
- `config.BrowserConfig{ ChromePath string; IdleTimeoutMinutes int }` on `OcodeConfig.Browser`
- `cdp.NewConn(r io.Reader, w io.Writer) *Conn`; `(*Conn).Call(ctx, sessionID, method string, params any, result any) error`; `(*Conn).Subscribe(sessionID, method string) (<-chan json.RawMessage, func())`; `(*Conn).Close() error`
- `cdp.NewEgressProxy(dialer *net.Dialer) (*EgressProxy, error)`; `(*EgressProxy).ProxyServerURL() string` (with credential user-info); `(*EgressProxy).Close() error`
- `cdp.NewManager(opts ManagerOptions) *Manager` where `ManagerOptions{ ChromePath string; IdleTimeout time.Duration; Supervisor *tool.ProcessSupervisor; Dialer *net.Dialer; EmitNav func(cdp.NavEvent); Log *log.Logger }` — `cdp.NavEvent{StateKey, URL string; Status int; Error string}` is cdp-local (Part 05 maps it to `browse.NavEvent{Mode:"chrome"}`; `cdp` never imports `browse`)
- `(*Manager).Attach(ctx, stateKey string, sink FrameSink) (*Target, error)`; `(*Manager).Revoke(stateKey string)`; `(*Manager).Close(ctx) error`
- `cdp.FrameSink` interface: `Frame(width, height uint32, jpeg []byte)`, `Console(ev ConsoleEvent)`, `Network(ev NetworkEvent)`, `Error(msg string)`
- `(*Target).Navigate(ctx, url string) error`, `Back`, `Forward`, `Reload`, `Resize(w, h int, dpr float64)`, `Mouse(MouseEvent)`, `Key(KeyEvent)`, `Detach()`
- Browse WS endpoint: `GET /b/{stateKey}/__cdp?__grant=<token>`; server→client: binary `frame` (`[u32 BE width][u32 BE height]` + JPEG), JSON `console` / `network` / `error`; client→server JSON `nav` / `back` / `forward` / `reload` / `resize` / `mouse` / `key` (fields per spec § Transport).
- Web: `browserStore.isPrivateHost(host: string): boolean`; `useCdpSocket(stateKey, browseBase, enabled) → { send(msg), status, error, onFrame(cb) }`; `<ChromeViewport stateKey browseBase url />`.
