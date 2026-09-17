package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
)

func setPathValues(r *http.Request, values map[string]string) {
	for k, v := range values {
		r.SetPathValue(k, v)
	}
}

type fakeTestWorkspace struct {
	mu         sync.Mutex
	apiURL     string
	token      string
	disconnect func() error
	projects   []string
}

func (f *fakeTestWorkspace) APIURL() string { return f.apiURL }
func (f *fakeTestWorkspace) Token() string  { return f.token }
func (f *fakeTestWorkspace) Disconnect() error {
	if f.disconnect != nil {
		return f.disconnect()
	}
	return nil
}

func newTestProxyHandler(t *testing.T, host, path string, ws *fakeTestWorkspace) *Handler {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	store, err := projects.NewStoreAt(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatalf("projects store: %v", err)
	}
	h.projects = store
	if err := store.AddRemote(host, path); err != nil {
		t.Fatalf("add remote project: %v", err)
	}
	reg := newTestRegistry(func(target remote.Target, p string) (remoteHostWorkspace, error) {
		return ws, nil
	})
	h.remoteHosts = reg
	return h
}

func injectProxy(t *testing.T, reg *remoteHostRegistry, host string, ws *fakeTestWorkspace) {
	t.Helper()
	proxy, err := remote.NewAPIProxy(ws.apiURL, ws.token, nil)
	if err != nil {
		t.Fatalf("build proxy: %v", err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, ok := reg.byHost[host]
	if !ok {
		e = &remoteHostEntry{}
		e.connecting.L = &e.mu
		reg.byHost[host] = e
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.workspace = ws
	e.connected = true
	e.proxy = proxy
}

func fakeRemoteServer(t *testing.T, ws *fakeTestWorkspace) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Path != "" {
				ws.mu.Lock()
				ws.projects = append(ws.projects, body.Path)
				ws.mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"ok":true}`)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"remote-path":"%s","remote-method":"%s","remote-auth":"%s"}`,
			r.URL.Path, r.Method, r.Header.Get("Authorization"))
	})
	return httptest.NewServer(mux)
}

func TestHandleRemoteProxy_UnknownHost(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "tok"}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	var connectCalled atomic.Bool
	h.remoteHosts.connect = func(target remote.Target, path string) (remoteHostWorkspace, error) {
		connectCalled.Store(true)
		return nil, fmt.Errorf("should not be called")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/unknownhost/api/chat?token=local", nil)
	setPathValues(r, map[string]string{"host": "unknownhost", "rest": "chat"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if connectCalled.Load() {
		t.Error("connect should not have been called for unknown host")
	}
}

func TestHandleRemoteProxy_KnownHostUnknownPath(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "tok"}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/user@realhost/api/chat?token=local&project=/unregistered/path", nil)
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoteProxy_ValidPair(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote := fakeRemoteServer(t, ws)
	defer remote.Close()
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat?token=local&project=/home/user/project", nil)
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if auth := resp["remote-auth"]; auth != "Bearer remote-tok" {
		t.Errorf("expected Bearer remote-tok, got %q", auth)
	}
	// Assert the forwarded request hit /api/chat with the correct path.
	if got := resp["remote-path"]; got != "/api/chat" {
		t.Errorf("expected forwarded path /api/chat, got %q", got)
	}
}

func TestHandleRemoteProxy_TokenStripped(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}

	// Custom remote that echoes the raw query string so we can verify
	// token stripping.
	var mu sync.Mutex
	var lastRawQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"ok":true}`)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lastRawQuery = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"remote-path":"%s","remote-method":"%s","remote-auth":"%s"}`,
			r.URL.Path, r.Method, r.Header.Get("Authorization"))
	})
	remote := httptest.NewServer(mux)
	defer remote.Close()
	ws.apiURL = remote.URL

	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/user@realhost/api/sessions?token=local&foo=bar&project=/home/user/project", nil)
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "sessions"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	// Verify auth was injected.
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if auth := resp["remote-auth"]; auth != "Bearer remote-tok" {
		t.Errorf("expected Bearer remote-tok, got %q", auth)
	}
	// Verify the remote received the forwarded path (without local token).
	if got := resp["remote-path"]; got != "/api/sessions" {
		t.Errorf("expected forwarded path /api/sessions, got %q", got)
	}
	// Verify token= was stripped from the query string.
	mu.Lock()
	q := lastRawQuery
	mu.Unlock()
	if strings.Contains(q, "token=") {
		t.Errorf("token param should have been stripped, raw query: %q", q)
	}
	if !strings.Contains(q, "foo=bar") {
		t.Errorf("foo=bar should have survived, raw query: %q", q)
	}
}

