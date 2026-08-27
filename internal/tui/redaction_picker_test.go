package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
)

// TestDefaultRedactionBaseURL_LocalModel verifies that a /localmodel-managed
// model gets its deterministic chat port as the tier-2 scanner base URL
// (with the /v1 suffix, since the scanner appends "/chat/completions").
func TestDefaultRedactionBaseURL_LocalModel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := config.SaveLocalModelConfig("local/bonsai-8b-1bit", true, 1, 50000); err != nil {
		t.Fatalf("SaveLocalModelConfig: %v", err)
	}
	// One registered id → first reserved port (11458).
	if got := defaultRedactionBaseURL("local/bonsai-8b-1bit"); got != "http://localhost:11458/v1" {
		t.Errorf("defaultRedactionBaseURL(local) = %q, want %q", got, "http://localhost:11458/v1")
	}
	// A second registered id sorts after the first → next port.
	if err := config.SaveLocalModelConfig("local/zeta-model", true, 1, 50000); err != nil {
		t.Fatalf("SaveLocalModelConfig: %v", err)
	}
	if got := defaultRedactionBaseURL("local/zeta-model"); got != "http://localhost:11459/v1" {
		t.Errorf("defaultRedactionBaseURL(sorted local) = %q, want %q", got, "http://localhost:11459/v1")
	}
	// Unregistered local model → no default base URL.
	if got := defaultRedactionBaseURL("local/never-registered"); got != "" {
		t.Errorf("defaultRedactionBaseURL(unregistered) = %q, want \"\"", got)
	}
}

// TestAppendRedactionModelLocalModels guards that the redaction-model picker
// lists only ENABLED registered local models, and that the helper is a no-op
// for other picker kinds.
func TestAppendRedactionModelLocalModels(t *testing.T) {
	cfg := config.Config{}
	cfg.Ocode.LocalModels = map[string]config.LocalModelConfig{
		"local/bonsai-8b-1bit": {Enabled: true, MaxParallel: 1},
		"local/disabled-model": {Enabled: false, MaxParallel: 1},
		"local/zeta-model":     {Enabled: true, MaxParallel: 1},
	}
	m := model{config: &cfg, pickerKind: "redaction-model"}
	m.appendRedactionModelLocalModels()

	joined := strings.Join(m.pickerValues, "\n")
	for _, want := range []string{"local/bonsai-8b-1bit", "local/zeta-model"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected enabled local model %q in picker values, got:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "local/disabled-model") {
		t.Errorf("disabled local model must not be listed, got:\n%s", joined)
	}

	// Header present, values line up with items.
	if len(m.pickerItems) != len(m.pickerValues) || len(m.pickerItems) != len(m.pickerIsHeader) {
		t.Fatalf("picker slices out of sync: items=%d values=%d headers=%d",
			len(m.pickerItems), len(m.pickerValues), len(m.pickerIsHeader))
	}
	foundHeader := false
	for i, item := range m.pickerItems {
		if item == "Local Models" {
			foundHeader = true
			if !m.pickerIsHeader[i] {
				t.Fatal("Local Models header not marked as header")
			}
		}
	}
	if !foundHeader {
		t.Fatal("expected \"Local Models\" section header")
	}

	// No-op for other picker kinds.
	other := model{config: &cfg, pickerKind: "model"}
	other.appendRedactionModelLocalModels()
	if len(other.pickerItems) != 0 {
		t.Errorf("appendRedactionModelLocalModels must be a no-op for kind=model, got %d items", len(other.pickerItems))
	}
}

// TestBuildLLMScanner_LocalModelRewritesServeID guards that a /localmodel
// MLX model's request name is rewritten to the id the server was launched
// with (ExpectedServeID), matching the main chat client — mlx_lm.server
// treats the body's "model" field as a live model-switch instruction, so the
// persisted "local/<id>" would otherwise fail the scan request.
func TestBuildLLMScanner_LocalModelRewritesServeID(t *testing.T) {
	man, ok := discovery.ChatManifestForHost("local/bonsai-8b-1bit")
	if !ok {
		t.Skip("no chat manifest for local/bonsai-8b-1bit on this host")
	}
	got := buildLLMScanner("http://localhost:11458/v1", "local/bonsai-8b-1bit", false)
	if got == nil {
		t.Fatal("expected non-nil scanner for local model")
	}
	if want := man.ExpectedServeID(); got.Model != want {
		t.Errorf("scanner Model = %q, want %q (server serve id)", got.Model, want)
	}
}

