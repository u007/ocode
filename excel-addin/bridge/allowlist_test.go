package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recordingUpstream stands in for ocode serve and remembers what it was sent.
type recordingUpstream struct {
	mu    sync.Mutex
	hits  int
	last  string // method + " " + path
	auth  string
	query string
}

func (u *recordingUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.hits++
	u.last = r.Method + " " + r.URL.Path
	u.auth = r.Header.Get("Authorization")
	u.query = r.URL.RawQuery
	u.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

func (u *recordingUpstream) snapshot() (int, string, string, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hits, u.last, u.auth, u.query
}

// noRedirect keeps the bridge's own status codes visible instead of following them.
var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func doAPI(t *testing.T, base, method, rawPath string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, base+rawPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Bridge-Key", key)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAPIAllowlistForwardsEveryPaneRouteWithOcodeToken(t *testing.T) {
	up := &recordingUpstream{}
	upSrv := httptest.NewServer(up)
	defer upSrv.Close()
	s, _ := newTestBridge(t, upSrv.URL)

	routes := []struct{ method, path string }{
		{"GET", "/api/events"},
		{"POST", "/api/chat"},
		{"GET", "/api/sessions"},
		{"GET", "/api/sessions/ses_abc-1.2_X"},
		{"GET", "/api/sessions/ses_abc/state"},
		{"GET", "/api/sessions/ses_abc/recap"},
		{"POST", "/api/sessions/ses_abc/message"},
		{"POST", "/api/sessions/ses_abc/cancel"},
		{"POST", "/api/sessions/ses_abc/retry"},
		{"POST", "/api/sessions/ses_abc/compact"},
		{"PUT", "/api/sessions/ses_abc/title"},
		{"PUT", "/api/sessions/ses_abc/model"},
		{"POST", "/api/sessions/ses_abc/rewinds"},
		{"GET", "/api/sessions/ses_abc/user-messages"},
		{"POST", "/api/permissions/resolve"},
		{"POST", "/api/questions"},
		{"GET", "/api/models"},
		{"POST", "/api/projects"},
	}
	for _, rt := range routes {
		resp := doAPI(t, s.URL, rt.method, rt.path)
		hits, last, auth, _ := up.snapshot()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s: want 200 from upstream, got %d", rt.method, rt.path, resp.StatusCode)
			continue
		}
		if want := rt.method + " " + rt.path; last != want {
			t.Errorf("upstream saw %q, want %q (hits=%d)", last, want, hits)
		}
		if auth != "Bearer ocode-secret" {
			t.Errorf("%s %s: upstream auth %q, want the ocode token", rt.method, rt.path, auth)
		}
	}
}

