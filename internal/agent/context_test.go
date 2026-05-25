package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
