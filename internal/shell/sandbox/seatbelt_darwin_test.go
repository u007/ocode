//go:build darwin

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// seatbeltAvailable is whether /usr/bin/sandbox-exec exists on this machine.
func seatbeltAvailable() bool {
	return fileExists("/usr/bin/sandbox-exec")
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// runWrapped runs a command script through the seatbelt wrapper against the
// given writable roots and returns combined output + the exit error (nil on
// success). args is joined into a single bash -c string so multi-arg commands
// execute correctly.
func runWrapped(writables []string, args ...string) (string, error) {
	script := strings.Join(args, " ")
	roots := RootSet{WritableRoots: writables, NetworkEgress: true}
	w := seatbeltWrapper{}
	wrapped, err := w.Wrap(&exec.Cmd{Path: "/bin/bash", Args: []string{"bash", "-c", script}}, roots)
	if err != nil {
		return "", err
	}
	out, err := wrapped.CombinedOutput()
	return string(out), err
}

// TestSeatbeltConfinesWrites: a write inside a writable root succeeds; a write
// to a sibling outside all roots fails at the OS level.
func TestSeatbeltConfinesWrites(t *testing.T) {
	if !seatbeltAvailable() {
		t.Skip("/usr/bin/sandbox-exec not present")
	}
	work := t.TempDir()
	fail := t.TempDir()

	if _, err := runWrapped([]string{work}, "touch", filepath.Join(work, "ok.txt")); err != nil {
		t.Fatalf("write inside writable root failed: %v", err)
	}
	if _, err := runWrapped([]string{work}, "touch", filepath.Join(fail, "blocked.txt")); err == nil {
		t.Fatal("write outside all roots succeeded — sandbox did not confine")
	}
}

// TestSeatbeltConfinesMutations: unlink aiming at a read-only root fails; the
// same inside a writable root succeeds.
func TestSeatbeltConfinesMutations(t *testing.T) {
	if !seatbeltAvailable() {
		t.Skip("/usr/bin/sandbox-exec not present")
	}
	work := t.TempDir()
	ro := t.TempDir()

	// Create a file in the read-only root first (outside the sandbox) so a
	// confined unlink of it is a targeted read-only-root mutation.
	roFile := filepath.Join(ro, "victim.txt")
	if err := os.WriteFile(roFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	workFile := filepath.Join(work, "mine.txt")
	if err := os.WriteFile(workFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runWrapped([]string{work}, "rm", roFile); err == nil {
		t.Fatal("unlink in a read-only root succeeded — sandbox did not confine the mutation")
	}
	if _, err := runWrapped([]string{work}, "rm", workFile); err != nil {
		t.Fatalf("unlink inside writable root failed: %v", err)
	}

	// Rename: moving a file out of a writable root into a read-only root must
	// fail; a rename wholly inside the writable root must succeed.
	src := filepath.Join(work, "move-src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runWrapped([]string{work}, "mv", src, filepath.Join(ro, "escaped.txt")); err == nil {
		t.Fatal("rename into a read-only root succeeded — sandbox did not confine the mutation")
	}
	if _, err := runWrapped([]string{work}, "mv", src, filepath.Join(work, "move-dst.txt")); err != nil {
		t.Fatalf("rename inside writable root failed: %v", err)
	}

	// Symlink: creating a symlink inside the writable root succeeds; the link
	// resolution itself is governed by the read/write ops on the target (the
	// link cannot be used to mutate a read-only root).
	if _, err := runWrapped([]string{work}, "ln", "-s", roFile, filepath.Join(work, "link.txt")); err != nil {
		t.Fatalf("symlink creation inside writable root failed: %v", err)
	}
}

// TestSeatbeltAllowsExec: exec is global — toolchains run wrapped.
func TestSeatbeltAllowsExec(t *testing.T) {
	if !seatbeltAvailable() {
		t.Skip("/usr/bin/sandbox-exec not present")
	}
	out, err := runWrapped([]string{t.TempDir()}, "python3", "--version")
	if err != nil {
		t.Fatalf("python3 --version failed wrapped: %v (%s)", err, out)
	}
	if !strings.Contains(out, "Python") {
		t.Fatalf("python3 --version output %q lacks Python", out)
	}
}
