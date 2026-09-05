package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeatbeltProfileGrantsWritableRoots locks the SBPL generation contract:
// a file-write* rule per (real) writable root and a global file-read* (reads
// stay open — write-integrity only). Uses real dirs so canonicalization
// succeeds; the emitted subpath is the canonical (symlink-resolved) path.
func TestSeatbeltProfileGrantsWritableRoots(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	roots := RootSet{
		WritableRoots: []string{a, b},
		ReadRoots:     nil,
		NetworkEgress: true,
	}
	profile := seatbeltProfile(roots)
	if profile == "" {
		t.Fatal("seatbeltProfile returned empty (canonicalization failed)")
	}
	for _, r := range []string{a, b} {
		want := `(allow file-write* (subpath "` + canonicalForTest(t, r) + `"))`
		if !strings.Contains(profile, want) {
			t.Fatalf("profile missing writable-root rule for %s:\n%s", r, profile)
		}
	}
	if !strings.Contains(profile, "(allow file-read*") {
		t.Fatalf("profile missing global file-read*:\n%s", profile)
	}
}

// TestSeatbeltProfileGrantsDevNullOnly locks the /dev grant surface at the
// profile-emission level: /dev/null (pure discard target — a write there is a
// no-op) is granted; /dev/tty is DELIBERATELY not (a fresh open would let a
// confined subprocess paint over the running TUI, bypassing captured output).
// See seatbeltProfileSafe for the rationale and the empirical probe list.
func TestSeatbeltProfileGrantsDevNullOnly(t *testing.T) {
	roots := RootSet{WritableRoots: []string{t.TempDir()}, NetworkEgress: true}
	profile := seatbeltProfile(roots)
	if profile == "" {
		t.Fatal("seatbeltProfile returned empty (canonicalization failed)")
	}
	if !strings.Contains(profile, `(allow file-write* (path "/dev/null"))`) {
		t.Fatalf("profile missing the /dev/null discard grant:\n%s", profile)
	}
	if strings.Contains(profile, `"/dev/tty"`) {
		t.Fatalf("profile grants /dev/tty — a confined process could paint over the live TUI:\n%s", profile)
	}
}

// TestSeatbeltAllowsDevNullDiscard exec-tests the /dev/null grant under real
// sandbox-exec: an O_RDWR open (the "exec 3<>" redirection form) must succeed.
func TestSeatbeltAllowsDevNullDiscard(t *testing.T) {
	if !seatbeltAvailable() {
		t.Skip("/usr/bin/sandbox-exec not present")
	}
	work := t.TempDir()
	if _, err := runWrapped([]string{work}, "sh", "-c", "exec 3<>/dev/null"); err != nil {
		t.Fatalf("O_RDWR open of /dev/null failed under sandbox: %v", err)
	}
}

// TestSeatbeltDeniesDevTTYFreshOpen exec-tests the /dev/tty denial under a
// REAL controlling pseudo-terminal: script(1) forks the probe inside a fresh
// pty session, so the child has /dev/tty even in headless test runs — this
// removes the vacuous-pass case where a tty-less CI would fail the open for
// the wrong reason. Control first: unsandboxed under the pty, the fresh open
// succeeds. Then confined (same pty harness, sandbox-exec outside the pty
// fork): the fresh open of the tty device must be denied by the default
// write rule (only /dev/null is granted — see TestSeatbeltProfileGrantsDevNullOnly).
func TestSeatbeltDeniesDevTTYFreshOpen(t *testing.T) {
	if !seatbeltAvailable() {
		t.Skip("/usr/bin/sandbox-exec not present")
	}
	if !fileExists("/usr/bin/script") {
		t.Skip("/usr/bin/script not present")
	}
	work := t.TempDir()
	probe := "exec 3<>/dev/tty"

	// Control: a fresh /dev/tty open works under a real pty unsandboxed.
	if err := exec.Command("/usr/bin/script", "-q", "/dev/null", "/bin/sh", "-c", probe).Run(); err != nil {
		t.Skipf("pty control probe did not reproduce an openable /dev/tty: %v", err)
	}

	// Confined: same pty harness with sandbox-exec OUTSIDE the pty fork, so
	// script's pty is the confined sh's controlling terminal and the fresh
	// open resolves to a real tty device.
	roots := RootSet{WritableRoots: []string{work}, NetworkEgress: true}
	profile := seatbeltProfile(roots)
	if profile == "" {
		t.Fatal("seatbeltProfile returned empty (canonicalization failed)")
	}
	sandboxed := exec.Command("/usr/bin/script", "-q", "/dev/null",
		sandboxExecPath, "-p", profile, "/bin/sh", "-c", probe)
	out, err := sandboxed.CombinedOutput()
	if err == nil {
		t.Fatalf("fresh open of /dev/tty succeeded under sandbox with a real controlling pty — the TUI-corruption surface is open:\n%s", out)
	}
	t.Logf("confined /dev/tty open denied as expected: %v (%s)", err, strings.TrimSpace(string(out)))
}

