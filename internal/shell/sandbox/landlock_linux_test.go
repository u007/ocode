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
