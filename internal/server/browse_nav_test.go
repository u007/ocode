package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestEnableBrowseWiresPublisher proves the closure EnableBrowse installs on
// the browse server actually fans emissions onto the bus (not just the
// helper). A real document navigation whose DNS fails still emits a terminal
// event with an Error, so the SPA can never sit on a silent loading state.
func TestEnableBrowseWiresPublisher(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	bs := browse.New("", nil)
	s.EnableBrowse("http://127.0.0.1:0", bs)

	ch := s.handler.bus.Subscribe(nil)
	defer s.handler.bus.Unsubscribe(ch)

	grant := bs.MintGrant("tab:wire")
	r := httptest.NewRequest("GET", "/b/tab:wire/https/example.invalid/?__grant="+grant, nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	w := httptest.NewRecorder()
	// Redeem grant (302), grab cookie, then re-request as the SPA would.
	bs.Handler().ServeHTTP(w, r)
	if w.Code != 302 {
		t.Fatalf("grant redeem: got %d want 302", w.Code)
	}
	var cookie string
	for _, c := range w.Result().Cookies() {
		if c.Name == "ocode_browse" { // browse's unexported browseCookie const
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie after grant redeem")
	}
	r2 := httptest.NewRequest("GET", "/b/tab:wire/https/example.invalid/", nil)
	r2.Header.Set("Sec-Fetch-Dest", "document")
	r2.AddCookie(&http.Cookie{Name: "ocode_browse", Value: cookie})
	bs.Handler().ServeHTTP(httptest.NewRecorder(), r2)

	// Expect a loading event and a terminal event (with Error: DNS failure)
	// to arrive on the bus via the wired publisher.
	var events []browse.NavEvent
	for {
		select {
		case env := <-ch:
			if env.Event != "browse_nav" {
				continue
			}
			ev, ok := env.Data.(browse.NavEvent)
			if !ok {
				t.Fatalf("Data type = %T", env.Data)
			}
			events = append(events, ev)
			if ev.Error != "" {
				goto done
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out; events so far: %+v", events)
		}
	}
done:
	if len(events) < 2 {
		t.Fatalf("want >=2 events (loading + terminal), got %d: %+v", len(events), events)
	}
	if events[0].Status != 0 || events[0].Error != "" {
		t.Fatalf("first event should be loading (status 0, no error): %+v", events[0])
	}
	last := events[len(events)-1]
	if last.Error == "" || !strings.Contains(last.URL, "example.invalid") {
		t.Fatalf("terminal event should carry DNS error + upstream URL: %+v", last)
	}
}

// A subresource request (Sec-Fetch-Dest: image) must NOT produce a nav event;
// only a top-level document navigation does. This guards against flooding the
// address bar/status row with per-asset noise.
func TestBrowseNavOnlyForTopDocument(t *testing.T) {
	var events []browse.NavEvent
	bs := browse.New("", nil)
	bs.SetNavPublisher(func(_ string, ev browse.NavEvent) { events = append(events, ev) })

	grant := bs.MintGrant("tab:x")

	// Document navigation (DNS-fails upstream → loading + terminal, never zero).
	docReq := httptest.NewRequest("GET", "/b/tab:x/https/example.invalid/?__grant="+grant, nil)
	docReq.Header.Set("Sec-Fetch-Dest", "document")
	docW := httptest.NewRecorder()
	bs.Handler().ServeHTTP(docW, docReq) // 302 grant redeem; emits nothing
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
	if len(events) < 1 {
		t.Fatalf("document nav emitted %d nav events, want >=1", len(events))
	}
	afterDoc := len(events)

	// Subresource request with the same cookie: zero additional events.
	imgReq := httptest.NewRequest("GET", "/b/tab:x/https/example.invalid/pic.png", nil)
	imgReq.Header.Set("Sec-Fetch-Dest", "image")
	imgReq.AddCookie(&http.Cookie{Name: "ocode_browse", Value: cookie})
	bs.Handler().ServeHTTP(httptest.NewRecorder(), imgReq)
	if len(events) != afterDoc {
		t.Fatalf("subresource produced nav events: %d → %d", afterDoc, len(events))
	}
}
