package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeOcodeConnect stands in for ocode's GET /api/auth/connect. It serves body and records the calls it receives.
type fakeOcodeConnect struct {
	mu   sync.Mutex
	body string
	fail int
	hits int
	auth string
}

func (f *fakeOcodeConnect) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits++
	f.auth = r.Header.Get("Authorization")
	if r.URL.Path != "/api/auth/connect" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if f.fail != 0 {
		http.Error(w, "boom from ocode", f.fail)
		return
	}
	_, _ = io.WriteString(w, f.body)
}

// connectListJSON is shaped like ocode's answer, masked key tails and OAuth account included, so the tests can prove
// that none of it reaches the pane.
const connectListJSON = `{"providers":[
 {"id":"openai","label":"OpenAI","status":"✗","statusDetail":"not configured","hasCredential":false,"methods":[{"id":"apikey"}]},
 {"id":"opencode-go","label":"OpenCode Go","kind":"api","status":"✓","statusDetail":"api key","hasCredential":true,"masked":"••••••••••••xk4X","methods":[]},
 {"id":"anthropic","label":"Anthropic","kind":"oauth","status":"✓","statusDetail":"oauth (james@example.com)","hasCredential":true,"masked":"oauth ••••","methods":[]},
 {"id":"deepseek","label":"DeepSeek","status":"✓","statusDetail":"env","hasCredential":false,"methods":[]},
 {"id":"zai","label":"Z.AI","status":"✓","statusDetail":"config","hasCredential":false,"methods":[]}
]}`

func newConnectedProvidersBridge(t *testing.T, fake *fakeOcodeConnect) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(fake)
	t.Cleanup(up.Close)
	s, _ := newTestBridge(t, up.URL)
	return s
}

// The pane learns which providers are connected and nothing else: no key tail, no account, no per-provider detail.
func TestConnectedProvidersListsOnlyConnectedIDsSorted(t *testing.T) {
	fake := &fakeOcodeConnect{body: connectListJSON}
	s := newConnectedProvidersBridge(t, fake)
	resp := doAPI(t, s.URL, "GET", "/bridge/connected-providers")
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d (%q)", resp.StatusCode, b)
	}
	if want := `{"providers":["anthropic","deepseek","opencode-go","zai"]}`; strings.TrimSpace(string(b)) != want {
		t.Fatalf("body %q, want %q", b, want)
	}
	for _, leak := range []string{"xk4X", "••", "james@example.com", "statusDetail", "masked"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("response leaks %q: %s", leak, b)
		}
	}
	if fake.auth != "Bearer ocode-secret" {
		t.Errorf("ocode saw auth %q, want the bridge's own token", fake.auth)
	}
}

func TestConnectedProvidersWithNoneConnectedIsAnEmptyList(t *testing.T) {
	fake := &fakeOcodeConnect{body: `{"providers":[{"id":"openai","status":"✗"}]}`}
	s := newConnectedProvidersBridge(t, fake)
	resp := doAPI(t, s.URL, "GET", "/bridge/connected-providers")
	b, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(b)) != `{"providers":[]}` {
		t.Fatalf("want an empty list, not null: %d %q", resp.StatusCode, b)
	}
}

func TestConnectedProvidersReportsOcodeFailuresAs502(t *testing.T) {
	for name, fake := range map[string]*fakeOcodeConnect{
		"ocode answers 500":          {fail: http.StatusInternalServerError},
		"ocode answers a bare array": {body: `[]`},
		"ocode answers no providers": {body: `{}`},
		"ocode answers not JSON":     {body: `nope`},
	} {
		t.Run(name, func(t *testing.T) {
			s := newConnectedProvidersBridge(t, fake)
			resp := doAPI(t, s.URL, "GET", "/bridge/connected-providers")
			b, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(b), `"error"`) {
				t.Fatalf("want 502 with an error body, got %d %q", resp.StatusCode, b)
			}
		})
	}
}

func TestConnectedProvidersRouteRequiresPaneKeyAndGET(t *testing.T) {
	fake := &fakeOcodeConnect{body: connectListJSON}
	s := newConnectedProvidersBridge(t, fake)
	req, _ := http.NewRequest("GET", s.URL+"/bridge/connected-providers", nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without the pane key: want 401, got %d", resp.StatusCode)
	}
	resp = doAPI(t, s.URL, "POST", "/bridge/connected-providers")
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("POST must not reach the handler: got %d", resp.StatusCode)
	}
	if hits := fake.hits; hits != 0 {
		t.Errorf("unauthenticated or wrong-method requests reached ocode %d time(s)", hits)
	}
}
