package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

const key = "k-test"

func newTestBridge(t *testing.T, ocodeURL string) (*httptest.Server, *Hub) {
	t.Helper()
	h, hub, err := NewHandler(Config{OcodeURL: ocodeURL, OcodeToken: "ocode-secret", PaneKey: key, CallTimeout: 2 * time.Second,
		AllowedOrigins: []string{"https://pane.example"}})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s, hub
}

func rpc(t *testing.T, base, method string, params any, withKey bool) (*http.Response, map[string]json.RawMessage) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest("POST", base+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if withKey {
		req.Header.Set("X-Bridge-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestMCPListsContractToolsUnprefixedAndSorted(t *testing.T) {
	s, _ := newTestBridge(t, "http://127.0.0.1:1")
	resp, out := rpc(t, s.URL, "tools/list", map[string]any{}, true)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var res struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out["result"], &res); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tl := range res.Tools {
		if strings.HasPrefix(tl.Name, "excel_") {
			t.Errorf("tool %q is prefixed; ocode adds the server-name prefix itself", tl.Name)
		}
		if tl.Description == "" || tl.InputSchema["type"] != "object" {
			t.Errorf("tool %q lacks description or object schema", tl.Name)
		}
		names = append(names, tl.Name)
	}
	if !sort.StringsAreSorted(names) || len(names) < 12 {
		t.Fatalf("tools must be sorted (prompt-cache stability) and complete, got %v", names)
	}
}

func TestMCPRequiresKeyAndIgnoresNotifications(t *testing.T) {
	s, _ := newTestBridge(t, "http://127.0.0.1:1")
	if resp, _ := rpc(t, s.URL, "tools/list", nil, false); resp.StatusCode != 401 {
		t.Fatalf("want 401 without key, got %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("POST", s.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("X-Bridge-Key", key)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notification must get 202, got %d", resp.StatusCode)
	}
}

func TestMCPLoopbackOnly(t *testing.T) {
	h, _, _ := NewHandler(Config{OcodeURL: "http://127.0.0.1:1", PaneKey: key})
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.RemoteAddr = "10.1.2.3:5555"
	req.Header.Set("X-Bridge-Key", key)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("non-loopback MCP caller must be refused, got %d", rec.Code)
	}
}

// paneClient plays the Excel task pane: reads SSE calls, answers via POST /bridge/result.
func paneClient(t *testing.T, base, wb string, answer func(CallEvent) Result) (stop func()) {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/bridge/stream?workbook="+wb+"&instance=test-pane", nil)
	req.Header.Set("X-Bridge-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("stream: %v %v", err, resp)
	}
	ready := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(resp.Body)
		var ev string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				ev = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data := strings.TrimPrefix(line, "data: ")
				if ev == "hello" {
					close(ready)
				}
				if ev == "call" {
					var c CallEvent
					_ = json.Unmarshal([]byte(data), &c)
					r := answer(c)
					r.ID = c.ID
					b, _ := json.Marshal(r)
					rq, _ := http.NewRequest("POST", base+"/bridge/result?workbook="+wb, bytes.NewReader(b))
					rq.Header.Set("X-Bridge-Key", key)
					if rr, err := http.DefaultClient.Do(rq); err == nil {
						rr.Body.Close()
					}
				}
			}
		}
	}()
	<-ready
	return func() { resp.Body.Close() }
}

func TestMCPCallReachesPaneAndBack(t *testing.T) {
	s, _ := newTestBridge(t, "http://127.0.0.1:1")
	stop := paneClient(t, s.URL, "wb1", func(c CallEvent) Result {
		return Result{OK: true, Content: "pane ran " + c.Tool + " with " + string(c.Args)}
	})
	defer stop()
	_, out := rpc(t, s.URL, "tools/call", map[string]any{"name": "read_range", "arguments": map[string]any{"sheet": "S", "address": "A1"}}, true)
	var res struct {
		Content []struct{ Type, Text string } `json:"content"`
		IsError bool                          `json:"isError"`
	}
	if err := json.Unmarshal(out["result"], &res); err != nil {
		t.Fatal(err)
	}
	if res.IsError || len(res.Content) != 1 || !strings.HasPrefix(res.Content[0].Text, "pane ran read_range") {
		t.Fatalf("unexpected result %+v (raw %s)", res, out["result"])
	}
}

