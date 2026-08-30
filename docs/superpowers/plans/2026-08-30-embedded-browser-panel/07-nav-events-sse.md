# Part 07 — Server-Authoritative Nav/Status Events over SSE

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ Security model — "Address bar is server-authoritative"; § Backend proxy — nav events).

**Goal:** Bridge the browse server's `NavEvent`s onto the existing unified event bus so the SPA's already-open `EventSource` (`GET /api/events`) receives them. The address bar and status row render **only** these server events — never page-reported URLs — which is the URL-spoofing defense from the spec. This part wires the publisher, guarantees a minimal one-loading + one-terminal event per top-level navigation (never per subresource), and locks the `browse_nav` bus-event contract that Part 08 consumes.

## Verified codebase facts (do not re-derive)

- The unified bus is `*EventBus` at `internal/server/event_bus.go`, held on the **Handler**: `Handler.bus` (`internal/server/handler.go:48`). From a `*Server` value reach it as **`s.handler.bus`**.
- `func (b *EventBus) Publish(event, project, sessionID string, data any)` (`event_bus.go:129`) fans every envelope out to **all** subscribers non-blockingly; there is no server-side project filtering inside `Publish`, so a global event with an empty `project` reaches every open SSE client.
- `Envelope` fields: `Event, Project, SessionID string; Seq uint64; Data any` (`event_bus.go`).
- `func (b *EventBus) Subscribe(projects []string) chan Envelope` (`event_bus.go:79`) and `func (b *EventBus) Unsubscribe(ch chan Envelope)` (`event_bus.go:98`) — the test uses these directly.
- The SSE endpoint is `GET /api/events` → `handleEvents` (`server.go:133`), already streaming every bus envelope to the SPA.
- `sessionScopedEvents` (`event_bus.go`) gates events that MUST carry a session id and logs a loud error otherwise. `browse_nav` is **project/global-scoped** — it must **NOT** be added to that map. The `stateKey` travels inside the payload, not as the bus `sessionID`.

## Interfaces

- **Consumes** (from Part 01): `browse.NavEvent` (JSON tags `state_key,url,status,mode,error`) and `(*browse.Server).SetNavPublisher(func(stateKey string, ev NavEvent))`, invoked internally by `emitNav`.
- **Produces:** the `browse_nav` bus-event contract — `Envelope{Event:"browse_nav", Project:"", SessionID:"", Data: browse.NavEvent}`. Part 08's SSE handler routes on `event === "browse_nav"` by `data.state_key` into `browserStore.applyNavEvent`.

**Files:**
- Modify: `internal/server/server.go` — inside `EnableBrowse` (added in Part 01), set the nav publisher.
- Create: `internal/server/browse_nav_test.go`.

