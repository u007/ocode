package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runRemoteSearchCommand executes the generated remote shell command locally
// through /bin/sh. The script only uses POSIX shell + grep, so this exercises
// the real matching/pruning behavior instead of asserting on flag strings.
func runRemoteSearchCommand(t *testing.T, command string) (string, int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("remote search script is POSIX shell")
	}
	out, err := exec.Command("sh", "-c", command).Output()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run command: %v", err)
	}
	return string(out), exitErr.ExitCode()
}

// writeSearchFile creates a file (and its parent directories) under root.
func writeSearchFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteSearchCommandFindsMatchesAndSkipsHiddenDirs is the regression pin
// for the root-prune bug: the old command searched "." with
// --exclude-dir='.*', which grep matches against the search root too, so it
// pruned the whole tree and returned zero matches for every query.
func TestRemoteSearchCommandFindsMatchesAndSkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	writeSearchFile(t, root, "src/a.go", "first line\nneeds these Controls here\n")
	writeSearchFile(t, root, "src/nested/b.go", "controls nested\n")
	writeSearchFile(t, root, ".hidden/secret.go", "controls must not be found\n")
	writeSearchFile(t, root, "src/skip.ts", "controls wrong extension\n")

	command, base, err := remoteSearchCommand(
		remoteWork{Path: root},
		fileSearchParams{keywords: []string{"controls"}, exts: []string{"go"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if base != filepath.Base(root) {
		t.Fatalf("base = %q, want %q", base, filepath.Base(root))
	}
	out, code := runRemoteSearchCommand(t, command)
	if code != 0 {
		t.Fatalf("command exited %d, output %q", code, out)
	}
	results, capped := parseRemoteSearchOutput(out, base)
	if capped {
		t.Fatal("unexpected capped")
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v (raw output %q)", len(results), results, out)
	}
	if results[0].Path != "src/a.go" || results[0].Line != 2 {
		t.Fatalf("first result = %+v, want src/a.go:2", results[0])
	}
	if results[1].Path != "src/nested/b.go" {
		t.Fatalf("second result = %+v, want src/nested/b.go", results[1])
	}
	for _, r := range results {
		if strings.Contains(r.Path, ".hidden") {
			t.Fatalf("hidden directory matched: %+v", r)
		}
		if strings.Contains(r.Path, "skip.ts") {
			t.Fatalf("excluded extension matched: %+v", r)
		}
	}
}

// TestRemoteSearchCommandReportsMissingRoot pins that a stale project path is
// an error (exit 3), not a silent empty result.
func TestRemoteSearchCommandReportsMissingRoot(t *testing.T) {
	command, _, err := remoteSearchCommand(
		remoteWork{Path: filepath.Join(t.TempDir(), "does-not-exist")},
		fileSearchParams{keywords: []string{"x"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, code := runRemoteSearchCommand(t, command)
	if code != 3 {
		t.Fatalf("command exited %d, want 3", code)
	}
}

// TestRemoteSearchCommandExitsWhenCdFails pins that a failed cd to the parent
// exits 3 right away, so grep never runs in the ssh login directory.
func TestRemoteSearchCommandExitsWhenCdFails(t *testing.T) {
	command, _, err := remoteSearchCommand(
		remoteWork{Path: "~/www/aimsai2"},
		fileSearchParams{keywords: []string{"x"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(command, `cd "$HOME/www" || exit 3; `) {
		t.Fatalf("command does not exit 3 right after the cd: %q", command)
	}
}

// TestRemoteSearchCommandRejectsDegenerateRoot pins the guard for a "." root,
// which cannot be searched by name without re-introducing the prune bug.
func TestRemoteSearchCommandRejectsDegenerateRoot(t *testing.T) {
	if _, _, err := remoteSearchCommand(
		remoteWork{Path: "."},
		fileSearchParams{keywords: []string{"x"}},
	); err == nil {
		t.Fatal("expected an error for a '.' search root")
	}
	if _, _, err := remoteSearchCommand(
		remoteWork{Path: "/"},
		fileSearchParams{keywords: []string{"x"}},
	); err == nil {
		t.Fatal("expected an error for a filesystem-root search root")
	}
}

// TestRemoteSearchCommandKeepsTildeRoot pins that a tilde-rooted remote path is
// still expanded by the generated command (remote.ShellQuotePath), while the
// searched directory is named rather than ".".
func TestRemoteSearchCommandKeepsTildeRoot(t *testing.T) {
	command, base, err := remoteSearchCommand(
		remoteWork{Path: "~/www/aimsai2"},
		fileSearchParams{keywords: []string{"x"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if base != "aimsai2" {
		t.Fatalf("base = %q, want aimsai2", base)
	}
	if !strings.Contains(command, `cd "$HOME/www"`) {
		t.Fatalf("command does not cd to the expanded parent: %q", command)
	}
	if strings.Contains(command, " . 2>/dev/null") {
		t.Fatalf("command still searches '.': %q", command)
	}
	if !strings.Contains(command, " 'aimsai2' 2>/dev/null") {
		t.Fatalf("command does not search the quoted basename: %q", command)
	}
}

// TestRemoteSearchCommandEmitsIncludesBeforeExcludes pins the GNU grep filter
// precedence rule: when an --exclude is seen before any --include, GNU grep
// searches EVERY file and silently ignores the extension filter (verified on
// GNU grep 3.11; BSD grep tolerates either order, so this assertion is the
// cross-platform pin).
func TestRemoteSearchCommandEmitsIncludesBeforeExcludes(t *testing.T) {
	command, _, err := remoteSearchCommand(
		remoteWork{Path: "/home/u/proj"},
		fileSearchParams{
			keywords:       []string{"x"},
			exts:           []string{"go", "ts"},
			ignorePatterns: []string{"*.log"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	firstInclude := strings.Index(command, "--include=")
	firstExclude := strings.Index(command, "--exclude=")
	if firstInclude < 0 || firstExclude < 0 {
		t.Fatalf("command missing include/exclude filters: %q", command)
	}
	if firstInclude > firstExclude {
		t.Fatalf("first --include must precede the first --exclude (GNU grep drops includes otherwise): %q", command)
	}
}
