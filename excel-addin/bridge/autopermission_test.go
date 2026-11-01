package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeOcodeConfig stands in for ocode's /api/config/ocode/permissions-auto. It serves cfg on GET, records every PUT
// body, and answers fail[method] with that status when set.
type fakeOcodeConfig struct {
	mu       sync.Mutex
	cfg      string
	fail     map[string]int
	gets     int
	puts     [][]byte
	hits     int
	lastAuth string
	lastKey  string
}

const ocodeAutoPath = "/api/config/ocode/permissions-auto"

func (f *fakeOcodeConfig) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits++
	f.lastAuth = r.Header.Get("Authorization")
	f.lastKey = r.Header.Get("X-Bridge-Key")
	if r.URL.Path != ocodeAutoPath {
		http.NotFound(w, r)
		return
	}
	if status, ok := f.fail[r.Method]; ok {
		http.Error(w, "boom from ocode", status)
		return
	}
	switch r.Method {
	case http.MethodGet:
		f.gets++
		_, _ = io.WriteString(w, f.cfg)
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.puts = append(f.puts, b)
		_, _ = io.WriteString(w, `{}`)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

// autoConfigJSON is a realistic block that is switched off (ocode omits "enabled" then). The large integer must
// survive the bridge's round trip digit for digit.
const autoConfigJSON = `{"model":"typesafe/jev-latest","min_confidence":0.85,"max_context_bytes":9007199254740993,` +
	`"allow_destructive":true,"grants":[{"tool":"bash","pattern":"go test *"}],"relaxed_concerns":["network"]}`

// snapshot returns what the fake has seen so far. It is the only way tests read the fake's fields.
func (f *fakeOcodeConfig) snapshot() (hits int, puts [][]byte, auth, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits, f.puts, f.lastAuth, f.lastKey
}

func newAutoPermissionBridge(t *testing.T, fake *fakeOcodeConfig) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(fake)
	t.Cleanup(up.Close)
	s, _ := newTestBridge(t, up.URL)
	return s
}

// autoCall sends one request to the bridge's auto-permission route and returns the status and body.
func autoCall(t *testing.T, base, method, body string) (int, string) {
	t.Helper()
	resp := doAPI(t, base, method, "/bridge/auto-permission", body)
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

func decodeNumbers(t *testing.T, b []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("not a JSON object: %q: %v", b, err)
	}
	return m
}

func TestAutoPermissionGETReportsOcodeEnabledFlag(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      string
		wantCode int
		want     string
	}{
		{"absent means off", autoConfigJSON, 200, `{"enabled":false}`},
		{"explicitly off", `{"enabled":false}`, 200, `{"enabled":false}`},
		{"on", `{"enabled":true,"model":"typesafe/jev-latest"}`, 200, `{"enabled":true}`},
		{"enabled is not a bool", `{"enabled":"yes"}`, 502, ""},
		{"config is null", `null`, 502, ""},
		{"config is not an object", `[]`, 502, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeOcodeConfig{cfg: tc.cfg}
			s := newAutoPermissionBridge(t, fake)
			code, body := autoCall(t, s.URL, "GET", "")
			if code != tc.wantCode {
				t.Fatalf("status %d, want %d (body %q)", code, tc.wantCode, body)
			}
			if tc.want != "" && strings.TrimSpace(body) != tc.want {
				t.Fatalf("body %q, want %q", body, tc.want)
			}
			if tc.wantCode == 502 && !strings.Contains(body, `"error"`) {
				t.Fatalf("502 must carry an error body, got %q", body)
			}
			if _, puts, _, _ := fake.snapshot(); len(puts) != 0 {
				t.Fatal("GET must not write to ocode")
			}
		})
	}
}

