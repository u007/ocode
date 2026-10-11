package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeOcodeConfigFileForTest writes a raw ocodeconfig.json into an isolated HOME
// and returns nothing, so a test can assert how LoadOcodeConfig resolves it.
func writeOcodeConfigFileForTest(t *testing.T, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ocodeconfig.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSpeechSummaryEnabledDefaultsOn pins the one thing that differs from every
// sibling gate: a fresh install summarises before speaking. Every other
// *_enabled gate defaults false, so a copy-paste of their handling would ship
// this feature silently OFF for everyone.
func TestSpeechSummaryEnabledDefaultsOn(t *testing.T) {
	setHomeTree(t, t.TempDir())
	cfg := defaultOcodeConfig()
	if !cfg.SpeechSummaryEnabled {
		t.Fatal("defaultOcodeConfig must enable speech summaries")
	}
	if cfg.SpeechSummaryModel != "" {
		t.Fatalf("default model must be empty (falls back to small then main), got %q", cfg.SpeechSummaryModel)
	}
}

// TestLoadOcodeConfigReferencingAbsentSpeechSummaryKeepsDefaultOn is the
// regression for a real bug: the load apply was first written inside the
// `context_model` presence guard, so the fields were only read when the file
// happened to contain a context_model key. A file that mentions neither must
// still keep the default.
func TestLoadOcodeConfigReferencingAbsentSpeechSummaryKeepsDefaultOn(t *testing.T) {
	setHomeTree(t, t.TempDir())
	writeOcodeConfigFileForTest(t, `{"model":"openai/gpt-5"}`)

	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if !cfg.Ocode.SpeechSummaryEnabled {
		t.Error("an absent speech_summary_enabled key must keep the TRUE default")
	}
	if cfg.Ocode.SpeechSummaryModel != "" {
		t.Errorf("an absent speech_summary_model must stay empty, got %q", cfg.Ocode.SpeechSummaryModel)
	}
}

// TestLoadOcodeConfigHonoursExplicitSpeechSummaryValues is the other half: a
// file that DOES set them wins, including the explicit false that must be able
// to turn the default off.
func TestLoadOcodeConfigHonoursExplicitSpeechSummaryValues(t *testing.T) {
	setHomeTree(t, t.TempDir())
	writeOcodeConfigFileForTest(t,
		`{"speech_summary_model":"anthropic/claude-haiku-4-5","speech_summary_enabled":false}`)

	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if cfg.Ocode.SpeechSummaryModel != "anthropic/claude-haiku-4-5" {
		t.Errorf("model = %q", cfg.Ocode.SpeechSummaryModel)
	}
	if cfg.Ocode.SpeechSummaryEnabled {
		t.Error("an explicit false must switch the default off")
	}
}

// TestSaveOcodeSpeechSummaryPersistsBothFields covers the settings/sidebar
// write path end to end: what SaveOcodeSpeechSummary writes must survive a
// reload, so the control and the runtime agree.
func TestSaveOcodeSpeechSummaryPersistsBothFields(t *testing.T) {
	setHomeTree(t, t.TempDir())
	model := "openai/gpt-4o-mini"
	enabled := false
	if _, _, err := SaveOcodeSpeechSummary(&model, &enabled); err != nil {
		t.Fatalf("SaveOcodeSpeechSummary: %v", err)
	}

	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if cfg.Ocode.SpeechSummaryModel != "openai/gpt-4o-mini" || cfg.Ocode.SpeechSummaryEnabled {
		t.Fatalf("round trip lost values: model=%q enabled=%v", cfg.Ocode.SpeechSummaryModel, cfg.Ocode.SpeechSummaryEnabled)
	}

	// And explicitly re-enabling with a cleared model must round-trip too.
	cleared := ""
	on := true
	if _, _, err := SaveOcodeSpeechSummary(&cleared, &on); err != nil {
		t.Fatalf("SaveOcodeSpeechSummary: %v", err)
	}
	cfg = Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if cfg.Ocode.SpeechSummaryModel != "" || !cfg.Ocode.SpeechSummaryEnabled {
		t.Fatalf("clearing the model must persist: model=%q enabled=%v", cfg.Ocode.SpeechSummaryModel, cfg.Ocode.SpeechSummaryEnabled)
	}
}

// TestSaveOcodeSpeechSummaryMergesAPartialWrite is the regression for the
// overwrite bug: a gate-only write must leave the persisted model alone and a
// model-only write must leave the persisted gate alone, even though each body
// carries one key. The merge is against the on-disk config, so it also survives
// a value changed out of band between the two writes.
func TestSaveOcodeSpeechSummaryMergesAPartialWrite(t *testing.T) {
	setHomeTree(t, t.TempDir())

	model := "anthropic/claude-haiku-4-5"
	on := true
	if _, _, err := SaveOcodeSpeechSummary(&model, &on); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Gate-only write (the sidebar checkbox): the model must survive.
	off := false
	gotModel, gotEnabled, err := SaveOcodeSpeechSummary(nil, &off)
	if err != nil {
		t.Fatalf("gate-only save: %v", err)
	}
	if gotModel != "anthropic/claude-haiku-4-5" || gotEnabled {
		t.Fatalf("merged result = (%q, %v), want the model kept and the gate off", gotModel, gotEnabled)
	}

	// Model-only write (the picker): the gate must stay off.
	picked := "openai/gpt-4o-mini"
	gotModel, gotEnabled, err = SaveOcodeSpeechSummary(&picked, nil)
	if err != nil {
		t.Fatalf("model-only save: %v", err)
	}
	if gotModel != "openai/gpt-4o-mini" || gotEnabled {
		t.Fatalf("merged result = (%q, %v), want the picked model and the gate still off", gotModel, gotEnabled)
	}

	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if cfg.Ocode.SpeechSummaryModel != "openai/gpt-4o-mini" || cfg.Ocode.SpeechSummaryEnabled {
		t.Fatalf("disk after partial writes = model=%q enabled=%v", cfg.Ocode.SpeechSummaryModel, cfg.Ocode.SpeechSummaryEnabled)
	}
}

// TestSpeechSummaryDeltaIsNotEmptyAndApplies guards the profile/delta layer:
// without the ProfileOverrideCount entries a profile carrying ONLY these two
// fields counted as empty and was treated as "no overrides", so it could never
// set them. The apply half goes through EffectiveOcodeConfig, which is what the
// runtime actually consults.
func TestSpeechSummaryDeltaIsNotEmptyAndApplies(t *testing.T) {
	setHomeTree(t, t.TempDir())

	model := "anthropic/claude-haiku-4-5"
	enabled := false
	delta := ProfileDelta{SpeechSummaryModel: &model, SpeechSummaryEnabled: &enabled}
	if n := ProfileOverrideCount(delta); n != 2 {
		t.Fatalf("ProfileOverrideCount = %d, want 2", n)
	}

	base := defaultOcodeConfig()
	base.SpeechSummaryModel = "base/keep-me"
	base.SpeechSummaryEnabled = true
	base.Profiles = map[string]ProfileDelta{"speechy": delta}
	eff := EffectiveOcodeConfig(&base, "speechy")
	if eff.SpeechSummaryModel != model {
		t.Errorf("profile model did not apply: %q", eff.SpeechSummaryModel)
	}
	if eff.SpeechSummaryEnabled {
		t.Error("profile must be able to switch the default-true gate OFF")
	}
	// The base must not be mutated by resolving a profile.
	if base.SpeechSummaryModel != "base/keep-me" || !base.SpeechSummaryEnabled {
		t.Errorf("EffectiveOcodeConfig mutated its base: %+v", base)
	}
}
