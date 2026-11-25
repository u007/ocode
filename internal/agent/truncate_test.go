package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Test contract for the tool-results cache (keep when editing these tests):
//
//  1. ACTUAL PATH — tests assert the real on-disk location the code resolves
//     and writes to (<cache dir>/<toolUseID>.txt), not a mocked writer.
//  2. HARMLESS — no test may read from or write into the user's real state
//     dir (~/.local/state/opencode or %LOCALAPPDATA%\opencode). Every test
//     that touches the cache calls isolateToolResultCache first.
//  3. CROSS-PLATFORM — isolation goes through XDG_STATE_HOME, which
//     toolResultCacheDir honors before any OS-specific branch; OS-specific
//     branches are covered by skip-guarded subtests rather than build tags.
//
// isolateToolResultCache points the tool-results cache at a per-test temp dir
// and returns the resolved cache dir.
func isolateToolResultCache(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, err := toolResultCacheDir()
	if err != nil {
		t.Fatalf("toolResultCacheDir: %v", err)
	}
	return dir
}

func TestToolResultCacheDirResolution(t *testing.T) {
	t.Run("xdg override wins everywhere", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("XDG_STATE_HOME", base)
		t.Setenv("LOCALAPPDATA", filepath.Join(base, "should-not-win"))
		got, err := toolResultCacheDir()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(base, "opencode", "tool-results"); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
	t.Run("windows falls back to LOCALAPPDATA", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("windows-only branch")
		}
		base := t.TempDir()
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("LOCALAPPDATA", base)
		got, err := toolResultCacheDir()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(base, "opencode", "tool-results"); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
	t.Run("unix defaults to ~/.local/state", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix-only branch")
		}
		home := t.TempDir()
		t.Setenv("XDG_STATE_HOME", "")
		setHomeTree(t, home)
		got, err := toolResultCacheDir()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".local", "state", "opencode", "tool-results"); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
}

// TestTruncateToolResultWritesActualPath verifies the full result lands at
// <cache dir>/<toolUseID>.txt — the path the truncation notice tells the model
// to read back — with the untruncated content.
func TestTruncateToolResultWritesActualPath(t *testing.T) {
	dir := isolateToolResultCache(t)
	input := strings.Repeat("row\n", maxToolResultLines+5)
	got := TruncateToolResult("call_actual_path", input)

	want := filepath.Join(dir, "call_actual_path.txt")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected full result at %q: %v", want, err)
	}
	if string(data) != input {
		t.Fatalf("cached file content mismatch (%d bytes vs %d)", len(data), len(input))
	}
	// The notice embeds the path with %q, so compare against the quoted form
	// (matters on Windows where backslashes are escaped).
	if !strings.Contains(got, fmt.Sprintf("%q", want)) {
		t.Fatalf("truncation notice must reference %q, got tail: %q", want, got[len(got)-300:])
	}
}

func TestTruncateToolResultByLines(t *testing.T) {
	isolateToolResultCache(t)
	input := strings.Repeat("line\n", maxToolResultLines+20)
	got := TruncateToolResult("tool-lines", input)

	if !strings.Contains(got, "[output truncated: showing 100/120 lines") {
		t.Fatalf("expected line truncation notice, got: %q", got)
	}
	if !strings.Contains(got, `"start_line": 101, "end_line": <n>`) {
		t.Fatalf("expected read pagination hint, got: %q", got)
	}
	if strings.Count(got, "line\n") > maxToolResultLines {
		t.Fatalf("expected at most %d visible lines before footer", maxToolResultLines)
	}
}

func TestTruncateToolResultByChars(t *testing.T) {
	isolateToolResultCache(t)
	input := strings.Repeat("x", maxToolResultChars+500)
	got := TruncateToolResult("tool-chars", input)

	if !strings.Contains(got, fmt.Sprintf("1/1 lines, %d/%d chars", maxToolResultChars, maxToolResultChars+500)) {
		t.Fatalf("expected char truncation notice, got: %q", got)
	}
	if !strings.Contains(got, strings.Repeat("x", 200)) {
		t.Fatal("expected visible prefix content to be preserved")
	}
	if strings.Contains(got, strings.Repeat("x", maxToolResultChars+100)) {
		t.Fatal("expected oversized single-line output to be truncated")
	}
}

func TestCleanupToolResults(t *testing.T) {
	cacheDir := isolateToolResultCache(t)
	if err := CleanupToolResults(time.Hour); err != nil {
		t.Fatalf("CleanupToolResults on missing dir: %v", err)
	}

	// Write a fake tool-result file into the isolated cache dir so we can
	// exercise the age check. We backdate its mtime to 3 days ago.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	stale := filepath.Join(cacheDir, "call_cleanup_test_stale.txt")
	fresh := filepath.Join(cacheDir, "call_cleanup_test_fresh.txt")

	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate stale file to 3 days ago.
	oldTime := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stale, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := CleanupToolResults(48 * time.Hour); err != nil {
		t.Fatalf("CleanupToolResults: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file should have been removed, err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file should still exist: %v", err)
	}
}

