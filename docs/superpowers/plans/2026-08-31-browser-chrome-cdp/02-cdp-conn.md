# Part 02 — CDP pipe client (`internal/browse/cdp/conn.go`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox steps. High-level by policy (no code in plans). TDD per task; commit per task.

**Goal:** A minimal, dependency-free Chrome DevTools Protocol client over the `--remote-debugging-pipe` transport: NUL-terminated JSON messages, request/response by `id`, per-session multiplexing, event subscriptions.

**Spec:** `docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md` § CDP client.

**Global constraints:** no `chromedp`/`rod`/any CDP dependency; package is `internal/browse/cdp`; tests via `go test ./internal/browse/cdp/`.

## Protocol facts the implementer must know

- Pipe transport: Chrome reads commands from fd 3 and writes responses/events to fd 4. Each message is a UTF-8 JSON object terminated by a single `\0` byte (no newline framing). Messages may be large (screencast frames are base64 JPEG inside JSON — several hundred KB); the reader must not use a line-length-capped scanner.
- Command: `{"id": n, "method": "Domain.method", "params": {...}, "sessionId": "..."}`. `sessionId` is omitted for browser-level commands (`Target.*`, `Browser.*`).
- Response: `{"id": n, "result": {...}, "sessionId"?: "..."}` or `{"id": n, "error": {"code": int, "message": string}, "sessionId"?}`.
- Event: `{"method": "Domain.event", "params": {...}, "sessionId"?: "..."}` — no `id`.
- Session attach: `Target.attachToTarget {targetId, flatten: true}` → `{sessionId}`; subsequent page commands carry that `sessionId`. `Target.setAutoAttach {autoAttach:true, waitForDebuggerOnStart:true, flatten:true}` produces `Target.attachedToTarget` events carrying new `sessionId`s for OOPIFs/workers, which must be resumed with `Runtime.runIfWaitingForDebugger` on that session.
- `Target.detachedFromTarget {sessionId}` means the session is gone; pending calls on it must fail.

## Interfaces produced (used by Part 04)

- `NewConn(r io.Reader, w io.Writer) *Conn` — starts the reader goroutine.
- `(*Conn).Call(ctx context.Context, sessionID, method string, params any, result any) error` — marshals `params` (nil allowed), blocks until the response, `ctx` cancel, or connection close; decodes `result` into `result` when non-nil; returns `*CDPError{Code, Message, Method}` for protocol errors, `ErrConnClosed` after close.
- `(*Conn).Subscribe(sessionID, method string) (events <-chan json.RawMessage, cancel func())` — buffered channel (size 64); events for `(sessionID, method)` are delivered as the raw `params` object; when the buffer is full the **oldest** event is dropped and a counter incremented (never block the reader). `sessionID == ""` subscribes to browser-level events. `method == "*"` is not supported (YAGNI).
- `(*Conn).Close() error` — closes writer, fails all pending calls with `ErrConnClosed`, closes all subscription channels, idempotent.
- `(*Conn).Done() <-chan struct{}` — closed when the reader exits (pipe EOF = Chrome died).

---

### Task 1: Framing — reader/writer over `\0`-terminated JSON

**Files:**
- Create: `internal/browse/cdp/conn.go`
- Test: `internal/browse/cdp/conn_test.go`

Test fixture used by every test in this part: a **fake peer** built from two `io.Pipe`s — the test holds the "Chrome" ends, reads commands as they arrive (splitting on `\0`), and writes back responses/events. Provide a tiny helper in the test file that decodes the next command and returns its `id`, `method`, `sessionId`, `params`.

- [ ] Step 1: Write failing tests: (a) `Call` writes exactly one message terminated by `\0` with sequential `id`s starting at 1; (b) a 2 MB response is read intact (no scanner cap); (c) `Done()` closes on peer EOF and an in-flight `Call` returns `ErrConnClosed`.
- [ ] Step 2: Run → fails (package missing).
- [ ] Step 3: Implement: `bufio.Reader` + `ReadBytes('\0')` (grows as needed), a writer mutex, an `id` counter, a `pending map[int]chan response`. Reader goroutine dispatches by presence of `id`.
- [ ] Step 4: Run → pass. `go vet`.
- [ ] Step 5: Commit `feat(browse/cdp): pipe framing + Call`.

---

### Task 2: Errors, context cancellation, close semantics

**Files:** same.

- [ ] Step 1: Write failing tests: (a) peer answers `{"id":1,"error":{"code":-32000,"message":"No target"}}` → `Call` returns `*CDPError` with those fields and `Method` set; (b) `ctx` cancelled before reply → `Call` returns `ctx.Err()` and the late reply is discarded without panic; (c) `Close()` twice is safe; after `Close`, `Call` returns `ErrConnClosed` immediately.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement; make sure the pending map entry is removed on cancel so the map does not leak.
- [ ] Step 4: Run → pass.
- [ ] Step 5: Commit `feat(browse/cdp): CDP errors, ctx cancel, close`.

---

### Task 3: Sessions and event subscriptions

**Files:** same.

- [ ] Step 1: Write failing tests: (a) `Call` with `sessionID="S1"` includes `"sessionId":"S1"` in the wire message; (b) `Subscribe("S1","Page.screencastFrame")` receives only events with that sessionId + method, and a browser-level subscription (`""`, `Target.attachedToTarget`) receives only sessionId-less events; (c) 70 events pushed with no reader → channel holds the newest 64, `Dropped()` (or an exported counter on the subscription) reports 6; (d) `cancel()` removes the subscription and closes the channel; `Close()` closes all remaining channels.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement subscription registry keyed by `(sessionID, method)` → slice of subscribers; dispatch under a read lock; drop-oldest via non-blocking send after draining one when full.
- [ ] Step 4: Run with `-race` → pass.
- [ ] Step 5: Commit `feat(browse/cdp): sessions + event subscriptions`.

## Verification for the part

- `go test -race ./internal/browse/cdp/` green.
- `conn.go` ≤ ~250 lines; if it grows past that, split subscriptions into `events.go`.
