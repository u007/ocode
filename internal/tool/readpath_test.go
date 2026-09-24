package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeUnicodeSpaces(t *testing.T) {
	cases := map[string]string{
		"a\u202fb": "a b", // narrow no-break space (macOS screenshot AM/PM)
		"a\u00a0b": "a b", // no-break space
		"a\u2007b": "a b", // figure space
		"a\u2009b": "a b", // thin space
		"a\ufeffb": "a b", // zero-width no-break space
		"plain":    "plain",
		"a b":      "a b",
	}
	for in, want := range cases {
		if got := NormalizeUnicodeSpaces(in); got != want {
			t.Errorf("NormalizeUnicodeSpaces(%q) = %q, want %q", in, got, want)
		}
	}
}

// skipIfCaseInsensitiveFS skips a test whose premise is that two names
// differing only by case are different files. macOS APFS is case-insensitive
// by default, where that premise does not hold.
func skipIfCaseInsensitiveFS(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "caseprobe")); err == nil {
		t.Skip("filesystem is case-insensitive; case-mismatch premise does not hold")
	}
}

func TestResolveReadTargetRecoversUnicodeSpaceSibling(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "Screenshot 2026-09-23 at 11.00.05\u202fPM.png")
	if err := os.WriteFile(real, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	asked := filepath.Join(dir, "Screenshot 2026-09-23 at 11.00.05 PM.png")

	res := ResolveReadTarget(asked, 0)
	if !res.Exists {
		t.Fatalf("expected recovery, got Exists=false hint=%q", res.Hint)
	}
	if res.Path != real {
		t.Fatalf("recovered path = %q, want %q", res.Path, real)
	}
	if res.Hint != "" {
		t.Fatalf("hint should be empty on success, got %q", res.Hint)
	}
}

func TestResolveReadTargetLiteralPathWins(t *testing.T) {
	dir := t.TempDir()
	literal := filepath.Join(dir, "a\u202fb.txt")
	if err := os.WriteFile(literal, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ResolveReadTarget(literal, 0)
	if !res.Exists || res.Path != literal {
		t.Fatalf("literal path should resolve to itself: %+v", res)
	}
}

func TestResolveReadTargetCaseMismatchNotRedirected(t *testing.T) {
	dir := t.TempDir()
	skipIfCaseInsensitiveFS(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "Report.PNG"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ResolveReadTarget(filepath.Join(dir, "report.png"), 0)
	if res.Exists {
		t.Fatalf("case-only mismatch must not auto-resolve, got %+v", res)
	}
	if !strings.Contains(res.Hint, "Report.PNG") {
		t.Fatalf("hint should name the case-variant, got %q", res.Hint)
	}
}

func TestResolveReadTargetAmbiguousVariantsHint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a\u202fb.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a\u00a0b.txt"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ResolveReadTarget(filepath.Join(dir, "a b.txt"), 0)
	if res.Exists {
		t.Fatalf("ambiguous match must not auto-resolve, got %+v", res)
	}
	if !strings.Contains(res.Hint, "non-ASCII space") {
		t.Fatalf("hint should flag the non-ASCII space variants, got %q", res.Hint)
	}
	// The invisible character must be made visible in the message.
	if !strings.Contains(res.Hint, `\u202f`) || !strings.Contains(res.Hint, `\u00a0`) {
		t.Fatalf("hint should escape the non-ASCII spaces, got %q", res.Hint)
	}
}

func TestResolveReadTargetMissingParentDirHint(t *testing.T) {
	dir := t.TempDir()
	res := ResolveReadTarget(filepath.Join(dir, "nope", "a.txt"), 0)
	if res.Exists {
		t.Fatal("expected miss")
	}
	if !strings.Contains(res.Hint, "parent directory does not exist") {
		t.Fatalf("hint = %q", res.Hint)
	}
}

func TestResolveReadTargetMissingWithoutCandidates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ResolveReadTarget(filepath.Join(dir, "zzz-nothing-here.bin"), 0)
	if res.Exists {
		t.Fatal("expected miss")
	}
	if res.Hint != "" {
		t.Fatalf("no similar names should yield an empty hint, got %q", res.Hint)
	}
}
