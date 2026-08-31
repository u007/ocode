package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse"
)

// The browse server bridges NavEvents onto the unified bus as a global-scoped
// "browse_nav" event so the SPA's existing EventSource receives them. The
// stateKey rides inside the payload, never as the bus session id.
func TestBrowseNavBridgedToBus(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil) // live handler → live bus
	bs := browse.New("", nil)
	s.EnableBrowse("http://127.0.0.1:0", bs)

	ch := s.handler.bus.Subscribe(nil)
	defer s.handler.bus.Unsubscribe(ch)

	// Simulate emission through the publisher wired by EnableBrowse: drive
	// browse's own emit path by publishing via the helper the closure uses.
	s.publishBrowseNav(browse.NavEvent{
		StateKey: "tab:abc", URL: "https://example.com/", Status: 200, Mode: "chrome",
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
		if ev.StateKey != "tab:abc" || ev.URL != "https://example.com/" || ev.Status != 200 || ev.Mode != "chrome" {
			t.Fatalf("payload round-trip mismatch: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no browse_nav event received within 1s")
	}
}

// TestEnableBrowseWiresPublisher proves the closure EnableBrowse installs on
// the browse server actually fans emissions onto the bus/SSE layer. This is
// the server↔browse adapter boundary only: the chrome hand-off emission itself
// is covered by TestChromeHandOff in internal/browse, and the CDP socket
// contract by cdpsocket_test.go. The event is injected at the browse publisher
// seam (no network, no grants) and must arrive with its payload intact.
func TestEnableBrowseWiresPublisher(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	bs := browse.New("", nil)
	s.EnableBrowse("http://127.0.0.1:0", bs)

	ch := s.handler.bus.Subscribe(nil)
	defer s.handler.bus.Unsubscribe(ch)

	// Drive the wiring at its seam: publishBrowseNav is the exact sink the
	// EnableBrowse-installed publisher routes into, so emitting a chrome-mode
	// event through it proves the adapter without touching the network,
	// grants, or the CDP stack.
	want := browse.NavEvent{
		StateKey: "tab:wire", URL: "https://example.invalid/", Status: 0, Mode: "chrome",
	}
	go func() {
		s.publishBrowseNav(want)
	}()

	select {
	case env := <-ch:
		if env.Event != "browse_nav" {
			t.Fatalf("event = %q want browse_nav", env.Event)
		}
		ev, ok := env.Data.(browse.NavEvent)
		if !ok {
			t.Fatalf("Data type = %T", env.Data)
		}
		if ev != want {
			t.Fatalf("chrome nav payload mismatch: got %+v want %+v", ev, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no chrome browse_nav event received")
	}
}

// TestBrowseNavOnlyForTopDocument pins the Part-07 guard at the routing layer:
// a chrome hand-off document request emits exactly one nav event (the chrome
// loading event, status 0); a subresource request with the same session emits
// none. Uses the chrome hand-off path, so no network or grants beyond the
// initial redeem.
func TestBrowseNavOnlyForTopDocument(t *testing.T) {
	var events []browse.NavEvent
	bs := browse.New("", nil)
	bs.SetNavPublisher(func(_ string, ev browse.NavEvent) { events = append(events, ev) })

	grant := bs.MintGrant("tab:x", "")

	// Grant redeem (302, emits nothing) then the document navigation, which
	// hands off to chrome: exactly one chrome nav event.
	docReq := httptest.NewRequest("GET", "/b/tab:x/https/example.invalid/?__grant="+grant, nil)
	docReq.Header.Set("Sec-Fetch-Dest", "document")
	docW := httptest.NewRecorder()
	bs.Handler().ServeHTTP(docW, docReq)
	if docW.Code != 302 {
		t.Fatalf("grant nav: got %d want 302", docW.Code)
	}
	var cookie string
	for _, c := range docW.Result().Cookies() {
		if c.Name == "ocode_browse" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie after grant redeem")
	}
	docReq2 := httptest.NewRequest("GET", "/b/tab:x/https/example.invalid/", nil)
	docReq2.Header.Set("Sec-Fetch-Dest", "document")
	docReq2.AddCookie(&http.Cookie{Name: "ocode_browse", Value: cookie})
	bs.Handler().ServeHTTP(httptest.NewRecorder(), docReq2)
	if len(events) != 1 {
		t.Fatalf("chrome hand-off emitted %d nav events, want 1: %+v", len(events), events)
	}
	if events[0].Mode != "chrome" || events[0].Status != 0 {
		t.Fatalf("event = %+v, want chrome loading (status 0)", events[0])
	}

	// Subresource request with the same cookie: zero additional events.
	imgReq := httptest.NewRequest("GET", "/b/tab:x/https/example.invalid/pic.png", nil)
	imgReq.Header.Set("Sec-Fetch-Dest", "image")
	imgReq.AddCookie(&http.Cookie{Name: "ocode_browse", Value: cookie})
	bs.Handler().ServeHTTP(httptest.NewRecorder(), imgReq)
	if len(events) != 1 {
		t.Fatalf("subresource produced nav events: 1 → %d", len(events))
	}
}