func TestAPIAllowlistRefusesEverythingElseWithoutForwarding(t *testing.T) {
	up := &recordingUpstream{}
	upSrv := httptest.NewServer(up)
	defer upSrv.Close()
	s, _ := newTestBridge(t, upSrv.URL)

	refused := []struct{ method, path string }{
		{"GET", "/api/terminal"},
		{"POST", "/api/terminal"},
		{"PUT", "/api/permissions/yolo"},
		{"PUT", "/api/permissions/mode"},
		{"POST", "/api/permissions/bash-rule"},
		{"GET", "/api/files/content"},
		{"POST", "/api/git/commit"},
		{"DELETE", "/api/projects/x"},
		{"DELETE", "/api/sessions/x"},
		{"GET", "/api/sessions/x/export"},
		{"GET", "/api/sessions/x/terminal"},
		{"POST", "/api/shell"},
		{"GET", "/api/chat"},                          // wrong method on an allowed path
		{"GET", "/api/permissions/resolve"},           // wrong method
		{"DELETE", "/api/sessions/x/state"},           // wrong method
		{"GET", "/api/sessions/x/title"},              // wrong method
		{"POST", "/api/sessions"},                     // wrong method
		{"GET", "/api/sessions/x/state/extra"},        // extra segment
		{"GET", "/api/sessions/a.b/c/state"},          // id cannot contain a slash
		{"GET", "/api/sessions/a%20b"},                // id outside the id alphabet
		{"GET", "/api/sessions/"},                     // empty id, trailing slash
		{"GET", "/api/sessions/x/state/"},             // trailing slash
		{"GET", "/api/sessions/..%2Fterminal"},        // encoded traversal
		{"GET", "/api/sessions/a%2Fstate"},            // encoded separator inside an id
		{"GET", "/api/sessions/%2e%2e/terminal"},      // encoded dot-dot
		{"GET", "/api/sessions/a%5Cstate"},            // encoded backslash
		{"GET", "/api/sessions/x%00/state"},           // encoded NUL
		{"GET", "/api/sessions/../terminal"},          // traversal
		{"GET", "/api/sessions/a%2F..%2Fterminal"},    // encoded traversal, the escaped separator form
		{"POST", "/api/sessions/../permissions/yolo"}, // traversal into a denied route
	}
	for _, rt := range refused {
		before, _, _, _ := up.snapshot()
		resp := doAPI(t, s.URL, rt.method, rt.path)
		after, _, _, _ := up.snapshot()
		if after != before {
			t.Errorf("%s %s reached the upstream", rt.method, rt.path)
		}
		// An unclean path (".." or "//") is answered by the mux's own redirect before allowAPI runs; the
		// redirect target is the cleaned path, which allowAPI checks when the client follows it. Nothing is
		// forwarded in either case. Every other refused path must be a 403 from allowAPI.
		redirect := resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect
		unclean := strings.Contains(rt.path, "..")
		if !(resp.StatusCode == http.StatusForbidden || (unclean && redirect)) {
			t.Errorf("%s %s: want 403 (or a mux redirect for an unclean path), got %d", rt.method, rt.path, resp.StatusCode)
		}
	}
}

func TestAPIAllowlistRefusesUncleanPathsItself(t *testing.T) {
	// Bypass the mux (which redirects unclean paths before allowAPI sees them) and check the guard directly.
	var forwarded int
	guard := allowAPI(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded++ }))
	for _, target := range []string{
		"/api/sessions/../terminal",
		"/api/sessions/a%2F..%2Fterminal",
		"/api/sessions/a%2Fstate",
		"/api/sessions/./state", // dot as the id: the id regex accepts it, only path.Clean refuses it
		"/api/sessions/x%00/state",
		"/api/sessions/./x/state",
		"/api/sessions/x//state",
		"/api/sessions/x/state/",
	} {
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: want 403 from the guard, got %d", target, rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
			t.Errorf("%s: want a short JSON error body, got %q", target, rec.Body.String())
		}
	}
	if forwarded != 0 {
		t.Fatalf("guard forwarded %d unclean request(s)", forwarded)
	}
}

func TestAPIAllowlistStripsTokenQueryParameter(t *testing.T) {
	up := &recordingUpstream{}
	upSrv := httptest.NewServer(up)
	defer upSrv.Close()
	s, _ := newTestBridge(t, upSrv.URL)

	doAPI(t, s.URL, "GET", "/api/sessions?token=pane-leak&limit=5")
	if _, _, _, q := up.snapshot(); q != "limit=5" {
		t.Fatalf("token was not stripped: upstream query %q", q)
	}
	doAPI(t, s.URL, "GET", "/api/sessions?token=a&token=b")
	if _, _, _, q := up.snapshot(); q != "" {
		t.Fatalf("repeated token not fully stripped: upstream query %q", q)
	}
	doAPI(t, s.URL, "GET", "/api/sessions?to%6Ben=c&x=1")
	if _, _, _, q := up.snapshot(); q != "x=1" {
		t.Fatalf("encoded token name not stripped: upstream query %q", q)
	}
}

func TestAPIAllowlistStillRequiresPaneKey(t *testing.T) {
	up := &recordingUpstream{}
	upSrv := httptest.NewServer(up)
	defer upSrv.Close()
	s, _ := newTestBridge(t, upSrv.URL)
	resp, err := noRedirect.Get(s.URL + "/api/models")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("allowed route without the pane key: want 401, got %d", resp.StatusCode)
	}
	if hits, _, _, _ := up.snapshot(); hits != 0 {
		t.Fatal("unauthenticated request reached the upstream")
	}
}
