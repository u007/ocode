package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The install endpoint refuses an empty or malformed selection before it
// touches the network or ~/.claude/skills.
func TestInstallIntentSkillsRejectsEmptySelection(t *testing.T) {
	for _, body := range []string{`{"names":[]}`, `{}`, `not json`} {
		h := NewHandler()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/skills/tanstack-intent/install", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		h.HandleInstallIntentSkills(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, w.Code)
		}
		if !strings.Contains(w.Body.String(), "names is required") {
			t.Fatalf("body %q: response %q does not name the missing field", body, w.Body.String())
		}
	}
}
