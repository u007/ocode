package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGitConfigPullFlag(t *testing.T) {
	cases := []struct {
		name string
		cfg  GitConfig
		want string
	}{
		// An in-memory config from an older caller has no strategy at all; it
		// must still produce an explicit flag (the whole point of the setting).
		{"unset defaults to rebase", GitConfig{}, "--rebase"},
		{"explicit rebase", GitConfig{PullStrategy: GitPullStrategyRebase}, "--rebase"},
		{"merge means no-rebase", GitConfig{PullStrategy: GitPullStrategyMerge}, "--no-rebase"},
		{"whitespace is ignored", GitConfig{PullStrategy: "  rebase  "}, "--rebase"},
		// Unknown values are rejected at load time; PullFlag alone must never
		// hand git an empty/typo flag.
		{"unknown falls back to rebase", GitConfig{PullStrategy: "ff-only"}, "--rebase"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.PullFlag(); got != tc.want {
				t.Fatalf("PullFlag() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGitConfigValidate(t *testing.T) {
	for _, cfg := range []GitConfig{
		{},
		{PullStrategy: GitPullStrategyRebase},
		{PullStrategy: GitPullStrategyMerge},
	} {
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate(%+v) = %v, want nil", cfg, err)
		}
	}
	if err := (GitConfig{PullStrategy: "fast-forward"}).Validate(); err == nil {
		t.Fatal("Validate accepted an unknown pull strategy")
	}
}

func TestGitConfigDefaultIsRebase(t *testing.T) {
	if got := defaultOcodeConfig().Git.PullStrategy; got != GitPullStrategyRebase {
		t.Fatalf("default pull strategy = %q, want %q", got, GitPullStrategyRebase)
	}
}

func TestSaveOcodeGitConfigRoundTripAndOnDiskShape(t *testing.T) {
	chdirTempForConfigTest(t)
	home := t.TempDir()
	setHomeTree(t, home)

	// Default: nothing is written, so an untouched install gains no key.
	if err := SaveOcodeGitConfig(GitConfig{}); err != nil {
		t.Fatalf("SaveOcodeGitConfig(default) failed: %v", err)
	}
	configPath := filepath.Join(home, ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}
	if _, ok := parsed["git"]; ok {
		t.Fatalf("git block written for the rebase default: %s", data)
	}

	// Persist merge and read it back through the loader.
	if err := SaveOcodeGitConfig(GitConfig{PullStrategy: GitPullStrategyMerge}); err != nil {
		t.Fatalf("SaveOcodeGitConfig(merge) failed: %v", err)
	}
	loaded, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy failed: %v", err)
	}
	if got := loaded.Git.PullStrategy; got != GitPullStrategyMerge {
		t.Fatalf("loaded pull strategy = %q, want %q", got, GitPullStrategyMerge)
	}
	if got := loaded.Git.PullFlag(); got != "--no-rebase" {
		t.Fatalf("loaded PullFlag() = %q, want --no-rebase", got)
	}

	// Write the block with a plain merge so the store test above is not the
	// only proof the on-disk key is named as documented.
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}
	block, ok := parsed["git"].(map[string]any)
	if !ok {
		t.Fatalf("git block missing from saved config: %s", data)
	}
	if got := block["pull_strategy"]; got != GitPullStrategyMerge {
		t.Fatalf("saved git.pull_strategy = %v, want %q", got, GitPullStrategyMerge)
	}

	// Switching back to rebase removes the key again rather than leaving a
	// stale merge preference behind.
	if err := SaveOcodeGitConfig(GitConfig{PullStrategy: GitPullStrategyRebase}); err != nil {
		t.Fatalf("SaveOcodeGitConfig(rebase) failed: %v", err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	parsed = nil
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}
	if _, ok := parsed["git"]; ok {
		t.Fatalf("git block survived a switch back to rebase: %s", data)
	}
}

func TestLoadOcodeConfigRejectsUnknownGitPullStrategy(t *testing.T) {
	chdirTempForConfigTest(t)
	home := t.TempDir()
	setHomeTree(t, home)

	path := filepath.Join(home, ".config", "opencode", "ocodeconfig.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"git":{"pull_strategy":"fast-forward"}}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadOcodeConfigCopy(); err == nil {
		t.Fatal("LoadOcodeConfigCopy accepted git.pull_strategy=fast-forward")
	}
}
