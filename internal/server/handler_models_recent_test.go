package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// recentTestHandler builds a *Handler with the shared model-state file
// (XDG_STATE_HOME/opencode/model.json) pointed at a throwaway dir, so the
// recents writes land in temp state instead of the developer's real list.
func recentTestHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return NewHandler()
}

func doRecentRequest(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/models/recent", strings.NewReader(body))
	h.HandleRemoveRecentModel(w, r)
	return w
}

type recentsResponse struct {
	Model   string   `json:"model"`
	Recent  bool     `json:"recent"`
	Recents []string `json:"recents"`
}

func decodeRecents(t *testing.T, w *httptest.ResponseRecorder) recentsResponse {
	t.Helper()
	var resp recentsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, w.Body.String())
	}
	return resp
}

func TestHandleRecentModelRejectsBadInput(t *testing.T) {
	h := recentTestHandler(t)

	cases := []struct {
		name string
		body string
	}{
		{"invalid json", `{`},
		{"missing model", `{}`},
		{"empty model", `{"model":""}`},
		{"whitespace model", `{"model":"   "}`},
		{"missing provider", `{"model":"gpt-4o-mini"}`},
		{"empty provider", `{"model":"/gpt-4o-mini"}`},
		{"empty model part", `{"model":"openai/"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doRecentRequest(t, h, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("DELETE status = %d, want 400 (body=%s)", w.Code, tc.body)
			}
		})
	}
	if recents := config.LoadRecentModels(); len(recents) != 0 {
		t.Errorf("recents = %v after rejected requests, want none persisted", recents)
	}
}

func TestHandleRecentModelRemoveRoundTrip(t *testing.T) {
	h := recentTestHandler(t)
	const id = "anthropic/claude-sonnet-4-6"

	// Seed the recent list directly.
	if err := config.SaveRecentModel(id); err != nil {
		t.Fatalf("seed recent: %v", err)
	}
	if got := config.LoadRecentModels(); len(got) != 1 || got[0] != id {
		t.Fatalf("seeded recents = %v, want [%s]", got, id)
	}

	// Remove.
	w := doRecentRequest(t, h, `{"model":"`+id+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	resp := decodeRecents(t, w)
	if resp.Recent {
		t.Errorf("remove response recent = true, want false")
	}
	if resp.Model != id {
		t.Errorf("remove response model = %q, want %q", resp.Model, id)
	}
	for _, r := range resp.Recents {
		if r == id {
			t.Errorf("remove response still contains %q: %v", id, resp.Recents)
		}
	}
	if len(resp.Recents) != 0 {
		t.Errorf("remove response recents = %v, want empty", resp.Recents)
	}
	got := config.LoadRecentModels()
	if len(got) != 0 {
		t.Errorf("disk recents = %v after remove, want empty", got)
	}

	// Remove again — idempotent, still 200 with empty list.
	w = doRecentRequest(t, h, `{"model":"`+id+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("re-remove status = %d, want 200", w.Code)
	}
	resp = decodeRecents(t, w)
	if len(resp.Recents) != 0 {
		t.Errorf("re-remove recents = %v, want empty", resp.Recents)
	}
}

func TestHandleRecentModelKeepsOtherRecents(t *testing.T) {
	h := recentTestHandler(t)

	keep := "openai/gpt-4o"
	remove := "anthropic/claude-sonnet-4-6"
	for _, id := range []string{keep, remove} {
		if err := config.SaveRecentModel(id); err != nil {
			t.Fatalf("seed recent %s: %v", id, err)
		}
	}

	w := doRecentRequest(t, h, `{"model":"`+remove+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200", w.Code)
	}
	resp := decodeRecents(t, w)
	if len(resp.Recents) != 1 || resp.Recents[0] != keep {
		t.Errorf("remove response recents = %v, want [%s]", resp.Recents, keep)
	}
	got := config.LoadRecentModels()
	if len(got) != 1 || got[0] != keep {
		t.Errorf("disk recents = %v, want [%s]", got, keep)
	}
}

// TestRecentRoutesRegistered verifies the DELETE /api/models/recent route
// resolves through the server mux and reaches the handler.
func TestRecentRoutesRegistered(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := New("localhost:0", "", "", nil)

	// Seed a recent via the handler's own storage path so the delete has
	// something to remove, then assert the route reaches the handler.
	if err := config.SaveRecentModel("openai/gpt-4o"); err != nil {
		t.Fatalf("SaveRecentModel: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/models/recent", strings.NewReader(`{"model":"openai/gpt-4o"}`))
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /api/models/recent status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if got := config.LoadRecentModels(); len(got) != 0 {
		t.Errorf("route did not reach the remove handler: recents = %v, want empty", got)
	}

	// A second delete of the now-absent recent is idempotent (200, empty).
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodDelete, "/api/models/recent", strings.NewReader(`{"model":"openai/gpt-4o"}`))
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("re-DELETE /api/models/recent status = %d, want 200", w.Code)
	}
	if len(config.LoadRecentModels()) != 0 {
		t.Errorf("recents not empty after idempotent re-delete")
	}
}