**Prerequisite note on emission sites (Parts 03 & 06):** each top-level document navigation must emit exactly two events and no subresource events:
1. a **loading** event (`Status: 0`, `Error: ""`) published at the *start* of `handleExternal` / `handleLocal`, **before** the upstream round-trip, right after the target is parsed and confirmed to be the top document (i.e. the request that produced the browse cookie's clean navigation, not an asset). Distinguish the top document from subresources by the request's `Sec-Fetch-Dest` header: emit only when it is `document`/`iframe` or absent (direct navigation); never for `image`/`script`/`style`/`font`/`fetch`. Add this guard where those handlers call `s.emitNav`.
2. a **terminal** event after the response status is known — `Status:` the real upstream status on success, or `Status: 0` with `Error:` set on transport failure (DNS, refused, timeout, SSRF-blocked).

If Parts 03/06 as implemented emit only the terminal event, add the loading `emitNav` call at the documented spot. Keep `Mode` = `"proxied"` in `handleExternal`, `"local"` in `handleLocal`.

---

- [ ] **Step 1: Write the failing publisher-bridge test**

Create `internal/server/browse_nav_test.go`:

```go
package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/..." // remove — no wails import needed
)

// (Delete the stray import above; the file needs only encoding/json, testing,
// time, and the browse package.)
```

Replace with the real file:

```go
package server

import (
	"testing"
	"time"

	"ocode/internal/browse" // adjust to the module's real import path (see go.mod module line)
)

// The browse server bridges NavEvents onto the unified bus as a global-scoped
// "browse_nav" event so the SPA's existing EventSource receives them. The
// stateKey rides inside the payload, never as the bus session id.
func TestBrowseNavBridgedToBus(t *testing.T) {
	s := newTestServer(t) // helper that constructs *Server with a live bus; see note below
	bs := browse.New("", nil)
	s.EnableBrowse("http://127.0.0.1:0", bs)

	ch := s.handler.bus.Subscribe(nil)
	defer s.handler.bus.Unsubscribe(ch)

	// Drive the publisher exactly as browse.Server.emitNav would.
	bs.SetNavPublisher(func(stateKey string, ev browse.NavEvent) {
		// no-op override guard: the publisher set by EnableBrowse is what we test,
		// so re-fetch it below instead of overriding. (See Step 3.)
	})

	// Simulate emission through the wired publisher.
	s.publishBrowseNav(browse.NavEvent{
		StateKey: "tab:abc", URL: "https://example.com/", Status: 200, Mode: "proxied",
	})

	select {
	case env := <-ch:
		if env.Event != "browse_nav" {
			t.Fatalf("event = %q, want browse_nav", env.Event)
		}
		if env.SessionID != "" {
			t.Fatalf("SessionID = %q, want empty (global-scoped)", env.SessionID)
		}
		if env.Project != "" {
			t.Fatalf("Project = %q, want empty", env.Project)
		}
		ev, ok := env.Data.(browse.NavEvent)
		if !ok {
			t.Fatalf("Data type = %T, want browse.NavEvent", env.Data)
		}
		if ev.StateKey != "tab:abc" || ev.URL != "https://example.com/" || ev.Status != 200 || ev.Mode != "proxied" {
			t.Fatalf("payload round-trip mismatch: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no browse_nav event received within 1s")
	}
}
```

**Test-helper note:** if `newTestServer` does not already exist in the `server` package tests, add a minimal one that mirrors how other `internal/server` tests build a `*Server` (grep existing `_test.go` for the constructor pattern — several call `New(...)` directly). It must return a `*Server` whose `handler.bus` is live (it is, because `NewEventBus()` runs in the handler constructor at `handler.go:281`). The `publishBrowseNav` helper is introduced in Step 3 so the test can drive emission deterministically without a real HTTP round-trip.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestBrowseNavBridgedToBus`
Expected: FAIL — `s.publishBrowseNav` undefined (and possibly `EnableBrowse` if Part 01 is not yet merged; this part assumes Part 01 landed).

- [ ] **Step 3: Wire the publisher in `EnableBrowse` and add the emit helper**

In `internal/server/server.go`, extend the `EnableBrowse` body added in Part 01 so it installs the nav publisher, and factor the bus publish into a tiny helper so tests and the closure share one path:

```go
// publishBrowseNav fans a browse NavEvent onto the unified bus as a
// project/global-scoped event. The stateKey rides inside ev.Data — it is NOT
// the bus session id, so "browse_nav" must never be added to
// sessionScopedEvents. The SPA's existing EventSource picks it up and routes
// by ev.StateKey (see web browserStore.applyNavEvent, Part 08).
func (s *Server) publishBrowseNav(ev browse.NavEvent) {
	s.handler.bus.Publish("browse_nav", "", "", ev)
}
```

And inside `EnableBrowse`, after recording `s.browse = bs` / `s.browseBase = baseURL`:

```go
	bs.SetNavPublisher(func(_ string, ev browse.NavEvent) {
		s.publishBrowseNav(ev)
	})
```

The publisher's first parameter (`stateKey`) is redundant with `ev.StateKey`; ignore it with `_` and treat `ev.StateKey` as the single source of truth so the SPA and the bus never disagree.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/server/ -run TestBrowseNavBridgedToBus`
Expected: PASS.

- [ ] **Step 5: Add the loading-event guard test (top document only, no subresources)**

Still in `internal/server/browse_nav_test.go`, assert the subresource guard contract at the unit level by exercising the browse server directly (this pins the Parts 03/06 obligation so a regression there is caught here):

```go
import (
	"net/http/httptest"
	// ...existing imports
)

// A subresource request (Sec-Fetch-Dest: image) must NOT produce a nav event;
// only a top-level document navigation does. This guards against flooding the
// address bar/status row with per-asset noise.
func TestBrowseNavOnlyForTopDocument(t *testing.T) {
	var events []browse.NavEvent
	bs := browse.New("", nil)
	bs.SetNavPublisher(func(_ string, ev browse.NavEvent) { events = append(events, ev) })

	// Redeem a grant so the request is authenticated (mirrors Part 01 auth flow).
	grant := bs.MintGrant("tab:x")

	// Document navigation → expect at least one nav event.
	docReq := httptest.NewRequest("GET", "/b/tab:x/https/example.invalid/?__grant="+grant, nil)
	docReq.Header.Set("Sec-Fetch-Dest", "document")
	// (example.invalid resolves nowhere → terminal event carries an Error; that
	// is still exactly one loading + one terminal event, never zero.)
	_ = docReq // drive via bs.Handler().ServeHTTP with a recorder; see Step 6 note

	// Subresource navigation → expect zero nav events.
	// Build a fresh authenticated request whose Sec-Fetch-Dest is "image".
	// Assert len(events) is unchanged across the subresource call.
}
```

Flesh this out against the real handler once Parts 03/06 exist: serve `docReq` through `bs.Handler()`, record `len(events)` (expect ≥1), then serve an authenticated `image`-dest request and assert `len(events)` did not grow. If Parts 03/06 are not yet implemented when this part is executed, mark this step's assertions with a `t.Skip("pending Parts 03/06 emission sites")` and leave the test body in place so it activates automatically — and add a `TODO.md` entry: "browse nav subresource-guard test skipped until Parts 03/06 land".

- [ ] **Step 6: Run to verify (pass or documented skip)**

Run: `go test ./internal/server/ -run TestBrowseNav`
Expected: `TestBrowseNavBridgedToBus` PASS; `TestBrowseNavOnlyForTopDocument` PASS once 03/06 are in, otherwise SKIP with the recorded TODO.

- [ ] **Step 7: Document the frontend contract inline**

Add a comment above `publishBrowseNav` (already drafted in Step 3) and confirm the SSE payload shape matches what Part 08 expects: `event: "browse_nav"`, `data: {state_key, url, status, mode, error?}`. No code change if Step 3's comment already states it; this step is a read-through to ensure the JSON tag names on `browse.NavEvent` (Part 01: `state_key`, `url`, `status`, `mode`, `error`) exactly match the fields Part 08's `applyNavEvent` destructures. If they drift, fix the tags in Part 01's `NavEvent`, not here.

- [ ] **Step 8: Full-package test + build**

Run: `go build ./... && go test ./internal/server/ ./internal/browse/`
Expected: build OK, tests PASS (with the documented skip if 03/06 pending).

- [ ] **Step 9: Commit**

```bash
git add internal/server/server.go internal/server/browse_nav_test.go
git commit -m "feat(browse): bridge nav/status events onto the SSE bus (server-authoritative address bar)"
```
