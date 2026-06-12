package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

// gitInit sets up a throwaway repo with an initial commit of the named files
// and chdirs into it for the duration of the test.
func gitInit(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("config", "commit.gpgsign", "false")

	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		run("add", name)
	}
	if len(files) > 0 {
		run("commit", "-q", "-m", "initial")
	}
	return dir
}

func TestReadContextFile_TrackedAndDirtyReturnsHEAD(t *testing.T) {
	gitInit(t, map[string]string{"AGENTS.md": "committed body\n"})
	// Mutate the working tree copy.
	if err := os.WriteFile("AGENTS.md", []byte("working tree edits\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, ok := readContextFile("AGENTS.md")
	if !ok {
		t.Fatal("readContextFile returned !ok for tracked file")
	}
	if got != "committed body\n" {
		t.Fatalf("expected HEAD body, got %q", got)
	}
}

func TestReadContextFile_UntrackedReturnsWorkingTree(t *testing.T) {
	gitInit(t, nil)
	if err := os.WriteFile("AGENTS.md", []byte("untracked body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, ok := readContextFile("AGENTS.md")
	if !ok {
		t.Fatal("readContextFile returned !ok for untracked file")
	}
	if got != "untracked body\n" {
		t.Fatalf("expected working tree body, got %q", got)
	}
}

func TestReadContextFile_TrackedCleanReturnsWorkingTree(t *testing.T) {
	gitInit(t, map[string]string{"AGENTS.md": "committed clean\n"})
	got, ok := readContextFile("AGENTS.md")
	if !ok {
		t.Fatal("readContextFile returned !ok for tracked clean file")
	}
	if got != "committed clean\n" {
		t.Fatalf("expected committed body, got %q", got)
	}
}

// isolateHome redirects os.UserHomeDir() (and the Windows APPDATA branch
// in globalOcodeDir) to a fresh temp dir for the duration of the test.
// Without this, a developer's real ~/.config/opencode/*.OCODE.md files
// could leak into LoadModelContext results and make assertions non-hermetic.
func isolateHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", h)
	}
	return h
}

func TestLoadModelContext_EmptyNameReturnsEmpty(t *testing.T) {
	isolateHome(t)
	gitInit(t, map[string]string{"deepseek-v4-flash.OCODE.md": "body"})
	if got := LoadModelContext(""); got != "" {
		t.Fatalf("expected empty string for empty model name, got %q", got)
	}
	if got := LoadModelContext("   "); got != "" {
		t.Fatalf("expected empty string for whitespace model name, got %q", got)
	}
}

func TestLoadModelContext_StemMatchFromProjectRoot(t *testing.T) {
	isolateHome(t)
	gitInit(t, map[string]string{
		"deepseek-v4-flash.OCODE.md": "ROOT_BODY\n",
	})
	got := LoadModelContext("deepseek-v4-flash")
	if !strings.Contains(got, "ROOT_BODY") {
		t.Fatalf("expected ROOT_BODY in result, got %q", got)
	}
	if !strings.Contains(got, "deepseek-v4-flash.OCODE.md") {
		t.Fatalf("expected filename in result framing, got %q", got)
	}
}

func TestLoadModelContext_CaseInsensitiveStem(t *testing.T) {
	isolateHome(t)
	gitInit(t, map[string]string{
		"deepseek-v4-flash.OCODE.md": "lowercase stem file\n",
	})
	// Loader must treat stems case-insensitively. Asking with mixed case
	// should pick up the lowercase file.
	got := LoadModelContext("DeepSeek-V4-Flash")
	if !strings.Contains(got, "lowercase stem file") {
		t.Fatalf("case-insensitive stem match failed, got %q", got)
	}
}

func TestLoadModelContext_SiblingStemIsIgnored(t *testing.T) {
	isolateHome(t)
	gitInit(t, map[string]string{
		"deepseek-v4-flash.OCODE.md": "FLASH_BODY\n",
		"deepseek-v4-pro.OCODE.md":   "PRO_BODY\n",
	})
	got := LoadModelContext("deepseek-v4-flash")
	if strings.Contains(got, "PRO_BODY") {
		t.Fatalf("pro body leaked into flash result: %q", got)
	}
	if !strings.Contains(got, "FLASH_BODY") {
		t.Fatalf("expected flash body, got %q", got)
	}
}

func TestLoadModelContext_NoMatchReturnsEmpty(t *testing.T) {
	isolateHome(t)
	gitInit(t, map[string]string{
		"deepseek-v4-pro.OCODE.md": "PRO_BODY\n",
	})
	if got := LoadModelContext("deepseek-v4-flash"); got != "" {
		t.Fatalf("expected empty string when no matching file, got %q", got)
	}
}

func TestLoadModelContext_TrackedDirtyReturnsHEAD(t *testing.T) {
	isolateHome(t)
	// Commit a clean version, then dirty the working tree. The loader must
	// return the HEAD version (git-stable-version rule) — not the dirty copy.
	gitInit(t, map[string]string{
		"deepseek-v4-flash.OCODE.md": "HEAD_BODY\n",
	})
	if err := os.WriteFile("deepseek-v4-flash.OCODE.md", []byte("WORKING_TREE_EDITS\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := LoadModelContext("deepseek-v4-flash")
	if strings.Contains(got, "WORKING_TREE_EDITS") {
		t.Fatalf("dirty working tree content leaked into loader output: %q", got)
	}
	if !strings.Contains(got, "HEAD_BODY") {
		t.Fatalf("expected HEAD body, got %q", got)
	}
}

func TestLoadModelContext_OpencodeDirSearch(t *testing.T) {
	isolateHome(t)
	// No file in project root, but one in .opencode/ — should still be found.
	gitInit(t, nil)
	if err := os.MkdirAll(".opencode", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".opencode", "deepseek-v4-flash.OCODE.md"),
		[]byte("OPENCODE_BODY\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := LoadModelContext("deepseek-v4-flash")
	if !strings.Contains(got, "OPENCODE_BODY") {
		t.Fatalf("expected .opencode body, got %q", got)
	}
}

func TestLoadModelContext_ProjectRootWinsOverOpencodeDir(t *testing.T) {
	isolateHome(t)
	// Both project root and .opencode/ have the same stem — root wins.
	gitInit(t, map[string]string{
		"deepseek-v4-flash.OCODE.md": "ROOT_WINS\n",
	})
	if err := os.MkdirAll(".opencode", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".opencode", "deepseek-v4-flash.OCODE.md"),
		[]byte("OPENCODE_LOSES\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := LoadModelContext("deepseek-v4-flash")
	if strings.Contains(got, "OPENCODE_LOSES") {
		t.Fatalf(".opencode/ content should not be returned when project root has the same stem: %q", got)
	}
	if !strings.Contains(got, "ROOT_WINS") {
		t.Fatalf("expected project root body, got %q", got)
	}
}

// --- Wildcard stem tests --------------------------------------------------
// Wildcard syntax: a stem ending in a single trailing '*' (e.g.
// `minimax-m*`) matches any model whose id starts with the stem's prefix.
// A bare '*' stem is rejected to prevent an accidental catch-all.

func TestLoadModelContext_WildcardStemMatchesPrefix(t *testing.T) {
	isolateHome(t)
	gitInit(t, map[string]string{
		"minimax-m*.OCODE.md": "WILDCARD_BODY\n",
	})
	// Wildcard stem should match several sibling models in the family.
	for _, m := range []string{"minimax-m2", "minimax-m2.1", "minimax-m2.5", "minimax-m2.7", "minimax-m3"} {
		got := LoadModelContext(m)
		if !strings.Contains(got, "WILDCARD_BODY") {
			t.Fatalf("wildcard stem failed to match model %q: got %q", m, got)
		}
	}
}

func TestLoadModelContext_WildcardStemDoesNotMatchUnrelated(t *testing.T) {
	isolateHome(t)
	gitInit(t, map[string]string{
		"minimax-m*.OCODE.md": "WILDCARD_BODY\n",
	})
	// These models share no prefix with the wildcard stem.
	for _, m := range []string{"claude-sonnet-4-6", "deepseek-v4-flash", "kimi-k2.5", "minimax"} {
		got := LoadModelContext(m)
		if got != "" {
			t.Fatalf("wildcard stem should not match %q, got %q", m, got)
		}
	}
}

func TestLoadModelContext_BareStarIsRejected(t *testing.T) {
	isolateHome(t)
	// A bare '*.OCODE.md' must not match anything — otherwise the project-
	// root-wins rule would silently shadow every real model-specific file.
	gitInit(t, map[string]string{
		"*.OCODE.md": "CATCH_ALL\n",
	})
	for _, m := range []string{"claude-sonnet-4-6", "deepseek-v4-flash", "minimax-m2.5"} {
		got := LoadModelContext(m)
		if got != "" {
			t.Fatalf("bare '*' stem should not match %q, got %q", m, got)
		}
	}
}

func TestLoadModelContext_OnlyTrailingStarIsWildcard(t *testing.T) {
	isolateHome(t)
	// A '*' in the middle of the stem is literal, not a wildcard. So
	// 'minimax-*.5' is a literal stem that only matches an exact model
	// 'minimax-*.5' (which doesn't exist), and must not match 'minimax-m2.5'.
	gitInit(t, map[string]string{
		"minimax-*.5.OCODE.md": "INTERNAL_STAR\n",
	})
	got := LoadModelContext("minimax-m2.5")
	if got != "" {
		t.Fatalf("internal '*' should not act as a wildcard, got %q", got)
	}
}

func TestLoadModelContext_ExactBeatsWildcardInSameDir(t *testing.T) {
	isolateHome(t)
	// Both a wildcard and an exact file for the same model in the same dir.
	// Exact must win — otherwise the same content would be loaded twice.
	gitInit(t, map[string]string{
		"minimax-m*.OCODE.md":   "WILDCARD_BODY\n",
		"minimax-m2.5.OCODE.md": "EXACT_BODY\n",
	})
	got := LoadModelContext("minimax-m2.5")
	if !strings.Contains(got, "EXACT_BODY") {
		t.Fatalf("exact match body missing, got %q", got)
	}
	if strings.Contains(got, "WILDCARD_BODY") {
		t.Fatalf("wildcard body should not appear when an exact match exists in the same dir, got %q", got)
	}
	// Sibling models without an exact file should still hit the wildcard.
	got2 := LoadModelContext("minimax-m2.7")
	if !strings.Contains(got2, "WILDCARD_BODY") {
		t.Fatalf("sibling model should fall back to wildcard, got %q", got2)
	}
}

func TestLoadModelContext_BundledFallbackReturnsContent(t *testing.T) {
	isolateHome(t)
	gitInit(t, map[string]string{})
	// Register an in-memory embedded FS matching deepseek-v4-flash.
	fsys := fstest.MapFS{
		"deepseek-v4-flash.OCODE.md": &fstest.MapFile{
			Data: []byte("EMBEDDED_BODY\n"),
		},
	}
	SetBundledModelConfigFS(fsys)
	t.Cleanup(func() { SetBundledModelConfigFS(nil) })

	got := LoadModelContext("deepseek-v4-flash")
	if !strings.Contains(got, "EMBEDDED_BODY") {
		t.Fatalf("expected embedded body in result, got %q", got)
	}
	if !strings.Contains(got, "deepseek-v4-flash.OCODE.md") {
		t.Fatalf("expected filename in result framing, got %q", got)
	}
}

func TestLoadModelContext_BundledFallbackDiskWins(t *testing.T) {
	isolateHome(t)
	// Disk file exists for the model.
	gitInit(t, map[string]string{
		"deepseek-v4-flash.OCODE.md": "DISK_BODY\n",
	})
	// Also register an embedded file — disk should win.
	fsys := fstest.MapFS{
		"deepseek-v4-flash.OCODE.md": &fstest.MapFile{
			Data: []byte("EMBEDDED_BODY\n"),
		},
	}
	SetBundledModelConfigFS(fsys)
	t.Cleanup(func() { SetBundledModelConfigFS(nil) })

	got := LoadModelContext("deepseek-v4-flash")
	if !strings.Contains(got, "DISK_BODY") {
		t.Fatalf("expected DISK_BODY (disk wins), got %q", got)
	}
	if strings.Contains(got, "EMBEDDED_BODY") {
		t.Fatalf("embedded body should not appear when disk file exists, got %q", got)
	}
}

func TestLoadModelContext_WildcardProjectRootWinsOverOpencodeDirExact(t *testing.T) {
	isolateHome(t)
	// Project root has a wildcard, .opencode/ has an exact file for the
	// same model. Per the project-root-wins rule, the wildcard must be
	// returned — exact in a deeper dir does not beat wildcard in the root.
	gitInit(t, map[string]string{
		"minimax-m*.OCODE.md": "ROOT_WILDCARD\n",
	})
	if err := os.MkdirAll(".opencode", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".opencode", "minimax-m2.5.OCODE.md"),
		[]byte("OPENCODE_EXACT\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := LoadModelContext("minimax-m2.5")
	if !strings.Contains(got, "ROOT_WILDCARD") {
		t.Fatalf("project root wildcard should win, got %q", got)
	}
	if strings.Contains(got, "OPENCODE_EXACT") {
		t.Fatalf(".opencode/ exact should not beat project root wildcard, got %q", got)
	}
}