// TestCleanupToolResultsKeepsLiveOwnerSpool: a spool whose owner pid is alive
// survives past maxAge, while a dead owner's spool and a plain stale result
// are removed.
func TestCleanupToolResultsKeepsLiveOwnerSpool(t *testing.T) {
	savedAlive, savedOpen := spoolOwnerAlive, spoolIsOpen
	defer func() { spoolOwnerAlive, spoolIsOpen = savedAlive, savedOpen }()

	writeFile := func(t *testing.T, dir, name string, mtime time.Time) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("stale", func(t *testing.T) {
		cacheDir := isolateToolResultCache(t)
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		spoolOwnerAlive = func(pid int) bool { return pid == 111 }
		old := time.Now().Add(-72 * time.Hour)
		live := writeFile(t, cacheDir, "proc-111-1-proc-1.log", old)
		dead := writeFile(t, cacheDir, "proc-222-1-proc-1.log", old)
		plain := writeFile(t, cacheDir, "call_x.txt", old)

		if err := CleanupToolResults(48 * time.Hour); err != nil {
			t.Fatalf("CleanupToolResults: %v", err)
		}
		if _, err := os.Stat(live); err != nil {
			t.Errorf("live owner's spool should remain: %v", err)
		}
		for _, p := range []string{dead, plain} {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("%s should have been removed, err=%v", filepath.Base(p), err)
			}
		}
	})

	t.Run("own process: finished spool removed, open spool kept", func(t *testing.T) {
		cacheDir := isolateToolResultCache(t)
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		spoolOwnerAlive = func(pid int) bool { return true }
		openName := fmt.Sprintf("proc-%d-2-proc-2.log", os.Getpid())
		spoolIsOpen = func(name string) bool { return name == openName }
		old := time.Now().Add(-72 * time.Hour)
		finished := writeFile(t, cacheDir, fmt.Sprintf("proc-%d-1-proc-1.log", os.Getpid()), old)
		open := writeFile(t, cacheDir, openName, old)

		if err := CleanupToolResults(48 * time.Hour); err != nil {
			t.Fatalf("CleanupToolResults: %v", err)
		}
		if _, err := os.Stat(finished); !os.IsNotExist(err) {
			t.Errorf("finished spool of this process should be removed, err=%v", err)
		}
		if _, err := os.Stat(open); err != nil {
			t.Errorf("running process's spool should remain at any age: %v", err)
		}
	})

	t.Run("fresh live owner", func(t *testing.T) {
		cacheDir := isolateToolResultCache(t)
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		spoolOwnerAlive = func(pid int) bool { return pid == 111 }
		live := writeFile(t, cacheDir, "proc-111-1-proc-1.log", time.Now())

		if err := CleanupToolResults(48 * time.Hour); err != nil {
			t.Fatalf("CleanupToolResults: %v", err)
		}
		if _, err := os.Stat(live); err != nil {
			t.Errorf("fresh live owner's spool should remain: %v", err)
		}
	})
}

// TestTruncateToolResultCharCapHintsActualCutLine guards the resume hint: when
// the char cap trips inside line 3 of a 200-line output, the model must be told
// to continue at line 3 (and the byte offset of the cut), not at line 101.
func TestTruncateToolResultCharCapHintsActualCutLine(t *testing.T) {
	isolateToolResultCache(t)
	long := strings.Repeat("y", maxToolResultChars*2)
	input := "a\nb\n" + long + "\n" + strings.Repeat("c\n", 197)
	got := TruncateToolResult("tool-cutline", input)

	if strings.Contains(got, `"start_line": 101`) {
		t.Fatalf("hint must not skip to line 101 when cut happened at line 3, got tail: %q", got[len(got)-400:])
	}
	if !strings.Contains(got, `"start_line": 3`) {
		t.Fatalf("expected resume hint at line 3, got tail: %q", got[len(got)-400:])
	}
	// Cut lands at rune index maxToolResultChars; with ASCII input the byte
	// offset equals it. The hint must expose a byte-window continuation.
	want := fmt.Sprintf(`"offset_bytes": %d`, maxToolResultChars)
	if !strings.Contains(got, want) {
		t.Fatalf("expected byte-offset hint %s, got tail: %q", want, got[len(got)-400:])
	}
	if !strings.Contains(got, "showing 3/200 lines") {
		t.Fatalf("expected 3/200 lines shown, got tail: %q", got[len(got)-400:])
	}
}
