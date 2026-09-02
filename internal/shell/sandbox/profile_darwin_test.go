package sandbox

import (
	"os"
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

// TestSeatbeltProfileRejectsUnresolvableRoot locks fail-closed: a nonexistent
// writable root is an error, not silently dropped or broadened.
func TestSeatbeltProfileRejectsUnresolvableRoot(t *testing.T) {
	roots := RootSet{WritableRoots: []string{filepath.Join(t.TempDir(), "does-not-exist")}, NetworkEgress: true}
	if _, err := seatbeltProfileSafe(roots); err == nil {
		t.Fatal("unresolvable writable root must be rejected")
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