func TestAutoPermissionPUTChangesOnlyEnabled(t *testing.T) {
	for _, tc := range []struct {
		name     string
		before   string
		body     string // the PUT body from the pane
		wantBody string // the answer
		want     bool   // the enabled value ocode must receive
	}{
		{"off to on", autoConfigJSON, `{"enabled":true}`, `{"enabled":true}`, true},
		{"on to off", `{"enabled":true,"model":"typesafe/jev-latest","min_confidence":0.85,` +
			`"max_context_bytes":9007199254740993,"grants":[{"tool":"bash","pattern":"go test *"}]}`,
			`{"enabled":false}`, `{"enabled":false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeOcodeConfig{cfg: tc.before}
			s := newAutoPermissionBridge(t, fake)
			code, body := autoCall(t, s.URL, "PUT", tc.body)
			if code != 200 || strings.TrimSpace(body) != tc.wantBody {
				t.Fatalf("PUT answered %d %q, want 200 %q", code, body, tc.wantBody)
			}
			_, puts, auth, key := fake.snapshot()
			if len(puts) != 1 {
				t.Fatalf("want exactly one PUT to ocode, got %d", len(puts))
			}
			want := decodeNumbers(t, []byte(tc.before))
			want["enabled"] = tc.want
			if got := decodeNumbers(t, puts[0]); !reflect.DeepEqual(got, want) {
				t.Fatalf("PUT body changed more than enabled:\n got  %v\n want %v", got, want)
			}
			if !bytes.Contains(puts[0], []byte("9007199254740993")) {
				t.Fatalf("large integer lost precision in %s", puts[0])
			}
			if auth != "Bearer ocode-secret" {
				t.Fatalf("ocode got Authorization %q, want the bridge's ocode token", auth)
			}
			if key != "" {
				t.Fatalf("the pane key reached ocode: X-Bridge-Key %q", key)
			}
		})
	}
}

func TestAutoPermissionPUTRejectsBadBodiesBeforeTouchingOcode(t *testing.T) {
	for _, body := range []string{
		``,
		`{}`,
		`null`,
		`[]`,
		`not json`,
		`{"enabled":"yes"}`,
		`{"enabled":1}`,
		`{"enabled":null}`,
		`{"enabled":true,"model":"x"}`,
		`{"ENABLED":true}`,
		`{"enabled":true} {}`,
	} {
		fake := &fakeOcodeConfig{cfg: autoConfigJSON}
		s := newAutoPermissionBridge(t, fake)
		code, out := autoCall(t, s.URL, "PUT", body)
		if code != http.StatusBadRequest {
			t.Errorf("body %q: want 400, got %d (%q)", body, code, out)
		}
		if hits, _, _, _ := fake.snapshot(); hits != 0 {
			t.Errorf("body %q: bridge touched ocode (%d requests)", body, hits)
		}
	}
}

func TestAutoPermissionSurfacesOcodeErrorsAs502(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		fail   map[string]int
		want   string // substring the body must carry
	}{
		{"GET fails with 500", "GET", map[string]int{"GET": 500}, "GET /api/config/ocode/permissions-auto with 500 Internal Server Error: boom from ocode"},
		{"GET fails with 503", "GET", map[string]int{"GET": 503}, "GET /api/config/ocode/permissions-auto with 503 Service Unavailable: boom from ocode"},
		{"PUT's read fails", "PUT", map[string]int{"GET": 500}, "GET /api/config/ocode/permissions-auto with 500 Internal Server Error: boom from ocode"},
		{"PUT fails with 500", "PUT", map[string]int{"PUT": 500}, "PUT /api/config/ocode/permissions-auto with 500 Internal Server Error: boom from ocode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeOcodeConfig{cfg: autoConfigJSON, fail: tc.fail}
			s := newAutoPermissionBridge(t, fake)
			body := ""
			if tc.method == "PUT" {
				body = `{"enabled":true}`
			}
			code, out := autoCall(t, s.URL, tc.method, body)
			if code != http.StatusBadGateway {
				t.Fatalf("want 502, got %d (%q)", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("body %q does not carry %q", out, tc.want)
			}
			if _, puts, _, _ := fake.snapshot(); len(puts) != 0 {
				t.Fatalf("a failed read must not lead to a PUT, got %d", len(puts))
			}
		})
	}
}

func TestAutoPermissionReportsUnreachableOcodeAs502(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	up.Close()
	s, _ := newTestBridge(t, up.URL)
	for _, method := range []string{"GET", "PUT"} {
		body := ""
		if method == "PUT" {
			body = `{"enabled":true}`
		}
		code, out := autoCall(t, s.URL, method, body)
		if code != http.StatusBadGateway || !strings.Contains(out, "not reachable") {
			t.Fatalf("%s: want 502 naming the unreachable ocode, got %d %q", method, code, out)
		}
	}
}

func TestAutoPermissionRoutesRequirePaneKey(t *testing.T) {
	fake := &fakeOcodeConfig{cfg: autoConfigJSON}
	s := newAutoPermissionBridge(t, fake)
	for _, method := range []string{"GET", "PUT"} {
		req, _ := http.NewRequest(method, s.URL+"/bridge/auto-permission", strings.NewReader(`{"enabled":true}`))
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without the pane key: want 401, got %d", method, resp.StatusCode)
		}
	}
	if hits, _, _, _ := fake.snapshot(); hits != 0 {
		t.Fatal("an unauthenticated request reached ocode")
	}
}

// The routes are bridge-owned: the /api/ proxy must still refuse the ocode config path, so the allowlist did not grow.
func TestAutoPermissionConfigIsNotOpenedThroughTheAPIProxy(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		fake := &fakeOcodeConfig{cfg: autoConfigJSON}
		s := newAutoPermissionBridge(t, fake)
		resp := doAPI(t, s.URL, method, "/api/config/ocode/permissions-auto", `{"enabled":true}`)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s via /api/: want 403, got %d", method, resp.StatusCode)
		}
		if hits, _, _, _ := fake.snapshot(); hits != 0 {
			t.Errorf("%s via /api/ reached ocode", method)
		}
	}
}
