package changes

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// hasShell checks whether /bin/sh is available. Called by each bash-integration
// test so the test suite is portable.
func hasShell() bool {
	_, err := os.Stat("/bin/sh")
	return err == nil
}

// runShell executes cmd via /bin/sh -c in dir and returns the exit code.
func runShell(dir, cmd string) int {
	c := exec.Command("/bin/sh", "-c", cmd)
	c.Dir = dir
	err := c.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return -1
	}
	return 0
}

// ---------------------------------------------------------------------------
// StatBashRecorder integration tests
// ---------------------------------------------------------------------------

func TestStatBashRecorderDetectsCreate(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	reg := NewRegistry()
	rec := NewStatBashRecorder(tmpDir, reg)

	rec.Pre()
	cmd := "echo hi > foo"
	exitCode := runShell(tmpDir, cmd)
	rec.Post(cmd, exitCode)

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	want := filepath.Join(tmpDir, "foo")
	if list[0].OriginalPath != want {
		t.Errorf("path = %q, want %q", list[0].OriginalPath, want)
	}
	if list[0].Status != FileAdded {
		t.Errorf("Status = %v, want FileAdded", list[0].Status)
	}
	if list[0].Undoable {
		t.Error("bash entry should not be undoable")
	}
	if list[0].LastBashCommand != cmd {
		t.Errorf("LastBashCommand = %q, want %q", list[0].LastBashCommand, cmd)
	}
	if list[0].LastBashExitCode != 0 {
		t.Errorf("LastBashExitCode = %d, want 0", list[0].LastBashExitCode)
	}
}

func TestStatBashRecorderNoFalsePositiveOnPathInComment(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	reg := NewRegistry()
	rec := NewStatBashRecorder(tmpDir, reg)

	rec.Pre()
	cmd := `echo "this mentions /etc/passwd but doesn't touch it"`
	exitCode := runShell(tmpDir, cmd)
	rec.Post(cmd, exitCode)

	list := reg.List()
	if len(list) != 0 {
		t.Errorf("expected 0 entries, got %d: %+v", len(list), list)
	}
}

func TestStatBashRecorderSkipsNoiseDirs(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	// Pre-populate a noise directory with a file.
	nmDir := filepath.Join(tmpDir, "node_modules", "pkg")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	touchFile := filepath.Join(nmDir, "index.js")
	if err := os.WriteFile(touchFile, []byte("/* existing */\n"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	rec := NewStatBashRecorder(tmpDir, reg)

	rec.Pre()
	// This runs inside tmpDir; it should NOT detect anything under node_modules
	// because the walk skips that dir entirely.
	cmd := "echo 'hello'"
	exitCode := runShell(tmpDir, cmd)
	rec.Post(cmd, exitCode)

	list := reg.List()
	if len(list) != 0 {
		t.Errorf("expected 0 entries (noise dir skipped), got %d: %+v", len(list), list)
	}
}

func TestStatBashRecorderSkipsNoiseDirsTouch(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	nmDir := filepath.Join(tmpDir, "node_modules")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	rec := NewStatBashRecorder(tmpDir, reg)

	rec.Pre()
	// Touch a file inside the noise dir — should be invisible to both Pre and Post
	// walks, therefore zero touches.
	cmd := "touch node_modules/foo"
	exitCode := runShell(tmpDir, cmd)
	rec.Post(cmd, exitCode)

	list := reg.List()
	if len(list) != 0 {
		t.Errorf("expected 0 entries (noise dir skipped), got %d: %+v", len(list), list)
	}
}

// ---------------------------------------------------------------------------
// NotifyBashWrite unit test
// ---------------------------------------------------------------------------

func TestNotifyBashWrite(t *testing.T) {
	reg := NewRegistry()

	evt := BashWriteEvent{
		Command:  "cp a.txt b.txt",
		WorkDir:  "/tmp/work",
		ExitCode: 0,
		Touches: []BashTouch{
			{Path: "/tmp/work/b.txt", Op: BashAdded},
		},
	}
	reg.NotifyBashWrite(evt)

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if list[0].OriginalPath != "/tmp/work/b.txt" {
		t.Errorf("path = %q, want /tmp/work/b.txt", list[0].OriginalPath)
	}
	if list[0].Status != FileAdded {
		t.Errorf("Status = %v, want FileAdded", list[0].Status)
	}
	if list[0].Undoable {
		t.Error("bash entry should not be undoable")
	}
	if list[0].LastBashCommand != "cp a.txt b.txt" {
		t.Errorf("LastBashCommand = %q", list[0].LastBashCommand)
	}
}

func TestNotifyBashWriteEmptyTouchesNoOp(t *testing.T) {
	reg := NewRegistry()

	evt := BashWriteEvent{
		Command:  "echo hi",
		WorkDir:  "/tmp/work",
		ExitCode: 0,
		Touches:  nil,
	}
	reg.NotifyBashWrite(evt) // should not panic

	if len(reg.List()) != 0 {
		t.Errorf("expected empty list after nil touches, got %d", len(reg.List()))
	}
}
