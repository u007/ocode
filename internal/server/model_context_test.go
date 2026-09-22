package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
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

// TestSessionStatusRecomputesModelPromptForOverride is the regression guard for
// the web sidebar showing the WRONG model's conduct banner after a per-session
// model pick: buildStatusSnapshot derives ModelPrompt from the process-wide
// cfg.Model, and the per-session snapshot builders then overwrite MainModel with
// the session's effective (override) model WITHOUT recomputing ModelPrompt — so
// the row paired the new model id with the old model's .OCODE.md/Kaizen line.
func TestSessionStatusRecomputesModelPromptForOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate global config scans
	h := testHandlerWithConfig(t)
	root := t.TempDir()
	// Two distinct prompts. Neither model has an embedded fallback, so ModelPrompt
	// is always Kind "file" and the basename identifies which model resolved.
	for name, body := range map[string]string{
		"alpha-model.OCODE.md": "alpha conduct",
		"beta-model.OCODE.md":  "beta conduct",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	h.mu.Lock()
	h.cfg.Model = "opencode-go/alpha-model"
	h.workDir = root
	h.mu.Unlock()

	id := session.NewSessionID()
	saveSessionToDir(t, root, id)
	h.sessions.Register(id, root)
	if err := session.UpdateMetadataForDir(root, id, func(md map[string]any) {
		md["model"] = "opencode-go/beta-model"
	}); err != nil {
		t.Fatalf("set session override: %v", err)
	}

	rec := httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/status", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var snap TUIStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if snap.MainModel != "opencode-go/beta-model" {
		t.Fatalf("MainModel = %q, want the session override", snap.MainModel)
	}
	if snap.ModelPrompt == nil {
		t.Fatal("ModelPrompt nil; want the session model's conduct banner")
	}
	if got := filepath.Base(snap.ModelPrompt.Path); got != "beta-model.OCODE.md" {
		t.Fatalf("ModelPrompt path = %q, want beta-model.OCODE.md (session model, not the global alpha)", got)
	}
}

// TestPushSessionStatusSnapshotRecomputesModelPrompt covers the SSE push the web
// relies on for an immediate sidebar update after PUT /api/sessions/:id/model
// (the poll path in HandleSessionStatus is covered above).
func TestPushSessionStatusSnapshotRecomputesModelPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := testHandlerWithConfig(t)
	root := t.TempDir()
	for name, body := range map[string]string{
		"alpha-model.OCODE.md": "alpha conduct",
		"beta-model.OCODE.md":  "beta conduct",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	h.mu.Lock()
	h.cfg.Model = "opencode-go/alpha-model"
	h.workDir = root
	h.mu.Unlock()

	id := session.NewSessionID()
	saveSessionToDir(t, root, id)
	h.sessions.Register(id, root)

	// HandleSetSessionModel pushes the session-tagged "status" event the web
	// relies on; subscribe first so it is captured.
	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)
	if rec := setSessionModel(t, h, id, "opencode-go/beta-model"); rec.Code != http.StatusOK {
		t.Fatalf("set session model %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	select {
	case ev := <-sub:
		snap, ok := ev.Data.(TUIStatus)
		if !ok {
			t.Fatalf("status data type = %T, want TUIStatus", ev.Data)
		}
		if snap.MainModel != "opencode-go/beta-model" {
			t.Fatalf("MainModel = %q, want session override", snap.MainModel)
		}
		if snap.ModelPrompt == nil || filepath.Base(snap.ModelPrompt.Path) != "beta-model.OCODE.md" {
			t.Fatalf("ModelPrompt = %+v, want beta-model.OCODE.md", snap.ModelPrompt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no status event broadcast")
	}
}
