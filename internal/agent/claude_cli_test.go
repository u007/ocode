package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// withClaudePathSource swaps the login-shell PATH seam (and resets the resolve
// cache) for the duration of a test. The sync.Once cache var is re-initialized
// to its zero value (never copied — copying a sync.Once trips vet's noCopy).
func withClaudePathSource(t *testing.T, source func() string) {
	t.Helper()
	prevSource := claudeLoginShellPath
	claudeLoginShellPath = source
	claudeCLIPathOnce = sync.Once{}
	claudeCLIPathVal = ""
	t.Cleanup(func() {
		claudeLoginShellPath = prevSource
		claudeCLIPathOnce = sync.Once{}
		claudeCLIPathVal = ""
	})
}

func TestClaudeCLIPathPrefersLoginShellPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the executable-bit check is POSIX-only")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A second dir with a non-executable `claude` must be skipped, so the
	// first executable candidate wins.
	nonExec := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonExec, "claude"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	withClaudePathSource(t, func() string {
		return nonExec + string(os.PathListSeparator) + dir
	})

	if got := claudeCLIPath(); got != bin {
		t.Fatalf("claudeCLIPath() = %q, want the executable candidate %q", got, bin)
	}
}

func TestClaudeCLIPathFallsBackToBareName(t *testing.T) {
	withClaudePathSource(t, func() string { return t.TempDir() })

	if got := claudeCLIPath(); got != "claude" {
		t.Fatalf("claudeCLIPath() = %q, want the bare name fallback %q", got, "claude")
	}
}

func TestWithLoginShellPathReplacesPathEntry(t *testing.T) {
	loginPath := "/login/bin" + string(os.PathListSeparator) + "/usr/bin"
	withClaudePathSource(t, func() string { return loginPath })

	got := withLoginShellPath([]string{"HOME=/x", "PATH=/usr/bin:/bin", "TERM=xterm-256color"})
	want := []string{"HOME=/x", "PATH=" + loginPath, "TERM=xterm-256color"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withLoginShellPath() = %#v, want %#v", got, want)
	}

	// The login-shell PATH may itself contain several entries; only the PATH
	// key is rewritten and any duplicate PATH entries are dropped.
	dup := withLoginShellPath([]string{"PATH=/a", "PATH=/b", "HOME=/x"})
	wantDup := []string{"PATH=" + loginPath, "HOME=/x"}
	if !reflect.DeepEqual(dup, wantDup) {
		t.Fatalf("withLoginShellPath() with duplicate PATH = %#v, want %#v", dup, wantDup)
	}

	// No PATH at all → appended rather than silently absent.
	appended := withLoginShellPath([]string{"HOME=/x"})
	wantAppended := []string{"HOME=/x", "PATH=" + loginPath}
	if !reflect.DeepEqual(appended, wantAppended) {
		t.Fatalf("withLoginShellPath() without PATH = %#v, want %#v", appended, wantAppended)
	}
}

func TestWithLoginShellPathEmptyProbeIsNoOp(t *testing.T) {
	withClaudePathSource(t, func() string { return "" })

	env := []string{"HOME=/x", "PATH=/usr/bin:/bin"}
	got := withLoginShellPath(env)
	if !reflect.DeepEqual(got, env) {
		t.Fatalf("withLoginShellPath() with empty probe = %#v, want unchanged %#v", got, env)
	}
	// The returned slice must be the caller's own when nothing changes, so a
	// failed probe cannot mutate shared env state.
	if !strings.Contains(strings.Join(got, "\x00"), "PATH=/usr/bin:/bin") {
		t.Fatalf("withLoginShellPath() dropped the process PATH: %#v", got)
	}
}