func TestMCPToolErrorsAreResultsNotProtocolErrors(t *testing.T) {
	s, _ := newTestBridge(t, "http://127.0.0.1:1")
	// no pane at all
	_, out := rpc(t, s.URL, "tools/call", map[string]any{"name": "read_range", "arguments": map[string]any{}}, true)
	if out["error"] != nil || !strings.Contains(string(out["result"]), `"isError":true`) {
		t.Fatalf("no-pane must be a tool error the model can read, got %s", out)
	}
	// pane reports a failure
	stop := paneClient(t, s.URL, "wb1", func(CallEvent) Result { return Result{OK: false, Error: "Range not found"} })
	defer stop()
	_, out = rpc(t, s.URL, "tools/call", map[string]any{"name": "read_range", "arguments": map[string]any{}}, true)
	if !strings.Contains(string(out["result"]), "Range not found") || !strings.Contains(string(out["result"]), `"isError":true`) {
		t.Fatalf("pane error not surfaced: %s", out["result"])
	}
	// unknown tool is a protocol error
	_, out = rpc(t, s.URL, "tools/call", map[string]any{"name": "format_disk"}, true)
	if out["error"] == nil {
		t.Fatalf("unknown tool must be a JSON-RPC error: %s", out)
	}
}

func TestProxyInjectsOcodeTokenAndStripsPaneCredentials(t *testing.T) {
	var gotAuth, gotKey, gotOrigin, gotPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotKey, gotOrigin, gotPath = r.Header.Get("Authorization"), r.Header.Get("X-Bridge-Key"), r.Header.Get("Origin"), r.URL.Path
		io.WriteString(w, `{"sessions":[]}`)
	}))
	defer up.Close()
	s, _ := newTestBridge(t, up.URL)

	if resp, _ := http.Get(s.URL + "/api/sessions"); resp.StatusCode != 401 {
		t.Fatalf("/api without the pane key must be 401, got %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", s.URL+"/api/sessions", nil)
	req.Header.Set("X-Bridge-Key", key)
	req.Header.Set("Authorization", "Bearer pane-supplied-should-be-dropped")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("proxy: %v %v", err, resp)
	}
	if gotAuth != "Bearer ocode-secret" || gotKey != "" || gotOrigin != "" || gotPath != "/api/sessions" {
		t.Fatalf("upstream saw auth=%q key=%q origin=%q path=%q", gotAuth, gotKey, gotOrigin, gotPath)
	}
}

func TestProxyStreamsSSEIncrementally(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: text\ndata: {\"n\":1}\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, "event: text\ndata: {\"n\":2}\n\n")
	}))
	defer up.Close()
	s, _ := newTestBridge(t, up.URL)
	req, _ := http.NewRequest("GET", s.URL+"/api/events", nil)
	req.Header.Set("X-Bridge-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got := make(chan string, 1)
	go func() { sc := bufio.NewScanner(resp.Body); sc.Scan(); sc.Scan(); got <- sc.Text() }()
	select {
	case line := <-got:
		if !strings.Contains(line, `"n":1`) {
			t.Fatalf("first event wrong: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first SSE event was buffered until the stream ended; the proxy must flush per write")
	}
	close(release)
}

func TestForeignBrowserOriginIsRefused(t *testing.T) {
	s, _ := newTestBridge(t, "http://127.0.0.1:1")
	for origin, want := range map[string]int{"https://evil.example": 403, "https://pane.example": 200} {
		req, _ := http.NewRequest("GET", s.URL+"/bridge/tools", nil)
		req.Header.Set("X-Bridge-Key", key)
		req.Header.Set("Origin", origin)
		resp, _ := http.DefaultClient.Do(req)
		if resp.StatusCode != want {
			t.Errorf("origin %s: want %d got %d", origin, want, resp.StatusCode)
		}
	}
}

func TestProxyNeverForwardsPaneAuthorizationEvenWithoutOcodeToken(t *testing.T) {
	var gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, "{}")
	}))
	defer up.Close()
	h, _, _ := NewHandler(Config{OcodeURL: up.URL, PaneKey: key}) // no OcodeToken: ocode serve runs unauthenticated
	s := httptest.NewServer(h)
	defer s.Close()
	req, _ := http.NewRequest("GET", s.URL+"/api/sessions", nil)
	req.Header.Set("X-Bridge-Key", key)
	req.Header.Set("Authorization", "Bearer something-the-pane-made-up")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", err, resp)
	}
	if gotAuth != "" {
		t.Fatalf("pane-supplied Authorization reached ocode serve: %q", gotAuth)
	}
}

func TestStateStoreRoundTripPersistsAndRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	h, _, err := NewHandler(Config{OcodeURL: "http://127.0.0.1:1", PaneKey: key, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	do := func(method, path, body string) *http.Response {
		req, _ := http.NewRequest(method, s.URL+path, strings.NewReader(body))
		req.Header.Set("X-Bridge-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if resp := do("GET", "/bridge/state/wb1/journal", ""); resp.StatusCode != 404 {
		t.Fatalf("missing state must be 404, got %d", resp.StatusCode)
	}
	if resp := do("PUT", "/bridge/state/wb1/journal", `{"steps":[1,2]}`); resp.StatusCode != 204 {
		t.Fatalf("put: %d", resp.StatusCode)
	}
	resp := do("GET", "/bridge/state/wb1/journal", "")
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(b) != `{"steps":[1,2]}` {
		t.Fatalf("get: %d %s", resp.StatusCode, b)
	}
	// It is on disk, so a new bridge process sees it.
	h2, _, _ := NewHandler(Config{OcodeURL: "http://127.0.0.1:1", PaneKey: key, DataDir: dir})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/bridge/state/wb1/journal", nil)
	req.Header.Set("X-Bridge-Key", key)
	h2.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("not persisted: %d", rec.Code)
	}
	for _, bad := range []string{"/bridge/state/..%2Fetc/journal", "/bridge/state/wb1/..", "/bridge/state/wb%201/k"} {
		if r := do("GET", bad, ""); r.StatusCode == 200 {
			t.Errorf("%s must not succeed", bad)
		}
	}
	if r := do("PUT", "/bridge/state/wb1/k", "not json"); r.StatusCode != 400 {
		t.Errorf("non-JSON state must be 400, got %d", r.StatusCode)
	}
}

func TestWorkspaceIsCreatedPerWorkbookAndTraversalRefused(t *testing.T) {
	root := t.TempDir()
	h, _, _ := NewHandler(Config{OcodeURL: "http://127.0.0.1:1", PaneKey: key, WorkspaceRoot: root})
	s := httptest.NewServer(h)
	defer s.Close()
	post := func(path string) (*http.Response, map[string]string) {
		req, _ := http.NewRequest("POST", s.URL+path, nil)
		req.Header.Set("X-Bridge-Key", key)
		resp, _ := http.DefaultClient.Do(req)
		var m map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp, m
	}
	resp, m := post("/bridge/workspace/wb-1")
	if resp.StatusCode != 200 || m["path"] != root+"/wb-1" {
		t.Fatalf("got %d %v", resp.StatusCode, m)
	}
	if again, m2 := post("/bridge/workspace/wb-1"); again.StatusCode != 200 || m2["path"] != m["path"] {
		t.Fatal("must be idempotent")
	}
	guide := root + "/wb-1/CLAUDE.md"
	b, err := os.ReadFile(guide)
	if err != nil || !strings.Contains(string(b), "excel_describe_workbook") {
		t.Fatalf("workspace guidance not seeded: %v", err)
	}
	if err := os.WriteFile(guide, []byte("user edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	post("/bridge/workspace/wb-1")
	if b, _ := os.ReadFile(guide); string(b) != "user edited" {
		t.Fatalf("the user's edits to CLAUDE.md were overwritten: %q", b)
	}
	for _, bad := range []string{"/bridge/workspace/..", "/bridge/workspace/a%2Fb", "/bridge/workspace/a%20b"} {
		if r, _ := post(bad); r.StatusCode == 200 {
			t.Errorf("%s must be refused", bad)
		}
	}
}
