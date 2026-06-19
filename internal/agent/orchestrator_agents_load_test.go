package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOrchestratorAgentFilesParse(t *testing.T) {
	// The orchestrator .md files live at <repo>/.opencode/agents/.
	// When the test runs from internal/agent/, the init() loader can't find them
	// (it uses cwd = internal/agent/), so we test the parser directly: copy
	// the .md files into a temp dir and load via a fresh registry.
	repoRoot, err := findRepoRoot()
	if err != nil {
		wd, _ := os.Getwd()
		t.Skipf("repo root not found from cwd %s: %v", wd, err)
	}
	srcDir := filepath.Join(repoRoot, ".opencode", "agents")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", srcDir, err)
	}

	// Mirror files into a temp dir and load them.
	dstDir := t.TempDir()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dstDir, e.Name()), data, 0644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}

	// Build a fresh registry that searches the temp dir as its "project" location.
	reg := NewAgentRegistry()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		name := e.Name()[:len(e.Name())-len(".md")]
		def, _ := parseAgentFile(filepath.Join(dstDir, e.Name()))
		if def == nil {
			t.Errorf("%s: parseAgentFile returned nil", name)
			continue
		}
		if def.Mode != AgentModeSubagent {
			t.Errorf("%s: mode = %v, want subagent", name, def.Mode)
		}
		if def.SystemPrompt == "" {
			t.Errorf("%s: empty system prompt", name)
		}
		reg.addLoaded(*def)
	}

	for _, name := range []string{
		"orchestrator-planner",
		"orchestrator-explorer",
		"orchestrator-developer",
		"orchestrator-validator",
	} {
		def := reg.Get(name)
		if def == nil {
			t.Errorf("%s: not registered", name)
			continue
		}
		t.Logf("OK: %s — %d chars prompt, mode=%v", name, len(def.SystemPrompt), def.Mode)
	}
}

func TestOrchestratorAgentsSmallModelEligible(t *testing.T) {
	for _, name := range []string{"orchestrator-planner", "orchestrator-explorer"} {
		if !smallModelEligible(name) {
			t.Errorf("%s should be small-model eligible", name)
		}
	}
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
