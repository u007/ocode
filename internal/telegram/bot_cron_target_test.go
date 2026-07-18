package telegram

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/u007/ocode/internal/scheduler"
)

// TestBotRegisterCronTargetPersists covers the auto-registration path:
// selectSession calls b.registerCronTarget(e.CWD, chatID) on a successful
// selection. We exercise the same helper directly with a custom CWD that
// maps to a real per-project store under our test temp dir.
func TestBotRegisterCronTargetPersists(t *testing.T) {
	// Point the global data dir at a temp dir so DefaultStorePath stays
	// inside the test sandbox. On Linux we use XDG_DATA_HOME; on macOS
	// we override HOME so the fallback (~/.local/share/opencode) lands
	// inside the sandbox. On Windows we use LOCALAPPDATA.
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", dir)
	default:
		t.Setenv("XDG_DATA_HOME", dir)
		t.Setenv("HOME", dir)
	}

	workdir := filepath.Join(dir, "myproj")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	b := &Bot{}
	const chatID int64 = 4242
	if err := b.registerCronTarget(workdir, chatID); err != nil {
		t.Fatalf("registerCronTarget: %v", err)
	}

	// Re-open the registry and confirm the mapping.
	storePath, err := scheduler.DefaultStorePath(workdir)
	if err != nil {
		t.Fatalf("DefaultStorePath: %v", err)
	}
	tg := scheduler.NewTargets(storePath)
	got, err := tg.Get(workdir)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != chatID {
		t.Fatalf("chat id: got %d want %d", got, chatID)
	}

	// CronTargetPath should match the same file the registry writes to.
	gotPath, err := CronTargetPath(workdir)
	if err != nil {
		t.Fatalf("CronTargetPath: %v", err)
	}
	if gotPath != tg.Path() {
		t.Fatalf("path mismatch: %s vs %s", gotPath, tg.Path())
	}

	// Also confirm the resolved path lives under our temp data dir.
	if rel, err := filepath.Rel(dir, gotPath); err != nil || rel == ".." {
		t.Fatalf("CronTargetPath not under data dir: %s (rel %s)", gotPath, rel)
	}
}