// TestSetRedactionModel_RefreshesAutoAssignedLocalURL guards the stale-port
// bug: switching between local models must recompute the auto-assigned base
// URL (model A's port must not survive a switch to model B), while an
// explicitly custom URL is preserved.
func TestSetRedactionModel_RefreshesAutoAssignedLocalURL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Register two local models so each has a deterministic chat port.
	if err := config.SaveLocalModelConfig("local/bonsai-8b-1bit", true, 1, 50000); err != nil {
		t.Fatalf("SaveLocalModelConfig: %v", err)
	}
	if err := config.SaveLocalModelConfig("local/zeta-model", true, 1, 50000); err != nil {
		t.Fatalf("SaveLocalModelConfig: %v", err)
	}

	cfg := config.Config{}
	m := model{config: &cfg}

	// First pick: bonsai sorts first → first reserved port (11458).
	if err := m.setRedactionModel("local/bonsai-8b-1bit"); err != nil {
		t.Fatalf("setRedactionModel(bonsai): %v", err)
	}
	if got := m.config.Ocode.Security.Redaction.BaseURL; got != "http://localhost:11458/v1" {
		t.Fatalf("base_url after first pick = %q, want %q", got, "http://localhost:11458/v1")
	}

	// Switch to the second local model: the auto-assigned URL must be
	// recomputed (11459), not keep bonsai's port.
	if err := m.setRedactionModel("local/zeta-model"); err != nil {
		t.Fatalf("setRedactionModel(zeta): %v", err)
	}
	if got := m.config.Ocode.Security.Redaction.BaseURL; got != "http://localhost:11459/v1" {
		t.Fatalf("base_url after switch = %q, want %q (stale-port bug)", got, "http://localhost:11459/v1")
	}

	// An explicitly custom URL is preserved across a local-model switch.
	m.config.Ocode.Security.Redaction.BaseURL = "http://192.168.1.50:9000/v1"
	m.config.Ocode.Security.Redaction.Model = "local/zeta-model"
	if err := m.setRedactionModel("local/bonsai-8b-1bit"); err != nil {
		t.Fatalf("setRedactionModel(bonsai, custom URL): %v", err)
	}
	if got := m.config.Ocode.Security.Redaction.BaseURL; got != "http://192.168.1.50:9000/v1" {
		t.Fatalf("custom base_url was overwritten: %q", got)
	}
}

// TestBuildLLMScanner_LocalModelNoManifestFailsClosed guards that a local/*
// model with no chat manifest for this host fails the scanner closed (nil) —
// sending the raw "local/<id>" to an MLX server would be a live model-switch
// instruction to a model it doesn't have.
func TestBuildLLMScanner_LocalModelNoManifestFailsClosed(t *testing.T) {
	if got := buildLLMScanner("http://localhost:11458/v1", "local/not-a-catalog-model", false); got != nil {
		t.Errorf("expected nil scanner for unresolvable local model, got %+v", got)
	}
}

// TestOpenRedactionModelPickerListsLocalModels verifies the end-to-end wiring:
// /mask model opens the picker as kind="redaction-model" with the enabled
// local-models section appended to the standard listing.
func TestOpenRedactionModelPickerListsLocalModels(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := config.Config{}
	cfg.Ocode.LocalModels = map[string]config.LocalModelConfig{
		"local/bonsai-8b-1bit": {Enabled: true, MaxParallel: 1},
	}
	m := model{config: &cfg, input: textarea.New()}
	m.openRedactionModelPicker()

	if m.pickerKind != "redaction-model" {
		t.Fatalf("pickerKind = %q, want %q", m.pickerKind, "redaction-model")
	}
	joined := strings.Join(m.pickerValues, "\n")
	if !strings.Contains(joined, "local/bonsai-8b-1bit") {
		t.Errorf("expected enabled local model in picker values, got:\n%s", joined)
	}
}
