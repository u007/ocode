package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

func putAllowedHosts(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/permissions-hosts", strings.NewReader(body))
	h.HandleSetAllowedHostsConfig(w, r)
	return w
}

func getAllowedHosts(t *testing.T, h *Handler) []string {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/ocode/permissions-hosts", nil)
	h.HandleGetAllowedHostsConfig(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Hosts []string `json:"hosts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET body %q: %v", w.Body.String(), err)
	}
	return resp.Hosts
}

// TestHandleAllowedHostsPutThenGetRoundTrips pins that the PUT replies and the
// GET returns the same normalised, de-duplicated, sorted list, and that the
// list reaches the config file on disk.
func TestHandleAllowedHostsPutThenGetRoundTrips(t *testing.T) {
	h := testConfigHandler(t)

	w := putAllowedHosts(t, h, `{"hosts":["App.Example.com","*.Example.org","app.example.com","https://docs.site:8443/guide"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", w.Code, w.Body.String())
	}
	want := []string{"*.example.org", "app.example.com", "docs.site"}
	var reply struct {
		Hosts []string `json:"hosts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("PUT body %q: %v", w.Body.String(), err)
	}
	if !reflect.DeepEqual(reply.Hosts, want) {
		t.Fatalf("PUT reply hosts = %#v, want %#v", reply.Hosts, want)
	}
	if got := getAllowedHosts(t, h); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET hosts = %#v, want %#v", got, want)
	}

	var cfg config.Config
	if err := config.LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Ocode.Permissions.AllowedHosts; !reflect.DeepEqual(got, want) {
		t.Fatalf("on-disk allowed_hosts = %#v, want %#v", got, want)
	}

	w = putAllowedHosts(t, h, `{"hosts":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := getAllowedHosts(t, h); len(got) != 0 {
		t.Fatalf("empty PUT did not clear the list: %#v", got)
	}
}

// TestHandleAllowedHostsRejectsInvalidEntry pins that an entry which normalises
// to nothing is a 400 that names the entry, and that nothing is saved.
func TestHandleAllowedHostsRejectsInvalidEntry(t *testing.T) {
	h := testConfigHandler(t)
	if w := putAllowedHosts(t, h, `{"hosts":["keep.example.com"]}`); w.Code != http.StatusOK {
		t.Fatalf("seed status = %d, body=%s", w.Code, w.Body.String())
	}

	for _, bad := range []string{"*", "https://", "*.*.example.com"} {
		body, _ := json.Marshal(map[string]any{"hosts": []string{"ok.example.com", bad}})
		w := putAllowedHosts(t, h, string(body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("entry %q: status = %d, want 400, body=%s", bad, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), bad) {
			t.Fatalf("entry %q: 400 body does not name it: %s", bad, w.Body.String())
		}
	}

	if got := getAllowedHosts(t, h); !reflect.DeepEqual(got, []string{"keep.example.com"}) {
		t.Fatalf("rejected PUT changed the saved list: %#v", got)
	}
}

func TestHandleAllowedHostsRejectsMalformedBody(t *testing.T) {
	h := testConfigHandler(t)
	if w := putAllowedHosts(t, h, `{`); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400", w.Code)
	}
}

// TestHandleAllowedHostsPushesToLiveAgents pins that a saved list takes effect
// on an already-open chat's permission manager, with no rebuild.
func TestHandleAllowedHostsPushesToLiveAgents(t *testing.T) {
	h := testConfigHandler(t)

	live := agent.NewAgent(nil, nil, &config.Config{}, nil)
	h.mu.Lock()
	h.agents["ses_live"] = &agentSession{agent: live}
	h.mu.Unlock()
	if live.Permissions().HostAllowed("live.example.com") {
		t.Fatal("precondition: live agent already allows the host")
	}

	if w := putAllowedHosts(t, h, `{"hosts":["*.example.com"]}`); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", w.Code, w.Body.String())
	}
	pm := live.Permissions()
	if !pm.HostAllowed("live.example.com") {
		t.Fatalf("live agent did not receive the list: %#v", pm.AllowedHosts())
	}
	if pm.HostAllowed("example.com") {
		t.Fatal("wildcard entry must not cover the apex")
	}
}
