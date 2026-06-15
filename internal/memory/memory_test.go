package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildSnapshotAndRender(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	project := t.TempDir()
	base, err := os.UserConfigDir()
	_ = base
	userPath := filepath.Join(home, ".local", "share", "opencode", "memory", "user.md")
	globalPath := filepath.Join(home, ".local", "share", "opencode", "memory", "global.md")
	projectPath := filepath.Join(home, ".local", "share", "opencode", "project", slugForPath(project), "memory.md")

	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("user pref one\nuser pref two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte("global lesson\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte("project history\nproject decision\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := Status(project)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(snap.User.Preview, "user pref one") {
		t.Fatalf("unexpected user preview: %q", snap.User.Preview)
	}
	if !strings.Contains(snap.Project.Preview, "project history") {
		t.Fatalf("unexpected project preview: %q", snap.Project.Preview)
	}
	if !strings.Contains(snap.Global.Preview, "global lesson") {
		t.Fatalf("unexpected global preview: %q", snap.Global.Preview)
	}

	status := RenderStatus(snap)
	for _, want := range []string{"Status: enabled", "Project memory", "User memory", "Global history"} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected %q in status:\n%s", want, status)
		}
	}
	if strings.Index(status, "Project memory") > strings.Index(status, "User memory") {
		t.Fatalf("expected project memory before user memory in status:\n%s", status)
	}

	prompt := RenderPrompt(snap)
	for _, want := range []string{"[ocode:memory]", "Layered memory context is enabled", "## Project memory"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in prompt:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, "## Project memory") > strings.Index(prompt, "## User memory") {
		t.Fatalf("expected project memory before user memory in prompt:\n%s", prompt)
	}
	if strings.Index(prompt, "--- Project memory (") > strings.Index(prompt, "--- User memory (") {
		t.Fatalf("expected project memory file before user memory file in prompt:\n%s", prompt)
	}
}

func slugForPath(path string) string {
	if path == "" {
		return ""
	}
	path, _ = filepath.Abs(path)
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])[:12]
}
