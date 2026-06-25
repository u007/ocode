package config

import (
	"os"
	"testing"
)

func TestDefaultDiscoveryConfig(t *testing.T) {
	d := defaultDiscoveryConfig()
	if d.Enabled {
		t.Fatalf("discovery must default to disabled")
	}
	if d.EmbeddingModel != "" {
		t.Fatalf("embedding_model must default empty (no implicit vendor), got %q", d.EmbeddingModel)
	}
	if d.LocalServerURL != "" {
		t.Fatalf("local_server_url must default empty (no implicit server), got %q", d.LocalServerURL)
	}
	if len(d.PinnedSkills) == 0 {
		t.Fatalf("pinned skills must seed defaults")
	}
	wantIgnores := DefaultDiscoveryIgnorePaths()
	if len(d.IgnorePaths) != len(wantIgnores) {
		t.Fatalf("default ignore paths mismatch: got %v want %v", d.IgnorePaths, wantIgnores)
	}
	for i, want := range wantIgnores {
		if d.IgnorePaths[i] != want {
			t.Fatalf("default ignore path %d = %q, want %q", i, d.IgnorePaths[i], want)
		}
	}
}

func TestDiscoveryConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/ocodeconfig.json"

	cfg := defaultOcodeConfig()
	cfg.Discovery.Enabled = true
	cfg.Discovery.EmbeddingModel = "openai/text-embedding-3-small"
	cfg.Discovery.EmbeddingBackend = "http"
	cfg.Discovery.LocalModelStatus = "none"
	cfg.Discovery.LocalServerURL = "http://localhost:1234"
	cfg.Discovery.PinnedSkills = []string{"brainstorming"}
	cfg.Discovery.IgnorePaths = append(DefaultDiscoveryIgnorePaths(), "tmp/")

	if err := writeOcodeConfigFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	got := defaultOcodeConfig()
	if err := loadOcodeConfigFile(path, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Discovery.Enabled ||
		got.Discovery.EmbeddingModel != "openai/text-embedding-3-small" ||
		got.Discovery.EmbeddingBackend != "http" ||
		got.Discovery.LocalServerURL != "http://localhost:1234" ||
		len(got.Discovery.PinnedSkills) != 1 ||
		len(got.Discovery.IgnorePaths) != len(DefaultDiscoveryIgnorePaths())+1 {
		t.Fatalf("round-trip mismatch: %+v", got.Discovery)
	}
}

func TestDiscoveryIgnorePathsMergeDefaultsOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/ocodeconfig.json"
	if err := os.WriteFile(path, []byte(`{"discovery":{"ignore_paths":["tmp/"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := defaultOcodeConfig()
	if err := loadOcodeConfigFile(path, &got); err != nil {
		t.Fatal(err)
	}
	want := append(DefaultDiscoveryIgnorePaths(), "tmp/")
	if len(got.Discovery.IgnorePaths) != len(want) {
		t.Fatalf("ignore paths = %v, want %v", got.Discovery.IgnorePaths, want)
	}
	for i, path := range want {
		if got.Discovery.IgnorePaths[i] != path {
			t.Fatalf("ignore path %d = %q, want %q", i, got.Discovery.IgnorePaths[i], path)
		}
	}
}
