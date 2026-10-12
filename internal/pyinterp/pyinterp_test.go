package pyinterp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFakePython writes a POSIX shell stand-in for an interpreter. It answers
// --version with version, and for a `-I -c import ...` probe it succeeds only
// when importOK is true, otherwise it fails the way a missing module does.
func writeFakePython(t *testing.T, dir, name, version string, importOK bool) string {
	t.Helper()
	path := filepath.Join(dir, name)
	importCase := "echo \"ModuleNotFoundError: No module named 'onnx_asr'\" >&2\nexit 1"
	if importOK {
		importCase = "exit 0"
	}
	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *\"import \"*) " + importCase + " ;;\n" +
		"esac\n" +
		"echo 'Python " + version + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSelectBoundsRange: a first-on-PATH interpreter newer than the range must
// be skipped in favor of an in-range one, and when none is in range the error
// must name the range and the rejected versions.
func TestSelectBoundsRange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	dir := t.TempDir()
	// 3.14 first (mirrors /opt/homebrew/bin/python3), then 3.13 and 3.10.
	tooNew := writeFakePython(t, dir, "python3", "3.14.6", true)
	inRange := writeFakePython(t, dir, "python3.13", "3.13.14", true)
	tooOld := writeFakePython(t, dir, "python3.10", "3.10.20", true)
	r := Range{Min: [2]int{3, 11}, Max: [2]int{3, 13}}

	got, err := Select(t.Context(), r, [][]string{{tooNew}, {inRange}}, nil)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Version != [2]int{3, 13} {
		t.Fatalf("selected %v, want 3.13 (3.14 is outside the range)", got.Version)
	}

	_, err = Select(t.Context(), r, [][]string{{tooNew}, {tooOld}}, nil)
	if err == nil {
		t.Fatal("Select accepted only out-of-range interpreters")
	}
	msg := err.Error()
	if !strings.Contains(msg, "3.11-3.13") {
		t.Errorf("error does not name the supported range: %q", msg)
	}
	for _, want := range []string{"3.14", "3.10"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not report rejected %s: %q", want, msg)
		}
	}
}

// TestSelectRequiresModules: an in-range interpreter that cannot import the
// required modules must be skipped, and the rejection must say why.
func TestSelectRequiresModules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	dir := t.TempDir()
	noModule := writeFakePython(t, dir, "python3.12", "3.12.2", false)
	withModule := writeFakePython(t, dir, "python3.11", "3.11.9", true)
	r := Range{Min: [2]int{3, 10}}
	modules := []string{"onnx_asr"}

	got, err := Select(t.Context(), r, [][]string{{noModule}, {withModule}}, modules)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Version != [2]int{3, 11} {
		t.Fatalf("selected %v, want 3.11 (3.12 lacks onnx_asr)", got.Version)
	}

	_, err = Select(t.Context(), r, [][]string{{noModule}}, modules)
	if err == nil {
		t.Fatal("Select accepted an interpreter without the required module")
	}
	msg := err.Error()
	for _, want := range []string{"with onnx_asr", "No module named 'onnx_asr'"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
}

