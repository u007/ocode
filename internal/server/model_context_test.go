package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// serverRepoRoot returns the repo root (has skills/kaizen + *.OCODE.md files)
// so indicator tests exercise real embedded corpus state hermetically-ish, the
// same way internal/skill's kaizen delivery tests do.
func serverRepoRoot() string {
	_, f, _, _ := runtime.Caller(0) // internal/server/model_context_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(f), "..", ".."))
}

// The headless snapshot must carry the model-prompt indicator (custom
// .OCODE.md source + force-injected Kaizen directives) for the model actually
// configured, anchored at the server workdir.
func TestBuildStatusSnapshotCarriesModelPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate global config scans
	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.cfg.Model = "opencode-go/deepseek-v4-flash"
	h.workDir = serverRepoRoot()
	h.mu.Unlock()

	snap := h.buildStatusSnapshot()
	if snap.ModelPrompt == nil {
		t.Fatal("ModelPrompt nil for a tuned model with a repo .OCODE.md")
	}
	if snap.ModelPrompt.Kind != "file" {
		t.Errorf("ModelPrompt.Kind = %q, want file", snap.ModelPrompt.Kind)
	}
	if snap.ModelPrompt.Tokens <= 0 {
		t.Errorf("ModelPrompt.Tokens = %d, want > 0", snap.ModelPrompt.Tokens)
	}
	found := false
	for _, k := range snap.ModelPrompt.Kaizen {
		if k.Name == "conduct-tuning-deepseek-v4-flash" && k.TunedFor == "deepseek-v4-flash" {
			found = true
		}
	}
	if !found {
		t.Fatalf("kaizen directives missing conduct-tuning-deepseek-v4-flash: %+v", snap.ModelPrompt.Kaizen)
	}
}

// An untuned model with no .OCODE.md anywhere must produce a nil ModelPrompt so
// the web renders no banner (parity with the TUI row being absent).
func TestBuildStatusSnapshotNoModelPromptForUntunedModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate global config scans
	h := testHandlerWithConfig(t) // Model = gpt-4o-mini, no tuning
	h.mu.Lock()
	h.workDir = t.TempDir()
	h.mu.Unlock()

	snap := h.buildStatusSnapshot()
	if snap.ModelPrompt != nil {
		t.Fatalf("ModelPrompt = %+v, want nil for untuned model", snap.ModelPrompt)
	}
}

// GET /api/models must badge tuned models: has_model_prompt + has_kaizen true
// for a model with a repo .OCODE.md + conduct digest, false for an untuned
// model. Recents force both rows into the list regardless of the registry
// cache state.
func TestListModelsCarriesPromptFlags(t *testing.T) {
	h := favoriteTestHandler(t)
	h.mu.Lock()
	h.workDir = serverRepoRoot()
	h.mu.Unlock()

	const tuned = "opencode-go/deepseek-v4-flash"
	const untuned = "anthropic/claude-opus-4-8"
	for _, id := range []string{tuned, untuned} {
		if err := config.SaveRecentModel(id); err != nil {
			t.Fatalf("SaveRecentModel(%s): %v", id, err)
		}
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
	seen := map[string]bool{}
	for _, m := range models {
		seen[m.Name] = true
		switch m.Name {
		case tuned:
			if !m.HasModelPrompt || !m.HasKaizen {
				t.Errorf("%s flags = {prompt:%v kaizen:%v}, want both true", tuned, m.HasModelPrompt, m.HasKaizen)
			}
		case untuned:
			if m.HasModelPrompt || m.HasKaizen {
				t.Errorf("%s flags = {prompt:%v kaizen:%v}, want both false", untuned, m.HasModelPrompt, m.HasKaizen)
			}
		}
	}
	if !seen[tuned] || !seen[untuned] {
		t.Fatalf("recents did not force both rows into the list; seen=%v", seen)
	}
}
