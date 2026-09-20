//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// linuxManyBackends runs a wrapped bash against the linux wrapper and returns
// combined output + exit error. Skips when no backend is available.
func linuxManyBackends(t *testing.T, writables []string, script string) (string, error) {
	t.Helper()
	w := newLinuxWrapper(prodLinuxProbes())
	if !w.Available() {
		t.Skip("no Landlock or bubblewrap backend available on this host")
	}
	cmd, err := w.Wrap(bashCmd("/bin/bash", "-c", script), RootSet{WritableRoots: writables, NetworkEgress: true})
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestLinuxConfinesWrites: a write inside a writable root succeeds; a write to
// a sibling outside all roots fails at the OS level.
func TestLinuxConfinesWrites(t *testing.T) {
	work := t.TempDir()
	fail := t.TempDir()
	if _, err := linuxManyBackends(t, []string{work}, "touch "+filepath.Join(work, "ok.txt")); err != nil {
		t.Fatalf("write inside writable root failed: %v", err)
	}
	if _, err := linuxManyBackends(t, []string{work}, "touch "+filepath.Join(fail, "blocked.txt")); err == nil {
		t.Fatal("write outside all roots succeeded — sandbox did not confine")
	}
}

// TestLinuxConfinesMutations: unlink in a read-only root fails; the same
// inside a writable root succeeds.
func TestLinuxConfinesMutations(t *testing.T) {
	work := t.TempDir()
	ro := t.TempDir()
	roFile := filepath.Join(ro, "victim.txt")
	if err := os.WriteFile(roFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	workFile := filepath.Join(work, "mine.txt")
	if err := os.WriteFile(workFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := linuxManyBackends(t, []string{work}, "rm "+roFile); err == nil {
		t.Fatal("unlink in a read-only root succeeded — sandbox did not confine the mutation")
	}
	if _, err := linuxManyBackends(t, []string{work}, "rm "+workFile); err != nil {
		t.Fatalf("unlink inside writable root failed: %v", err)
	}

	// Rename escape across the boundary must fail.
	src := filepath.Join(work, "move-src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := linuxManyBackends(t, []string{work}, "mv "+src+" "+filepath.Join(ro, "escaped.txt")); err == nil {
		t.Fatal("rename into a read-only root succeeded")
	}
}

// TestLinuxAllowsExec: exec is global — toolchains run wrapped.
func TestLinuxAllowsExec(t *testing.T) {
	out, err := linuxManyBackends(t, []string{t.TempDir()}, "echo confined-ok")
	if err != nil {
		t.Fatalf("echo failed wrapped: %v", err)
	}
	if !strings.Contains(out, "confined-ok") {
		t.Fatalf("output %q lacks confined-ok", out)
	}
}

// TestLinuxAllowsDevNullWrite: /dev/null sits outside every writable root,
// but tools open it for write as a pure discard target (`cmd 2>/dev/null`) and
// Landlock denies an open no rule grants. Regression for the Linux lockout
// where bash reported "/dev/null: Permission denied" and every command using
// it failed.
func TestLinuxAllowsDevNullWrite(t *testing.T) {
	out, err := linuxManyBackends(t, []string{t.TempDir()}, "echo discard > /dev/null && echo devnull-ok")
	if err != nil {
		t.Fatalf("write to /dev/null failed under sandbox: %v", err)
	}
	if !strings.Contains(out, "devnull-ok") {
		t.Fatalf("output %q lacks devnull-ok", out)
	}
}

// TestLinuxAllowsFileWritableRoot is the regression for the Landlock EINVAL
// lockout: a writable root that is a regular FILE (projects.json / the global
// git-ignore files, which NewRootSet's protected-file carve-out expands out of
// the writable data dir) used to be added with the full directory-capable mask.
// landlock_add_rule(2) rejects that with EINVAL, aborting the ruleset, so the
// confiner failed closed and EVERY sandboxed command died with
// `sandbox-confine: landlock rule for ".../projects.json": invalid argument`.
// The file must be accepted as a root and remain writable.
func TestLinuxAllowsFileWritableRoot(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Only the FILE is writable — its parent directory is not, so a successful
	// open+truncate proves the file-level write/truncate grant took effect.
	if _, err := linuxManyBackends(t, []string{target}, "printf new > "+target); err != nil {
		t.Fatalf("write to a file writable root failed: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("file writable root content = %q, want %q", got, "new")
	}
}

// TestLinuxConfineEntrypointStripsProtocolEnv locks the env-scrubbing boundary
// at the confiner level: the OCODE_SANDBOX_* vars must not leak into the
// confined command's environment.
func TestLinuxConfineEntrypointStripsProtocolEnv(t *testing.T) {
	out, err := linuxManyBackends(t, []string{t.TempDir()}, "printenv OCODE_SANDBOX_ROOTS")
	if err == nil {
		t.Fatalf("protocol env leaked into the confined process (printenv found it): %q", out)
	}
}

var _ = exec.Command // keep os/exec import for bashCmd-like helpers
