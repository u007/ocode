package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// The headless web callbacks wired everything except the two discovery notices,
// so the TUI's "~ Discovered"/"~ Indexing" lines had no web counterpart. This
// pins the wiring and the payload shape (TextDelta, matching text/thinking).
func TestWireHeadlessAgentCallbacksEmitsDiscoveryNotices(t *testing.T) {
	h := NewHandler()
	h.sessions.Register("ses_discovery", "/proj")
	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	ag := &agent.Agent{}
	h.wireHeadlessAgentCallbacks("ses_discovery", ag)

	if ag.OnDiscovery == nil {
		t.Fatal("OnDiscovery not wired")
	}
	if ag.OnMDIndexing == nil {
		t.Fatal("OnMDIndexing not wired")
	}

	// An empty names payload must not emit a notice.
	ag.OnDiscovery("")

	ag.OnDiscovery("Notion/search, github/pr")
	ag.OnMDIndexing("docs/README.md")

	want := []struct {
		event string
		delta string
	}{
		{"discovery", "Notion/search, github/pr"},
		{"md_indexing", "docs/README.md"},
	}
	for _, w := range want {
		select {
		case env := <-sub:
			if env.Event != w.event {
				t.Fatalf("event = %q, want %q", env.Event, w.event)
			}
			if env.SessionID != "ses_discovery" {
				t.Fatalf("session = %q, want ses_discovery", env.SessionID)
			}
			td, ok := env.Data.(TextDelta)
			if !ok {
				t.Fatalf("data type = %T, want TextDelta", env.Data)
			}
			if td.Delta != w.delta {
				t.Fatalf("delta = %q, want %q", td.Delta, w.delta)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q event", w.event)
		}
	}
}

func TestHandleSessionDiscoveryConfigOnlyWithoutLiveAgent(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.cfg.Ocode.Discovery = config.DiscoveryConfig{
		Enabled:          true,
		EmbeddingModel:   "bge-m3",
		EmbeddingBackend: "local",
		IgnorePaths:      []string{"dist/"},
	}
	h.mu.Unlock()
	h.sessions.Register("ses_disc", "/proj")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions/ses_disc/discovery", nil)
	h.HandleSessionDiscovery(w, r, "ses_disc")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp discoveryStatusDTO
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Enabled || resp.EmbeddingModel != "bge-m3" || resp.EmbeddingBackend != "local" {
		t.Errorf("config not echoed: %+v", resp)
	}
	if len(resp.IgnorePaths) != 1 || resp.IgnorePaths[0] != "dist/" {
		t.Errorf("ignore paths not echoed: %+v", resp.IgnorePaths)
	}
	if resp.Live {
		t.Error("Live = true with no live agent, want false")
	}
}

func TestHandleSessionDiscoveryReportsLiveAgent(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.sessions.Register("ses_live", "/proj")
	h.mu.Lock()
	h.agents["ses_live"] = &agentSession{agent: &agent.Agent{}}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions/ses_live/discovery", nil)
	h.HandleSessionDiscovery(w, r, "ses_live")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp discoveryStatusDTO
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Live {
		t.Fatalf("Live = false, want true (live agent reachable): %+v", resp)
	}
	// Lists must serialize as [] not null — the web reads `.length` on them.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	for _, key := range []string{"attached_skills", "attached_mcp", "attached_md", "all_skills", "all_mcp", "all_md"} {
		if got := string(raw[key]); got != "[]" {
			t.Errorf("%s = %s, want []", key, got)
		}
	}
}

func TestHandleSessionDiscoveryUnknownSession(t *testing.T) {
	h := testHandlerWithConfig(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions/ses_missing/discovery", nil)
	h.HandleSessionDiscovery(w, r, "ses_missing")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}
