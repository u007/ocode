package config

import "testing"

func TestDefaultDiscoveryConfig(t *testing.T) {
	d := defaultDiscoveryConfig()
	if d.Enabled {
		t.Fatalf("discovery must default to disabled")
	}
	if d.EmbeddingModel != "" {
		t.Fatalf("embedding_model must default empty (no implicit vendor), got %q", d.EmbeddingModel)
	}
	if len(d.PinnedSkills) == 0 {
		t.Fatalf("pinned skills must seed defaults")
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
	cfg.Discovery.PinnedSkills = []string{"brainstorming"}

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
		len(got.Discovery.PinnedSkills) != 1 {
		t.Fatalf("round-trip mismatch: %+v", got.Discovery)
	}
}