// TestSeatbeltProfileCanonicalizesSymlinkRoot locks the realpath boundary: a
// symlink-alias root (e.g. /tmp → /private/tmp on macOS) is emitted as its
// canonical path, because a subpath filter on the symlink alias would not match
// the kernel-observed write and would wrongly block writable-root writes.
func TestSeatbeltProfileCanonicalizesSymlinkRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	canonical := canonicalForTest(t, link)
	if canonical == link {
		t.Skip("filesystem did not present a symlink alias for realpathing")
	}
	profile := seatbeltProfile(RootSet{WritableRoots: []string{link}, NetworkEgress: true})
	if profile == "" {
		t.Fatal("profile empty")
	}
	if !strings.Contains(profile, `(allow file-write* (subpath "`+canonical+`"))`) {
		t.Fatalf("profile did not emit canonical path %s:\n%s", canonical, profile)
	}
}

// TestSeatbeltProfileRejectsMaliciousPath locks the escaping boundary: a root
// containing a quote or newline must be rejected, never emitted raw.
func TestSeatbeltProfileRejectsMaliciousPath(t *testing.T) {
	for _, bad := range []string{`/tmp/evil") (allow process-exec*)`, "/tmp/evil\n(allow file-write* /)"} {
		roots := RootSet{WritableRoots: []string{bad}, NetworkEgress: true}
		if _, err := seatbeltProfileSafe(roots); err == nil {
			t.Fatalf("path %q must be rejected, got nil error", bad)
		}
	}
}

// TestSeatbeltProfileSkipsMissingRoot locks Linux parity: a nonexistent
// writable root is skipped, not a hard error — binding a missing source would
// fail startup, and skipping never widens the boundary. A missing cache
// subdir (e.g. ~/.cache/bun) is already covered when its existing parent
// (e.g. ~/.cache) is writable via subpath.
func TestSeatbeltProfileSkipsMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	roots := RootSet{WritableRoots: []string{missing}, NetworkEgress: true}
	profile, err := seatbeltProfileSafe(roots)
	if err != nil {
		t.Fatalf("missing writable root must be skipped, got error: %v", err)
	}
	if strings.Contains(profile, missing) {
		t.Fatalf("profile must not contain missing root %s:\n%s", missing, profile)
	}
}

// TestSeatbeltProfileSkipsMissingKeepsExisting locks the mixed-root
// regression: an existing root still gets a write rule while a missing
// sibling does not fail the profile.
func TestSeatbeltProfileSkipsMissingKeepsExisting(t *testing.T) {
	existing := t.TempDir()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	roots := RootSet{WritableRoots: []string{existing, missing}, NetworkEgress: true}
	profile, err := seatbeltProfileSafe(roots)
	if err != nil {
		t.Fatalf("mixed roots must not error, got: %v", err)
	}
	want := `(allow file-write* (subpath "` + canonicalForTest(t, existing) + `"))`
	if !strings.Contains(profile, want) {
		t.Fatalf("profile missing existing-root rule %s:\n%s", want, profile)
	}
	if strings.Contains(profile, missing) {
		t.Fatalf("profile must not contain missing root %s:\n%s", missing, profile)
	}
}

// TestSeatbeltProfileRejectsFilesystemRoot locks the boundary guard at the
// profile layer: a writable filesystem root is refused.
func TestSeatbeltProfileRejectsFilesystemRoot(t *testing.T) {
	roots := RootSet{WritableRoots: []string{"/"}, NetworkEgress: true}
	if _, err := seatbeltProfileSafe(roots); err == nil {
		t.Fatal("writable root \"/\" must be rejected")
	}
}

// TestSeatbeltWrapInvocation locks the Wrap contract: the original cmd is
// replaced by one invoking the trusted absolute sandbox-exec with the
// generated profile wrapping the original bash -c, and cmd attributes (Dir,
// Env) are preserved.
func TestSeatbeltWrapInvocation(t *testing.T) {
	base := bashCmd("/bin/bash", "-c", "echo hi")
	base.Dir = "/tmp"
	base.Env = []string{"A=B"}
	roots := RootSet{WritableRoots: []string{t.TempDir()}, NetworkEgress: true}
	w := seatbeltWrapper{}
	wrapped, err := w.Wrap(base, roots)
	if err != nil {
		t.Fatalf("wrap error: %v", err)
	}
	if wrapped.Path != "/usr/bin/sandbox-exec" {
		t.Fatalf("wrapped Path = %q, want /usr/bin/sandbox-exec", wrapped.Path)
	}
	if len(wrapped.Args) < 4 || wrapped.Args[0] != "/usr/bin/sandbox-exec" || wrapped.Args[1] != "-p" {
		t.Fatalf("wrapped Args = %v, want sandbox-exec -p <profile> bash -c ...", wrapped.Args)
	}
	if wrapped.Dir != "/tmp" {
		t.Fatalf("wrapped Dir = %q, want /tmp preserved", wrapped.Dir)
	}
	if len(wrapped.Env) != 1 || wrapped.Env[0] != "A=B" {
		t.Fatalf("wrapped Env = %v, want A=B preserved", wrapped.Env)
	}
}

func canonicalForTest(t *testing.T, p string) string {
	t.Helper()
	c, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return c
}
