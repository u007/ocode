// Package pyinterp chooses a Python interpreter on this host: the first
// candidate inside a version range and, when asked, one that can also import a
// set of modules. TTS installs use it with version-only selection; STT uses it
// with its required modules so a found interpreter is one that can actually run
// the engine.
package pyinterp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Range is an inclusive interpreter version range. A zero Max is unbounded.
type Range struct {
	Min [2]int
	Max [2]int
}

// Spec is a selected interpreter. Command is the argv prefix (["python3"] or
// ["py", "-3.12"]).
type Spec struct {
	Command []string
	Version [2]int
}

var versionRE = regexp.MustCompile(`Python (\d+)\.(\d+)`)

// scanWindow bounds how far above Min an unbounded range probes for versioned
// interpreter names (all shipped TTS runtimes set Max; this only keeps an
// unbounded range from degenerating).
const scanWindow = 8

// Accepts reports whether v lies inside the inclusive range.
func (r Range) Accepts(v [2]int) bool {
	if v[0] < r.Min[0] || (v[0] == r.Min[0] && v[1] < r.Min[1]) {
		return false
	}
	if r.Max != [2]int{} &&
		(v[0] > r.Max[0] || (v[0] == r.Max[0] && v[1] > r.Max[1])) {
		return false
	}
	return true
}

// String renders the range for user-facing errors ("3.11-3.13", or ">=3.10"
// when no ceiling is set).
func (r Range) String() string {
	lower := fmt.Sprintf("%d.%d", r.Min[0], r.Min[1])
	if r.Max == [2]int{} {
		return ">=" + lower
	}
	return lower + "-" + fmt.Sprintf("%d.%d", r.Max[0], r.Max[1])
}

// minorRange returns the minor versions to probe, newest first, so the newest
// supported interpreter wins.
func minorRange(r Range) []int {
	hi := r.Min[1] + scanWindow
	if r.Max != [2]int{} {
		hi = r.Max[1]
	}
	var minors []int
	for m := hi; m >= r.Min[1]; m-- {
		minors = append(minors, m)
	}
	return minors
}

// ExtraCandidateDirs are the absolute directories probed in addition to PATH.
// They are consulted because a user who installed Python with a package
// manager often has it outside PATH for a non-login shell.
//
// It is a package var so a test can restrict the search to a controlled set:
// these directories are probed by absolute path, so a test that only rewrites
// PATH cannot stop them from finding the host's real Python. A host with
// onnx-asr installed anywhere would otherwise make ocode's install gate
// unreachable in a hermetic test.
var ExtraCandidateDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"}

// FrameworkCandidateGlobs are the absolute-path patterns probed for a
// framework-installed Python (the python.org installer on macOS). Like
// ExtraCandidateDirs, it is a var so a test can remove the host's real
// interpreter from the search.
var FrameworkCandidateGlobs = []string{
	"/Library/Frameworks/Python.framework/Versions/3.%d/bin/python3",
}

// Candidates lists the argv prefixes to probe for r, in preference order: the
// newest supported minor first, then the unversioned names. Versioned names are
// probed deliberately: a too-new python3 is usually what PATH resolves, and a
// user following an install hint (`brew install python@3.12`) gets a versioned
// binary that is NOT reachable as `python3`. Select version-checks every
// candidate, so a too-old or too-new python3 never silently wins.
func Candidates(r Range) [][]string {
	minors := minorRange(r)
	if runtime.GOOS == "windows" {
		// The py launcher resolves the newest install for a bare `-3`, so pin
		// the range's minors explicitly and newest-first.
		var candidates [][]string
		for _, m := range minors {
			candidates = append(candidates, []string{"py", "-3." + strconv.Itoa(m)})
		}
		return append(candidates, []string{"py", "-3"}, []string{"python"}, []string{"python3"})
	}
	var candidates [][]string
	for _, m := range minors {
		candidates = append(candidates, []string{"python3." + strconv.Itoa(m)})
	}
	candidates = append(candidates, []string{"python3"}, []string{"python"})
	for _, dir := range ExtraCandidateDirs {
		for _, m := range minors {
			candidates = append(candidates, []string{filepath.Join(dir, "python3."+strconv.Itoa(m))})
		}
		candidates = append(candidates, []string{filepath.Join(dir, "python3")})
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, m := range minors {
			candidates = append(candidates, []string{filepath.Join(home, ".pyenv", "shims", "python3."+strconv.Itoa(m))})
		}
		candidates = append(candidates, []string{filepath.Join(home, ".pyenv", "shims", "python3")})
	}
	if runtime.GOOS == "darwin" {
		for _, m := range minors {
			for _, pattern := range FrameworkCandidateGlobs {
				matches, _ := filepath.Glob(fmt.Sprintf(pattern, m))
				for _, match := range matches {
					candidates = append(candidates, []string{match})
				}
			}
		}
	}
	return candidates
}

