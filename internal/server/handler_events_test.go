package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHandleEventsStreamsEnvelopes: a connected /api/events client receives
// published envelopes as SSE frames carrying the full envelope JSON. The
// handler runs against a real HTTP server so frames arrive over a blocking
// buffered reader (reading httptest's recorder body concurrently with the
// handler writing to it is a data race and sees an empty stream).
func TestHandleEventsStreamsEnvelopes(t *testing.T) {
	h := NewHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleEvents(w, r)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/events?projects=%2Fproj")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	// Wait until the handler has subscribed, then publish.
	deadline := time.Now().Add(5 * time.Second)
	for len(h.bus.ViewedProjects()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("handler never registered the project view")
		}
		time.Sleep(5 * time.Millisecond)
	}

	h.bus.Publish("turn_done", "/proj", "ses_1", map[string]string{"model": "m"})
	h.bus.Publish("status", "/proj", "", map[string]string{"model": "m"})

	var got []map[string]any
	for len(got) < 2 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v (got %d envelopes so far)", err, len(got))
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
			t.Fatalf("bad envelope JSON: %v", err)
		}
		got = append(got, m)
	}

	if len(got) != 2 {
		t.Fatalf("received %d envelopes, want 2", len(got))
	}
	if got[0]["event"] != "turn_done" || got[0]["session_id"] != "ses_1" || got[0]["project"] != "/proj" {
		t.Fatalf("first envelope = %v", got[0])
	}
	if _, ok := got[0]["seq"]; !ok {
		t.Fatalf("envelope missing seq: %v", got[0])
	}
	if got[1]["event"] != "status" {
		t.Fatalf("second envelope = %v", got[1])
	}
}

// TestHandleEventsRegistersProjectViews: the projects query param drives the
// bus's subscriber-aware scope.
func TestHandleEventsRegistersProjectViews(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/events?projects=%2Fa,%2Fb&projects=%2Fc", nil)
	if got := parseEventsProjects(req); len(got) != 3 || got[0] != "/a" || got[2] != "/c" {
		t.Fatalf("parseEventsProjects = %v", got)
	}
}

// TestHandleEventsUnsubscribesOnDisconnect: when the client disconnects, the
// bus subscriber is removed (no goroutine leak; publishes no longer reach it).
func TestHandleEventsUnsubscribesOnDisconnect(t *testing.T) {
	h := NewHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/events", nil)
	ctx, cancel := contextWithCancel(req)
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		h.HandleEvents(rec, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	if len(h.bus.ViewedProjects()) != 0 {
		t.Fatalf("expected no projects, got %v", h.bus.ViewedProjects())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not exit on disconnect")
	}
}

// contextWithCancel returns a cancellable context for the request (httptest
// requests come with a Background context that never cancels).
func contextWithCancel(req *http.Request) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