func TestHandleRemoteProxy_FirstRegistrationPost(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote := fakeRemoteServer(t, ws)
	defer remote.Close()
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	// First request — should trigger POST /api/projects
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat?project=/home/user/project", nil)
	setPathValues(r1, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d; body: %s", w1.Code, w1.Body.String())
	}
	ws.mu.Lock()
	if len(ws.projects) != 1 {
		t.Errorf("expected 1 POST /api/projects, got %d", len(ws.projects))
	}
	ws.mu.Unlock()
	// Second request — should NOT trigger POST /api/projects
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat?project=/home/user/project", nil)
	setPathValues(r2, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d; body: %s", w2.Code, w2.Body.String())
	}
	ws.mu.Lock()
	if len(ws.projects) != 1 {
		t.Errorf("expected still 1 POST /api/projects after second request, got %d", len(ws.projects))
	}
	ws.mu.Unlock()
}

func TestHandleRemoteProxy_RemoteClosed(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote := fakeRemoteServer(t, ws)
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	remote.Close()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat?project=/home/user/project", nil)
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Stage string `json:"stage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Stage != "remote-register" {
		t.Errorf("expected stage remote-register, got %q", body.Stage)
	}
}

func TestHandleRemoteProxy_SSEStream(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	var sseStarted sync.WaitGroup
	sseStarted.Add(1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		sseStarted.Done()
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "data: {\"n\":%d}\n\n", i)
			flusher.Flush()
		}
	})
	remote := httptest.NewServer(mux)
	defer remote.Close()
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/user@realhost/api/events?token=local", nil)
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "events"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for i := 0; i < 3; i++ {
		expected := fmt.Sprintf(`{"n":%d}`, i)
		if !strings.Contains(body, expected) {
			t.Errorf("missing SSE event %q in body:\n%s", expected, body)
		}
	}
}

func TestHandleRemoteProxy_PercentEncodedHost(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote := fakeRemoteServer(t, ws)
	defer remote.Close()
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/user%40realhost/api/chat?token=local", nil)
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoteProxy_PathFromBody(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote := fakeRemoteServer(t, ws)
	defer remote.Close()
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	body := strings.NewReader(`{"project_path":"/home/user/project","prompt":"hi"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat", body)
	r.Header.Set("Content-Type", "application/json")
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoteProxy_HeaderFallback(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote := fakeRemoteServer(t, ws)
	defer remote.Close()
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/user@realhost/api/sessions?token=local", nil)
	r.Header.Set("X-Ocode-Project", "/home/user/project")
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "sessions"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoteProxy_NilRemoteHosts(t *testing.T) {
	h := NewHandler()
	h.projects = nil
	h.remoteHosts = nil
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/somehost/api/chat?token=local", nil)
	setPathValues(r, map[string]string{"host": "somehost", "rest": "chat"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil remoteHosts, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestProxyBuildFailureResolvesEntry is the regression guard for the stranded-
// waiter bug: when remote.NewAPIProxy fails in the connect success path, the
// entry must be marked failed, broadcast, and evicted — otherwise waiters block
// forever and the host is permanently poisoned.
func TestProxyBuildFailureResolvesEntry(t *testing.T) {
	reg := newRemoteHostRegistry(nil)
	var calls atomic.Int32
	reg.factory = func(target remote.Target, path string, sup *tool.ProcessSupervisor) (remoteHostWorkspaceConnector, error) {
		if calls.Add(1) == 1 {
			// An invalid API URL makes remote.NewAPIProxy's url.Parse fail.
			return &fakeConnector{fakeWorkspace: fakeWorkspace{apiURL: "://bad"}}, nil
		}
		return &fakeConnector{fakeWorkspace: fakeWorkspace{apiURL: "http://127.0.0.1:9", token: "t"}}, nil
	}

	done := make(chan error, 1)
	go func() {
		_, err := reg.workspaceFor("h", "/p")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a proxy-build error, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("workspaceFor stranded by proxy-build failure (entry never broadcast)")
	}

	// The evicted entry must let the next call retry and succeed.
	ws, err := reg.workspaceFor("h", "/p")
	if err != nil {
		t.Fatalf("retry after proxy-build failure: %v", err)
	}
	if ws.APIURL() != "http://127.0.0.1:9" {
		t.Fatalf("APIURL = %q, want http://127.0.0.1:9", ws.APIURL())
	}
}

// TestRemoteProxyRoutePatternPercentEncodedHost exercises the real ServeMux
// pattern (not manual SetPathValue) so the percent-decoding and the {rest...}
// shape the handler relies on are pinned end to end.
func TestRemoteProxyRoutePatternPercentEncodedHost(t *testing.T) {
	mux := http.NewServeMux()
	var gotHost, gotRest string
	mux.HandleFunc("/api/remote/{host}/api/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.PathValue("host")
		gotRest = r.PathValue("rest")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/remote/user%40host/api/sessions/s1?x=1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	if gotHost != "user@host" {
		t.Errorf("PathValue(host) = %q, want %q", gotHost, "user@host")
	}
	if gotRest != "sessions/s1" {
		t.Errorf("PathValue(rest) = %q, want %q", gotRest, "sessions/s1")
	}
}

// TestHandleRemoteProxy_RegistrationFailureThenRetry guards C1: a transient
// POST failure during registration must NOT permanently poison the (host,
// path) pair. The next request must retry the POST.
func TestHandleRemoteProxy_RegistrationFailureThenRetry(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}

	// Build a remote server with a swappable handler so we can simulate
	// a transient POST failure.
	var mu sync.Mutex
	failRegister := true
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			fail := failRegister
			mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintln(w, `{"error":"transient"}`)
				return
			}
			var body struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Path != "" {
				ws.mu.Lock()
				ws.projects = append(ws.projects, body.Path)
				ws.mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"ok":true}`)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"remote-path":"%s","remote-method":"%s","remote-auth":"%s"}`,
			r.URL.Path, r.Method, r.Header.Get("Authorization"))
	})
	remote := httptest.NewServer(mux)
	defer remote.Close()
	ws.apiURL = remote.URL

	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)

	// First request: POST /api/projects returns 500.
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat?project=/home/user/project", nil)
	setPathValues(r1, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w1, r1)
	if w1.Code != http.StatusBadGateway {
		t.Fatalf("first request: expected 502, got %d; body: %s", w1.Code, w1.Body.String())
	}
	var body1 struct {
		Stage string `json:"stage"`
	}
	json.Unmarshal(w1.Body.Bytes(), &body1)
	if body1.Stage != "remote-register" {
		t.Errorf("expected stage remote-register, got %q", body1.Stage)
	}

	// Enable registration.
	mu.Lock()
	failRegister = false
	mu.Unlock()

	// Second request: POST /api/projects now succeeds. Because the first
	// POST failed, isRegistered must still be false and the POST retried.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat?project=/home/user/project", nil)
	setPathValues(r2, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d; body: %s", w2.Code, w2.Body.String())
	}

	// The remote should have seen exactly 1 successful POST /api/projects.
	ws.mu.Lock()
	postCount := 0
	for _, p := range ws.projects {
		if p == "/home/user/project" {
			postCount++
		}
	}
	ws.mu.Unlock()
	if postCount != 1 {
		t.Errorf("expected 1 POST /api/projects, got %d", postCount)
	}
}

// TestHandleRemoteProxy_PathFromBody_RegistrationAssertion verifies I4: the
// path extracted from the JSON body actually triggers registration on the
// remote.
func TestHandleRemoteProxy_PathFromBody_RegistrationAssertion(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote := fakeRemoteServer(t, ws)
	defer remote.Close()
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	body := strings.NewReader(`{"project_path":"/home/user/project","prompt":"hi"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat", body)
	r.Header.Set("Content-Type", "application/json")
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	// Assert the fake remote received a POST /api/projects with the path.
	ws.mu.Lock()
	defer ws.mu.Unlock()
	found := false
	for _, p := range ws.projects {
		if p == "/home/user/project" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected /home/user/project in registered projects, got %v", ws.projects)
	}
}

// TestHandleRemoteProxy_HeaderFallback_RegistrationAssertion verifies I4:
// the path from X-Ocode-Project header triggers registration.
func TestHandleRemoteProxy_HeaderFallback_RegistrationAssertion(t *testing.T) {
	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote := fakeRemoteServer(t, ws)
	defer remote.Close()
	ws.apiURL = remote.URL
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/user@realhost/api/sessions?token=local", nil)
	r.Header.Set("X-Ocode-Project", "/home/user/project")
	setPathValues(r, map[string]string{"host": "user@realhost", "rest": "sessions"})
	h.HandleRemoteProxy(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	// Assert the fake remote received a POST /api/projects with the path.
	ws.mu.Lock()
	defer ws.mu.Unlock()
	found := false
	for _, p := range ws.projects {
		if p == "/home/user/project" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected /home/user/project in registered projects, got %v", ws.projects)
	}
}

// TestHandleRemoteProxy_ReconnectAfterDrop verifies I4: after a proxy error
// causes drop(host), the next request must trigger a fresh connect.
// This tests the drop + reconnect lifecycle without relying on the proxy's
// internal ErrorHandler timing.
func TestHandleRemoteProxy_ReconnectAfterDrop(t *testing.T) {
	ws1 := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok"}
	remote1 := fakeRemoteServer(t, ws1)
	defer remote1.Close()
	ws1.apiURL = remote1.URL

	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws1)
	injectProxy(t, h.remoteHosts, "user@realhost", ws1)

	// First request succeeds.
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat?project=/home/user/project", nil)
	setPathValues(r1, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d; body: %s", w1.Code, w1.Body.String())
	}

	// Verify the entry exists.
	h.remoteHosts.mu.Lock()
	_, exists := h.remoteHosts.byHost["user@realhost"]
	h.remoteHosts.mu.Unlock()
	if !exists {
		t.Fatal("expected entry to exist after first request")
	}

	// Drop the entry (simulating what onError does after a proxy error).
	h.remoteHosts.drop("user@realhost")

	// Verify the entry is gone.
	h.remoteHosts.mu.Lock()
	_, exists = h.remoteHosts.byHost["user@realhost"]
	h.remoteHosts.mu.Unlock()
	if exists {
		t.Fatal("expected entry to be gone after drop")
	}

	// Second remote — the fresh connect should target this.
	ws2 := &fakeTestWorkspace{apiURL: "http://unused", token: "remote-tok2"}
	remote2 := fakeRemoteServer(t, ws2)
	defer remote2.Close()
	ws2.apiURL = remote2.URL
	h.remoteHosts.connect = func(target remote.Target, path string) (remoteHostWorkspace, error) {
		return ws2, nil
	}

	// Next request: triggers a fresh connect.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/api/remote/user@realhost/api/chat?project=/home/user/project", nil)
	setPathValues(r2, map[string]string{"host": "user@realhost", "rest": "chat"})
	h.HandleRemoteProxy(w2, r2)
	if w2.Code != http.StatusOK {
		t.Errorf("request after drop: expected 200, got %d; body: %s", w2.Code, w2.Body.String())
	}

	// The new request should have hit the second remote.
	var resp map[string]string
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if auth := resp["remote-auth"]; auth != "Bearer remote-tok2" {
		t.Errorf("expected Bearer remote-tok2 (from second remote), got %q", auth)
	}
}

// TestHandleRemoteProxy_ConnectUsesSavedPathAndPort pins the I1/I2 plumbing:
// a path-less request (e.g. /api/events) must connect using the FIRST saved
// project's Path — so a cold-host launch never runs `cd ”` — and the saved
// project's RemotePort must reach the connect target (a non-default SSH port
// would otherwise silently dial 22).
func TestHandleRemoteProxy_ConnectUsesSavedPathAndPort(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	store, err := projects.NewStoreAt(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatalf("projects store: %v", err)
	}
	h.projects = store
	if err := store.AddRemote("user@host", "~/webapp", 2222); err != nil {
		t.Fatalf("add remote project: %v", err)
	}

	ws := &fakeTestWorkspace{apiURL: "http://unused", token: "tok"}
	type capture struct {
		port int
		path string
	}
	got := make(chan capture, 1)
	h.remoteHosts = newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		got <- capture{port: target.Port, path: path}
		return ws, nil
	})

	// A path-less request: no project_path body, no ?project=, no header.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/remote/user%40host/api/events?token=local", nil)
	setPathValues(r, map[string]string{"host": "user@host", "rest": "events"})
	h.HandleRemoteProxy(w, r)

	select {
	case c := <-got:
		if c.port != 2222 {
			t.Errorf("connect target port = %d, want 2222 (saved RemotePort)", c.port)
		}
		if c.path != "~/webapp" {
			t.Errorf("connect path = %q, want %q (first saved project path)", c.path, "~/webapp")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connect was not called for a path-less request")
	}
}