// Select probes candidates in order and returns the first interpreter whose
// version is inside r and, when modules is non-empty, that can import every
// one of them. Candidates that resolve to the same binary with the same
// launcher arguments are probed once. Failure lists every rejected candidate
// and why, so the caller can show the user what was tried. All probes share
// ctx: once it expires the scan stops and the error wraps ctx.Err().
func Select(ctx context.Context, r Range, candidates [][]string, modules []string) (Spec, error) {
	var tried []string
	probed := map[string]bool{}
	for _, argv := range candidates {
		if ctx.Err() != nil {
			break
		}
		bin, err := exec.LookPath(argv[0])
		if err != nil {
			// intentionally not logged: a missing candidate name is the normal case
			// on most hosts, and every probed candidate is reported in the error
			continue
		}
		resolved, err := filepath.EvalSymlinks(bin)
		if err != nil {
			tried = append(tried, strings.Join(argv, " ")+" (unresolvable: "+err.Error()+")")
			continue
		}
		// `py -3.9` and `py -3.11` are one executable with different selections,
		// so the launcher arguments are part of the identity.
		key := resolved + "\x00" + strings.Join(argv[1:], "\x00")
		if probed[key] {
			continue
		}
		probed[key] = true

		args := append([]string{}, argv[1:]...)
		out, err := exec.CommandContext(ctx, bin, append(args, "--version")...).CombinedOutput()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			tried = append(tried, strings.Join(argv, " ")+" (failed: "+failureDetail(out, err)+")")
			continue
		}
		m := versionRE.FindSubmatch(out)
		if m == nil {
			tried = append(tried, strings.Join(argv, " ")+" (unparseable version)")
			continue
		}
		major, _ := strconv.Atoi(string(m[1]))
		minor, _ := strconv.Atoi(string(m[2]))
		version := [2]int{major, minor}
		if !r.Accepts(version) {
			tried = append(tried, fmt.Sprintf("%s (%d.%d)", strings.Join(argv, " "), major, minor))
			continue
		}
		if len(modules) > 0 {
			probe := "import " + strings.Join(modules, ", ")
			importArgs := append(append([]string{}, args...), "-I", "-c", probe)
			out, err := exec.CommandContext(ctx, bin, importArgs...).CombinedOutput()
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				tried = append(tried, strings.Join(argv, " ")+" ("+failureDetail(out, err)+")")
				continue
			}
		}
		return Spec{Command: append([]string{bin}, args...), Version: version}, nil
	}
	what := "Python " + r.String()
	if len(modules) > 0 {
		what += " with " + strings.Join(modules, ", ")
	}
	triedText := ""
	if len(tried) > 0 {
		triedText = " (tried: " + strings.Join(tried, ", ") + ")"
	}
	if err := ctx.Err(); err != nil {
		return Spec{}, fmt.Errorf("no %s found: scan stopped: %w%s", what, err, triedText)
	}
	return Spec{}, fmt.Errorf("no %s found%s", what, triedText)
}

// VenvPython is the interpreter inside a venv directory.
func VenvPython(venv string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venv, "Scripts", "python.exe")
	}
	return filepath.Join(venv, "bin", "python")
}

// failureDetail explains a probe that exited non-zero: the last line of its
// output when it printed any (a failed import ends in "ModuleNotFoundError:
// ..."), otherwise the exec error, so a silent failure is never reported empty.
func failureDetail(out []byte, err error) string {
	if line := lastLine(out); line != "" {
		return line
	}
	return err.Error()
}

// lastLine returns the final non-empty line of a process's combined output,
// or "" when it printed nothing.
func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
