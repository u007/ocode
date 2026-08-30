package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
)

// favoriteTestHandler builds a *Handler with the shared model-state file
// (XDG_STATE_HOME/opencode/model.json) pointed at a throwaway dir, so the
// favorite writes land in temp state instead of the developer's real list.
func favoriteTestHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return NewHandler()
}

func doFavoriteRequest(t *testing.T, h *Handler, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/api/models/favorite", strings.NewReader(body))
	if method == http.MethodPut {
		h.HandleAddFavoriteModel(w, r)
	} else {
		h.HandleRemoveFavoriteModel(w, r)
	}
	return w
}

type favoritesResponse struct {
	Model     string   `json:"model"`
	Favorite  bool     `json:"favorite"`
	Favorites []string `json:"favorites"`
}

func decodeFavorites(t *testing.T, w *httptest.ResponseRecorder) favoritesResponse {
	t.Helper()
	var resp favoritesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, w.Body.String())
	}
	return resp
}

func TestHandleFavoriteModelRejectsBadInput(t *testing.T) {
	h := favoriteTestHandler(t)

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
			w := doFavoriteRequest(t, h, http.MethodPut, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("PUT status = %d, want 400 (body=%s)", w.Code, tc.body)
			}
			w = doFavoriteRequest(t, h, http.MethodDelete, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("DELETE status = %d, want 400 (body=%s)", w.Code, tc.body)
			}
		})
	}
	if favs := config.LoadFavorites(); len(favs) != 0 {
		t.Errorf("favorites = %v after rejected requests, want none persisted", favs)
	}
}