// TestCandidatesPreferNewestSupportedMinor guards the search order: the
// unversioned `python3` is usually a too-new Homebrew build, so versioned names
// inside the range must be probed first, including the absolute
// /opt/homebrew/bin/python3.N path a user gets from `brew install python@N`
// (which is NOT reachable as bare `python3`).
func TestCandidatesPreferNewestSupportedMinor(t *testing.T) {
	candidates := Candidates(Range{Min: [2]int{3, 11}, Max: [2]int{3, 13}})
	joined := make([]string, 0, len(candidates))
	for _, c := range candidates {
		joined = append(joined, strings.Join(c, " "))
	}
	text := strings.Join(joined, "\n")
	t.Logf("candidates for 3.11-3.13:\n%s", text)

	// Every candidate must be for a minor inside the range.
	for _, rejected := range []string{"3.14", "3.10"} {
		if strings.Contains(text, rejected) {
			t.Errorf("candidate mentions %s, outside the range: %q", rejected, text)
		}
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(text, "py -3.13") || !strings.Contains(text, "py -3.11") {
			t.Errorf("windows candidates must pin range minors explicitly: %q", text)
		}
		return
	}

	for _, want := range []string{"python3.13", "python3.12", "python3.11"} {
		if !strings.Contains(text, want) {
			t.Errorf("versioned candidate %q missing", want)
		}
	}
	// Newest-first: 3.13 precedes 3.12 precedes 3.11, and every versioned name
	// precedes the bare python3 fallback.
	i13 := strings.Index(text, "python3.13")
	i12 := strings.Index(text, "python3.12")
	i11 := strings.Index(text, "python3.11")
	iBare := strings.Index(text, "\npython3\n")
	if !(i13 < i12 && i12 < i11) {
		t.Errorf("versioned candidates are not newest-first: 3.13@%d 3.12@%d 3.11@%d", i13, i12, i11)
	}
	if iBare >= 0 && iBare < i11 {
		t.Errorf("bare python3 must be a fallback, after every versioned in-range name")
	}
	if !strings.Contains(text, filepath.Join("/opt/homebrew/bin", "python3.13")) {
		t.Errorf("absolute homebrew python3.13 path missing: %q", text)
	}
}

// TestMinorRange covers the probe window: bounded by Max when set, and only
// Min..Min+scanWindow when a range is unbounded.
func TestMinorRange(t *testing.T) {
	if got := minorRange(Range{Min: [2]int{3, 11}, Max: [2]int{3, 13}}); len(got) != 3 || got[0] != 13 || got[2] != 11 {
		t.Errorf("minorRange(bounded) = %v, want [13 12 11]", got)
	}
	got := minorRange(Range{Min: [2]int{3, 10}})
	if len(got) != scanWindow+1 || got[0] != 10+scanWindow || got[len(got)-1] != 10 {
		t.Errorf("minorRange(unbounded) = %v, want newest-first 18..10", got)
	}
}

// TestRangeString keeps the user-facing range text stable, since install and
// probe errors embed it.
func TestRangeString(t *testing.T) {
	if got := (Range{Min: [2]int{3, 11}, Max: [2]int{3, 13}}).String(); got != "3.11-3.13" {
		t.Errorf("String() = %q, want 3.11-3.13", got)
	}
	if got := (Range{Min: [2]int{3, 10}}).String(); got != ">=3.10" {
		t.Errorf("String() = %q, want >=3.10", got)
	}
}

// writeLauncher writes a POSIX stand-in for the Windows py launcher: one binary
// that reports a different version for each -3.N selector it is given.
func writeLauncher(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "launcher")
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  -3.9) echo 'Python 3.9.1' ;;\n" +
		"  -3.11) echo 'Python 3.11.2' ;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSelectProbesEachLauncherArgument: two selectors on one resolved binary
// are two distinct candidates. Deduping on the binary alone skipped the second.
func TestSelectProbesEachLauncherArgument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	launcher := writeLauncher(t, t.TempDir())
	r := Range{Min: [2]int{3, 10}}

	got, err := Select(t.Context(), r, [][]string{{launcher, "-3.9"}, {launcher, "-3.11"}}, nil)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Version != [2]int{3, 11} {
		t.Fatalf("selected %v, want 3.11 (the -3.9 selector is outside the range)", got.Version)
	}
}

// TestSelectDeadlineStopsScan: when the shared deadline expires mid-scan the
// error must wrap the deadline and must not record a probe as failed with an
// empty reason, which is what a killed child used to look like.
func TestSelectDeadlineStopsScan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	dir := t.TempDir()
	slow := filepath.Join(dir, "slow")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	r := Range{Min: [2]int{3, 10}}

	_, err := Select(ctx, r, [][]string{{slow}, {slow, "-x"}}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Select error = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if strings.Contains(err.Error(), "(failed: )") {
		t.Errorf("error reports a probe as failed with no reason: %q", err)
	}
}
