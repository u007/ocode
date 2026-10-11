package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/remote"
)

// openEventsStream connects to a test server running HandleEvents and returns
// a function yielding the next envelope on the stream (comment frames skipped).
func openEventsStream(t *testing.T, h *Handler, query string) func() map[string]any {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.HandleEvents))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events?"+query, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	reader := bufio.NewReader(resp.Body)
	return func() map[string]any {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read SSE frame: %v", err)
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
				t.Fatalf("bad envelope JSON %q: %v", line, err)
			}
			return m
		}
	}
}

// nextRelayed returns the next envelope that came from a remote host, skipping
// the local bus's own emitter frames (spending, runs, …) on the shared stream.
func nextRelayed(next func() map[string]any) map[string]any {
	for {
		if env := next(); env["host"] != nil {
			return env
		}
	}
}

// TestHandleEventsRelaysRemoteHost: ?hosts= makes the single local stream
// carry the remote host's envelopes, tagged with the host, after a
// host_stream open marker — so a browser needs no second pinned connection.
func TestHandleEventsRelaysRemoteHost(t *testing.T) {
	var gotAuth, gotProjects atomic.Value
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		gotProjects.Store(r.URL.Query().Get("projects"))
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": connected\n\n")
		fmt.Fprint(w, "event: envelope\ndata: {\"event\":\"turn_done\",\"session_id\":\"ses_r\",\"seq\":7,\"data\":{\"model\":\"m\"}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer remoteSrv.Close()

	ws := &fakeTestWorkspace{apiURL: remoteSrv.URL, token: "remote-tok"}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	next := openEventsStream(t, h, "projects=%2Fhome%2Fuser%2Fproject&hosts=user%40realhost")

	open := nextRelayed(next)
	if open["event"] != hostStreamEvent || open["host"] != "user@realhost" {
		t.Fatalf("first relayed frame = %v, want a %s marker for the host", open, hostStreamEvent)
	}
	env := nextRelayed(next)
	if env["event"] != "turn_done" || env["host"] != "user@realhost" || env["session_id"] != "ses_r" || env["seq"] != float64(7) {
		t.Fatalf("relayed envelope = %v, want turn_done tagged with the host and its own seq", env)
	}
	if gotAuth.Load() != "Bearer remote-tok" {
		t.Errorf("upstream Authorization = %q, want the remote bearer token", gotAuth.Load())
	}
	if gotProjects.Load() != "/home/user/project" {
		t.Errorf("upstream projects = %q, want the client's project list", gotProjects.Load())
	}
}

// TestHandleEventsRelayReconnects: when the upstream stream ends, the relay
// reopens it and emits a second host_stream marker (the client's reconcile cue).
func TestHandleEventsRelayReconnects(t *testing.T) {
	var conns atomic.Int32
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": connected\n\n")
		w.(http.Flusher).Flush()
		if conns.Add(1) == 1 {
			return // first stream ends at once
		}
		<-r.Context().Done()
	}))
	defer remoteSrv.Close()

	ws := &fakeTestWorkspace{apiURL: remoteSrv.URL, token: "remote-tok"}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	next := openEventsStream(t, h, "hosts=user%40realhost")

	for i := 0; i < 2; i++ {
		if env := nextRelayed(next); env["event"] != hostStreamEvent {
			t.Fatalf("frame %d = %v, want a %s marker", i, env, hostStreamEvent)
		}
	}
	if n := conns.Load(); n != 2 {
		t.Errorf("upstream connections = %d, want 2", n)
	}
}

// TestHandleEventsRelayReconnectsSilentUpstream: an upstream that stops
// sending bytes (no keepalive) is torn down and reopened. The browser cannot
// notice this itself — the local stream's pings keep its liveness timer armed.
func TestHandleEventsRelayReconnectsSilentUpstream(t *testing.T) {
	var conns atomic.Int32
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conns.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": connected\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // silent forever
	}))
	defer remoteSrv.Close()

	ws := &fakeTestWorkspace{apiURL: remoteSrv.URL, token: "remote-tok"}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	h.remoteHosts.eventsLiveness = 50 * time.Millisecond
	next := openEventsStream(t, h, "hosts=user%40realhost")

	nextRelayed(next)
	nextRelayed(next) // second marker only arrives if the silent stream was reopened
	if n := conns.Load(); n < 2 {
		t.Errorf("upstream connections = %d, want >= 2", n)
	}
}

// TestHandleEventsDoesNotRelayUnsavedHost: a host with no saved project is
// never connected to — the same admission as the path-less remote proxy.
func TestHandleEventsDoesNotRelayUnsavedHost(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	var connects atomic.Int32
	h.remoteHosts.connect = func(remote.Target, string) (remoteHostWorkspace, error) {
		connects.Add(1)
		return ws, nil
	}
	next := openEventsStream(t, h, "hosts=evil%40elsewhere")

	// The local bus still works; nothing was relayed or connected.
	deadline := time.Now().Add(5 * time.Second)
	for h.bus.SubscriberCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("handler never subscribed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.bus.Publish("status", "", "", map[string]string{"model": "m"})
	for env := next(); env["event"] != "status"; env = next() {
		if env["host"] != nil {
			t.Fatalf("relayed a frame for an unsaved host: %v", env)
		}
	}
	if n := connects.Load(); n != 0 {
		t.Errorf("remote connects = %d, want 0", n)
	}
}

// TestHandleRemoteProxy_CancelledRequestKeepsConnection: a browser that
// cancels a proxied request mid-registration (reload, tab close) must not
// tear the host's tunnel down under every other in-flight request.
func TestHandleRemoteProxy_CancelledRequestKeepsConnection(t *testing.T) {
	release := make(chan struct{})
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer remoteSrv.Close()
	defer close(release)

	ws := &fakeTestWorkspace{apiURL: remoteSrv.URL, token: "remote-tok"}
	var disconnects atomic.Int32
	ws.disconnect = func() error { disconnects.Add(1); return nil }
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)

	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/api/remote/user@realhost/api/sessions?project=/home/user/project", nil).WithContext(ctx)
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "sessions"})
	time.AfterFunc(50*time.Millisecond, cancel)
	h.HandleRemoteProxy(httptest.NewRecorder(), r)

	if n := disconnects.Load(); n != 0 {
		t.Errorf("workspace disconnected %d times after a caller-cancelled request, want 0", n)
	}
	if _, ok := h.remoteHosts.proxyFor("user@realhost", 0); !ok {
		t.Error("host connection was dropped by a caller-cancelled request")
	}
}