func TestHandleFavoriteModelAddRemoveRoundTrip(t *testing.T) {
	h := favoriteTestHandler(t)
	const id = "anthropic/claude-sonnet-4-6"

	// Add.
	w := doFavoriteRequest(t, h, http.MethodPut, `{"model":"`+id+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	resp := decodeFavorites(t, w)
	if !resp.Favorite || resp.Model != id {
		t.Errorf("add response = %+v, want favorite=true model=%q", resp, id)
	}
	if len(resp.Favorites) != 1 || resp.Favorites[0] != id {
		t.Errorf("add response favorites = %v, want [%s]", resp.Favorites, id)
	}
	if !config.IsFavorite(id) {
		t.Errorf("IsFavorite(%s) = false after add; disk state not persisted", id)
	}

	// Add again — idempotent, list unchanged.
	w = doFavoriteRequest(t, h, http.MethodPut, `{"model":"`+id+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("re-add status = %d, want 200", w.Code)
	}
	resp = decodeFavorites(t, w)
	if len(resp.Favorites) != 1 {
		t.Errorf("re-add favorites = %v, want single entry (idempotent)", resp.Favorites)
	}

	// Remove.
	w = doFavoriteRequest(t, h, http.MethodDelete, `{"model":"`+id+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200", w.Code)
	}
	resp = decodeFavorites(t, w)
	if resp.Favorite {
		t.Errorf("remove response favorite = true, want false")
	}
	for _, f := range resp.Favorites {
		if f == id {
			t.Errorf("remove response still contains %q: %v", id, resp.Favorites)
		}
	}
	if config.IsFavorite(id) {
		t.Errorf("IsFavorite(%s) = true after remove", id)
	}

	// Remove again — idempotent, still 200 with empty list.
	w = doFavoriteRequest(t, h, http.MethodDelete, `{"model":"`+id+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("re-remove status = %d, want 200", w.Code)
	}
	resp = decodeFavorites(t, w)
	if len(resp.Favorites) != 0 {
		t.Errorf("re-remove favorites = %v, want empty", resp.Favorites)
	}
}

func TestHandleFavoriteModelKeepsSavedOrder(t *testing.T) {
	h := favoriteTestHandler(t)

	for _, id := range []string{"openai/gpt-4o", "anthropic/claude-sonnet-4-6", "groq/compound"} {
		w := doFavoriteRequest(t, h, http.MethodPut, `{"model":"`+id+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("add %s status = %d, want 200", id, w.Code)
		}
	}
	got := decodeFavorites(t, doFavoriteRequest(t, h, http.MethodPut, `{"model":"openai/gpt-4o"}`))
	want := []string{"openai/gpt-4o", "anthropic/claude-sonnet-4-6", "groq/compound"}
	if strings.Join(got.Favorites, ",") != strings.Join(want, ",") {
		t.Errorf("favorites order = %v, want %v (re-adding must not reorder)", got.Favorites, want)
	}
}

// TestFavoriteRoutesRegistered verifies the PUT/DELETE /api/models/favorite
// routes resolve through the server mux (they were long missing from
// registerRoutes even though the handlers existed) and reach the handlers.
func TestFavoriteRoutesRegistered(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := New("localhost:0", "", "", nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/models/favorite", strings.NewReader(`{"model":"openai/gpt-4o"}`))
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/models/favorite status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !config.IsFavorite("openai/gpt-4o") {
		t.Errorf("route did not reach the add handler: IsFavorite = false")
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodDelete, "/api/models/favorite", strings.NewReader(`{"model":"openai/gpt-4o"}`))
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /api/models/favorite status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if config.IsFavorite("openai/gpt-4o") {
		t.Errorf("route did not reach the remove handler: IsFavorite = true")
	}
}

// TestListModelsDualMemberCarriesBothFlags pins the flag semantics the web/
// desktop star depends on: a model that is BOTH recently used and favorited
// must appear exactly once (Recently Used placement) while still reporting
// favorite=true, so its star renders lit and un-favoriting it from the web
// picker reaches RemoveFavoriteModel instead of re-adding it. Regression
// guard for the earlier `!recentSet[id] && favSet[id]` masking, which made
// recent favorites look unfavorited in the web/desktop picker.
func TestListModelsDualMemberCarriesBothFlags(t *testing.T) {
	h := favoriteTestHandler(t)
	const id = "openai/gpt-4o"

	if err := config.SaveFavoriteModel(id); err != nil {
		t.Fatalf("SaveFavoriteModel: %v", err)
	}
	if err := config.SaveRecentModel(id); err != nil {
		t.Fatalf("SaveRecentModel: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	h.HandleListModels(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var models []ModelInfo
	if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var seen int
	for _, m := range models {
		if m.Name == id {
			seen++
			if !m.Recent {
				t.Errorf("%s Recent = false, want true", id)
			}
			if !m.Favorite {
				t.Errorf("%s Favorite = false, want true (raw membership; section dedupe is the UI's job)", id)
			}
		}
	}
	if seen != 1 {
		t.Errorf("%s listed %d times, want exactly once", id, seen)
	}
	if seen == 1 && models[0].Name == id && !models[0].Recent {
		t.Errorf("dual member must lead the list (Recently Used placement), got %+v", models[0])
	}
}

// TestSetSessionModelRecordsRecentPick covers the recents feed for the new
// per-session model override: a session-scoped pick must land in the shared
// recently-used list (like the global config-model setter and the TUI's
// finishModelSwitch) so the web/desktop picker's Recently Used section stays
// in sync no matter which surface switched the model.
func TestSetSessionModelRecordsRecentPick(t *testing.T) {
	h := favoriteTestHandler(t)
	h.mu.Lock()
	h.cfg = &config.Config{}
	h.mu.Unlock()

	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.RegisterWithWindow(id, proj, "win-fav-test")
	// The override handler writes through the persisted transcript, so the
	// session must exist on disk first (in production the first turn creates
	// it before any model switch is possible).
	if err := session.SaveForDir(proj, id, "t", nil, nil); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/sessions/"+id+"/model",
		strings.NewReader(`{"model":"testprov/alpha"}`))
	h.HandleSetSessionModel(w, r, id)
	if w.Code != http.StatusOK {
		t.Fatalf("set status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	recents := config.LoadRecentModels()
	if len(recents) != 1 || recents[0] != "testprov/alpha" {
		t.Fatalf("LoadRecentModels() = %v, want [testprov/alpha]", recents)
	}

	// Clearing the override must not mutate the recent list.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodDelete, "/api/sessions/"+id+"/model", nil)
	h.HandleClearSessionModel(w, r, id)
	if w.Code != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", w.Code)
	}
	if recents := config.LoadRecentModels(); len(recents) != 1 || recents[0] != "testprov/alpha" {
		t.Errorf("LoadRecentModels() after clear = %v, want unchanged [testprov/alpha]", recents)
	}
}
