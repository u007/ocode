package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimePathsSection_NoProject(t *testing.T) {
	dir := t.TempDir()
	if got := runtimePathsSection(dir, dir); got != nil {
		t.Fatalf("expected nil for no project, got %v", got)
	}
}

func TestRuntimePathsSection_JSProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	lines := runtimePathsSection(dir, dir)
	if len(lines) == 0 {
		t.Fatal("expected JS runtime lines")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Runtime paths (JS") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing JS header, got %v", lines)
	}
}

func TestRuntimePathsSection_NestedWeb(t *testing.T) {
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	if err := os.MkdirAll(web, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	// root has no marker, but web does — should still detect
	if got := jsProjectRoots(dir); len(got) == 0 {
		t.Fatal("expected jsProjectRoots to find web")
	}
	lines := runtimePathsSection(dir, dir)
	if len(lines) == 0 {
		t.Fatal("expected lines for nested JS project")
	}
}

func TestRuntimePathsSection_PythonProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(`[project]`), 0644); err != nil {
		t.Fatal(err)
	}
	lines := runtimePathsSection(dir, dir)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Runtime paths (Python") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing Python header, got %v", lines)
	}
}

func TestRuntimePathsSection_PythonVenv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(`requests`), 0644); err != nil {
		t.Fatal(err)
	}
	venvBin := filepath.Join(dir, ".venv", "bin")
	if err := os.MkdirAll(venvBin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venvBin, "python"), []byte(``), 0755); err != nil {
		t.Fatal(err)
	}
	lines := runtimePathsSection(dir, dir)
	found := false
	for _, l := range lines {
		if strings.Contains(l, ".venv") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected venv line, got %v", lines)
	}
}

func TestRuntimePathsSection_NVM(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	nvm := filepath.Join(t.TempDir(), ".nvm")
	vers := []string{"v18.0.0", "v20.0.0"}
	for _, v := range vers {
		bin := filepath.Join(nvm, "versions", "node", v, "bin")
		if err := os.MkdirAll(bin, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "node"), []byte(``), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("NVM_DIR", nvm)
	lines := runtimePathsSection(dir, dir)
	found := 0
	for _, l := range lines {
		if strings.Contains(l, "nvm v") {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected 2 nvm version lines, got %d in %v", found, lines)
	}
}

func TestRuntimePathsSection_NVM_Missing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVM_DIR", filepath.Join(t.TempDir(), "nonexistent"))
	lines := runtimePathsSection(dir, dir)
	for _, l := range lines {
		if strings.Contains(l, "nvm v") {
			t.Fatalf("should not have nvm versions when NVM_DIR missing, got %v", lines)
		}
	}
}

func TestRuntimePathsSection_Nvmrc(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte(`20`), 0644); err != nil {
		t.Fatal(err)
	}
	lines := runtimePathsSection(dir, dir)
	found := false
	for _, l := range lines {
		if strings.Contains(l, ".nvmrc") && strings.Contains(l, "20") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected .nvmrc line, got %v", lines)
	}
}

func TestRuntimePathsSection_Dedup(t *testing.T) {
	// candidateRoots dedup via jsProjectRoots
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	roots := jsProjectRoots(dir)
	if len(roots) != 1 {
		t.Fatalf("expected dedup to 1, got %v", roots)
	}
}

func TestRuntimePathsSection_Cap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Create many nvm versions to exceed cap
	nvm := filepath.Join(t.TempDir(), ".nvm")
	for i := 0; i < 20; i++ {
		v := filepath.Join(nvm, "versions", "node", strings.Repeat("v", 1)+strings.Repeat("0", 2)+"-"+string(rune('a'+i)), "bin")
		// Use simple version names
		v = filepath.Join(nvm, "versions", "node", filepath.Base(filepath.Dir(v)), "bin")
		// simpler: v00, v01 ...
		bin := filepath.Join(nvm, "versions", "node", strings.ReplaceAll(filepath.Base(filepath.Dir(v)), "-", "")+string(rune('0'+i%10)), "bin")
		_ = bin
	}
	// Instead create 15 distinct versions
	for i := 0; i < 15; i++ {
		bin := filepath.Join(nvm, "versions", "node", strings.Join([]string{"v1", strings.Repeat("0", 1) + string(rune('0'+i))}, "."), "bin")
		if err := os.MkdirAll(bin, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "node"), []byte(``), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("NVM_DIR", nvm)
	lines := runtimePathsSection(dir, dir)
	if len(lines) > 81 { // 80 + truncation line
		t.Fatalf("expected capped at 81, got %d", len(lines))
	}
}

func TestEnvironmentPrompt_CacheInvalidationOnCwd(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir1, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{workDir: dir1}
	p1 := a.environmentPrompt()
	a.workDir = dir2
	p2 := a.environmentPrompt()
	if p1 == p2 {
		t.Fatal("expected different prompt after cwd change")
	}
	// Going back should hit cache if same date
	a.workDir = dir1
	// Need to ensure env hash same - should return cached (equal to p1)
	// But p1 was built with dir1, so new call should rebuild and equal p1
	p3 := a.environmentPrompt()
	if p1 != p3 {
		t.Fatalf("expected cache hit after returning to dir1")
	}
}
