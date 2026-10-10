package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateConfigEnv points BOTH the XDG and HOME config roots at a temp dir.
//
// paths.GlobalConfigDir checks XDG_CONFIG_HOME BEFORE falling back to
// $HOME/.config, so overriding HOME alone is not enough: on a machine where
// XDG_CONFIG_HOME is already set, the test would read the developer's real
// ocodeconfig.json and could pass or fail for reasons that have nothing to do
// with the code. Set both, and write the fixture into the XDG location that
// actually wins.
func isolateConfigEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	setHomeTree(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	dir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestJudgeModelConfigDefaults pins the invariant the whole per-slot selection
// rests on: every judge slot resolves to typesafe/jev-latest when nothing is
// configured. That string is the exact value of the three hardcoded constants
// this change deletes (discoveryJudgeModel, networkGuardJudgeModel,
// contentGuardJudgeModel), so an unconfigured install must be byte-identical.
//
// A slot defaulting to "" would be worse than a wrong default: resolveDecider
// turns a blank model into a nil client, and a nil client silently DISABLES the
// judge rather than falling back.
func TestJudgeModelConfigDefaults(t *testing.T) {
	cfg := defaultOcodeConfig()
	checks := []struct {
		name string
		got  string
	}{
		{"Discovery.JudgeModel", cfg.Discovery.JudgeModel},
		{"DocSearch.JudgeModel", cfg.DocSearch.JudgeModel},
		{"Search.JudgeModel", cfg.Search.JudgeModel},
		{"NetworkGuard.JudgeModel", cfg.NetworkGuard.JudgeModel},
		{"ContentGuard.JudgeModel", cfg.ContentGuard.JudgeModel},
	}
	for _, c := range checks {
		if c.got != "typesafe/jev-latest" {
			t.Errorf("%s = %q, want %q", c.name, c.got, "typesafe/jev-latest")
		}
	}
}

// TestJudgeModelConfigLoadsIndependently is the test that proves the point of
// this change: setting one slot must not move any other. All five are set to
// DIFFERENT values and each is read back, so a test that wired them to one shared
// field — the obvious way to implement this wrongly — would fail here rather than
// passing while proving nothing.
func TestJudgeModelConfigLoadsIndependently(t *testing.T) {
	chdirTempForConfigTest(t)
	configDir := isolateConfigEnv(t)
	body := `{
	  "discovery":     {"judge_model": "cloudflare-workers/@cf/cloudflare/clef-flash"},
	  "doc_search":    {"judge_model": "typesafe/jev-1.13.0"},
	  "search":        {"judge_model": "typesafe/jev-latest"},
	  "network_guard": {"judge_model": "cloudflare-workers/@cf/cloudflare/clef"},
	  "content_guard": {"judge_model": "typesafe/jev-1.13.0"}
	}`
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	want := map[string]string{
		"Discovery.JudgeModel":    "cloudflare-workers/@cf/cloudflare/clef-flash",
		"DocSearch.JudgeModel":    "typesafe/jev-1.13.0",
		"Search.JudgeModel":       "typesafe/jev-latest",
		"NetworkGuard.JudgeModel": "cloudflare-workers/@cf/cloudflare/clef",
		"ContentGuard.JudgeModel": "typesafe/jev-1.13.0",
	}
	got := map[string]string{
		"Discovery.JudgeModel":    cfg.Ocode.Discovery.JudgeModel,
		"DocSearch.JudgeModel":    cfg.Ocode.DocSearch.JudgeModel,
		"Search.JudgeModel":       cfg.Ocode.Search.JudgeModel,
		"NetworkGuard.JudgeModel": cfg.Ocode.NetworkGuard.JudgeModel,
		"ContentGuard.JudgeModel": cfg.Ocode.ContentGuard.JudgeModel,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
	// Guard the specific regression this change introduces: doc_search and
	// code_search used to be served by the SAME client as discovery, so a single
	// shared field would silently move all three.
	if cfg.Ocode.Discovery.JudgeModel == cfg.Ocode.DocSearch.JudgeModel {
		t.Error("discovery and doc_search resolve to the same value from different config keys — they are not independent")
	}
}

// TestJudgeModelConfigBlankKeepsDefault pins the other direction. A blank or
// whitespace-only judge_model must NOT clear the slot: that would silently
// disable a judge the user never touched. Disabling fails open (candidates are
// kept) so it is not a safety problem, but it is invisible and the user has no
// way to notice or undo it.
func TestJudgeModelConfigBlankKeepsDefault(t *testing.T) {
	chdirTempForConfigTest(t)
	configDir := isolateConfigEnv(t)
	body := `{
	  "discovery":  {"judge_model": ""},
	  "doc_search": {"judge_model": "   "}
	}`
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if got := cfg.Ocode.Discovery.JudgeModel; got != "typesafe/jev-latest" {
		t.Errorf("empty judge_model cleared the slot: Discovery.JudgeModel = %q", got)
	}
	if got := cfg.Ocode.DocSearch.JudgeModel; got != "typesafe/jev-latest" {
		t.Errorf("whitespace judge_model cleared the slot: DocSearch.JudgeModel = %q", got)
	}
}
