package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/shell/sandbox"
	"github.com/u007/ocode/internal/tool"
)

// passthroughInspectWrapper runs the command unconfined. It isolates the argv
// and environment hardening from the sandbox in the hostile-repository test, so
// a marker that stays absent proves the hardening itself stopped the vector.
type passthroughInspectWrapper struct{}

func (passthroughInspectWrapper) Wrap(cmd *exec.Cmd, _ sandbox.RootSet) (*exec.Cmd, error) {
	return cmd, nil
}
func (passthroughInspectWrapper) Available() bool { return true }

// fakeInspectWrapper stands in for a sandbox that is unavailable or refuses to
// wrap. It counts Wrap calls so a test can prove an unconfined run never happens.
type fakeInspectWrapper struct {
	available bool
	wrapErr   error
	wraps     int
}

func (f *fakeInspectWrapper) Wrap(cmd *exec.Cmd, _ sandbox.RootSet) (*exec.Cmd, error) {
	f.wraps++
	if f.wrapErr != nil {
		return nil, f.wrapErr
	}
	return cmd, nil
}
func (f *fakeInspectWrapper) Available() bool { return f.available }

// withInspectShellSandbox swaps the confinement backend for the test's duration.
func withInspectShellSandbox(t *testing.T, w sandbox.Wrapper) {
	t.Helper()
	old := inspectShellSandbox
	inspectShellSandbox = func() sandbox.Wrapper { return w }
	t.Cleanup(func() { inspectShellSandbox = old })
}

// newInspectShellTestTool builds the production tool for workDir. It skips only
// when this host genuinely cannot confine inspections: no sandbox, or a backend
// that refuses argv-form commands.
func newInspectShellTestTool(t *testing.T, workDir string) *inspectShellTool {
	t.Helper()
	if !inspectShellSandbox().Available() {
		t.Skip("mandatory sandbox is unavailable on this host")
	}
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sup.Shutdown(ctx); err != nil {
			t.Logf("supervisor shutdown: %v", err)
		}
	})
	tl, ok := newInspectShellTool(toolDigestSnapshot{WorkDir: workDir}, sup)
	if !ok {
		t.Skip("inspect_shell is unavailable: the sandbox cannot confine argv-form commands on this host")
	}
	return tl.(*inspectShellTool)
}

// runInspect executes one argv through the tool and returns its output.
func runInspect(tl tool.Tool, argv ...string) (string, error) {
	raw, _ := json.Marshal(map[string]any{"argv": argv})
	return tl.Execute(raw)
}

// writeInspectFile writes rel under root, creating parent directories.
func writeInspectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeInspectScript writes an executable POSIX shell script.
func writeInspectScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// runInspectTestGit runs git for fixture setup, with the global config disabled
// so the developer's own configuration cannot change the fixture.
func runInspectTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-c", "user.name=test", "-c", "user.email=test@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// buildHostileInspectRepo returns a repository whose config and attributes
// would run a marker script through every vector the explainer must block. It
// returns the repository and the marker path each vector writes when it runs.
func buildHostileInspectRepo(t *testing.T) (string, map[string]string) {
	t.Helper()
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	scripts := filepath.Join(base, "scripts")
	hooks := filepath.Join(base, "hooks")
	markersDir := filepath.Join(base, "markers")
	for _, d := range []string{repo, scripts, hooks, markersDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runInspectTestGit(t, repo, "init", "-q")
	// tracked.txt rides the evil driver, which names both process and textconv.
	// other.txt rides a clean-only driver, since git prefers process over clean
	// for a driver that defines both, so clean needs its own driver to be live.
	writeInspectFile(t, repo, ".gitattributes", "tracked.txt diff=evil filter=evil\nother.txt filter=cleanonly\n")
	writeInspectFile(t, repo, "tracked.txt", "one\n")
	writeInspectFile(t, repo, "other.txt", "one\n")
	runInspectTestGit(t, repo, "add", ".")
	runInspectTestGit(t, repo, "commit", "-q", "-m", "base")
	// Modify both files so every form has content to compare.
	writeInspectFile(t, repo, "tracked.txt", "two\n")
	writeInspectFile(t, repo, "other.txt", "two\n")

	markers := map[string]string{}
	script := func(vector, dir string) string {
		marker := filepath.Join(markersDir, vector+".marker")
		markers[vector] = marker
		p := filepath.Join(dir, vector+".sh")
		writeInspectScript(t, p, ": > '"+marker+"'")
		return p
	}
	runInspectTestGit(t, repo, "config", "core.fsmonitor", script("fsmonitor", scripts))
	runInspectTestGit(t, repo, "config", "core.pager", script("pager", scripts))
	runInspectTestGit(t, repo, "config", "diff.external", script("external", scripts))
	runInspectTestGit(t, repo, "config", "diff.evil.textconv", script("textconv", scripts))
	runInspectTestGit(t, repo, "config", "filter.cleanonly.clean", script("clean", scripts))
	runInspectTestGit(t, repo, "config", "filter.evil.smudge", script("smudge", scripts))
	runInspectTestGit(t, repo, "config", "filter.evil.process", script("process", scripts))
	runInspectTestGit(t, repo, "config", "filter.evil.required", "false")
	// The hooks path is a directory whose post-index-change hook writes a marker.
	marker := filepath.Join(markersDir, "hookspath.marker")
	markers["hookspath"] = marker
	writeInspectScript(t, filepath.Join(hooks, "post-index-change"), ": > '"+marker+"'")
	runInspectTestGit(t, repo, "config", "core.hooksPath", hooks)
	return repo, markers
}

// TestInspectShell_RejectsShellOperatorsAndUnknownFlags covers every refusal
// class. Each error must name an operand position, never its text.
func TestInspectShell_RejectsShellOperatorsAndUnknownFlags(t *testing.T) {
	root := t.TempDir()
	cases := map[string][]string{
		"semicolon":          {"ls", "a;b"},
		"pipe":               {"ls", "a|b"},
		"substitution":       {"ls", "$(x)"},
		"redirect":           {"ls", ">f"},
		"newline":            {"ls", "a\nb"},
		"empty operand":      {"ls", ""},
		"unknown ls flag":    {"ls", "-R"},
		"flag after operand": {"ls", ".", "-a"},
		"find exec":          {"find", ".", "-exec", "rm", "-print"},
		"find delete":        {"find", ".", "-delete"},
		"find no print":      {"find", ".", "-name", "x"},
		"head follow":        {"head", "-f", "x"},
		"go test":            {"go", "test"},
		"go env write":       {"go", "env", "-w", "GOFLAGS=x"},
		"git -c injection":   {"git", "-c", "core.pager=x", "status"},
		"git git-dir":        {"git", "--git-dir=/x", "status"},
		"git -C":             {"git", "-C", "/tmp", "status"},
		"git log":            {"git", "log"},
		"git show rev:path":  {"git", "show", "HEAD:file"},
		"git diff patch":     {"git", "diff", "-p", "--name-only"},
		"git diff no format": {"git", "diff"},
		"git rev:path":       {"git", "rev-parse", "HEAD:x"},
		"git pathspec magic": {"git", "status", "--", ":(exclude)x"},
		"not allowlisted":    {"sh", "-c", "id"},
		"absolute program":   {"/bin/ls"},
		"outside project":    {"ls", "../outside"},
	}
	for name, argv := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := validateInspectShellArgv(root, argv); err == nil {
				t.Fatalf("validateInspectShellArgv(%q) accepted a refused form", argv)
			}
		})
	}

	// A secret used as an operand must not appear in the refusal.
	const secret = "sk-SECRET-OPERAND-1234"
	for _, argv := range [][]string{{"ls", secret + ";x"}, {"ls", "--token=" + secret}, {"git", secret}} {
		_, err := validateInspectShellArgv(root, argv)
		if err == nil {
			t.Fatalf("validateInspectShellArgv(%q) accepted a refused form", argv)
		}
		if strings.Contains(err.Error(), "sk-SECRET") {
			t.Fatalf("refusal echoed a secret operand: %v", err)
		}
	}

	if _, err := validateInspectShellArgv(root, []string{"ls", "-a"}); err != nil {
		t.Fatalf("allowed form ls -a refused: %v", err)
	}
}

// TestInspectShell_FixedBinaryAndPath proves the executed program is the fixed
// absolute candidate, that a project-local impostor never runs, and that PATH
// and credentials are set as the spec requires.
func TestInspectShell_FixedBinaryAndPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executables and shell fixtures")
	}
	root := t.TempDir()
	dirs := inspectShellBinaryDirs()
	resolved, err := resolveInspectShellProgram(dirs, "ls")
	if err != nil {
		t.Fatalf("resolve ls: %v", err)
	}
	inFixed := false
	for _, d := range dirs {
		if filepath.Dir(resolved) == d {
			inFixed = true
		}
	}
	if !inFixed {
		t.Fatalf("ls resolved to %q, outside the fixed directories", resolved)
	}

	// A project-local ls must never be the program that runs.
	writeInspectScript(t, filepath.Join(root, "ls"), "echo PROJECT-LS-RAN")
	tl := newInspectShellTestTool(t, root)
	out, err := runInspect(tl, "ls")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if strings.Contains(out, "PROJECT-LS-RAN") {
		t.Fatalf("a project-local ls executed: %q", out)
	}

	t.Setenv("OPENAI_API_KEY", "sk-env-leak")
	t.Setenv("GIT_DIR", "/elsewhere")
	env := inspectShellEnv()
	wantPath := "PATH=" + strings.Join(dirs, string(os.PathListSeparator))
	sawPath := false
	for _, kv := range env {
		if kv == wantPath {
			sawPath = true
		}
		if strings.HasPrefix(kv, "OPENAI_API_KEY=") || strings.HasPrefix(kv, "GIT_DIR=") {
			t.Fatalf("inspection environment inherited %q", strings.SplitN(kv, "=", 2)[0])
		}
	}
	if !sawPath {
		t.Fatalf("PATH is not the fixed directory list; env=%v", env)
	}
}

// TestInspectShell_SymlinkFIFODeviceRejected proves links out of the project,
// FIFOs and devices are refused before anything executes. The FIFO case also
// runs through the tool under a watchdog, so a regression that opened the FIFO
// would be caught as a hang.
func TestInspectShell_SymlinkFIFODeviceRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink and FIFO fixtures are POSIX-only")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, outside, filepath.Join(root, "out"))
	mustSymlink(t, "/dev/null", filepath.Join(root, "dev"))
	if out, err := exec.Command("mkfifo", filepath.Join(root, "pipe")).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	mustSymlink(t, filepath.Join(root, "pipe"), filepath.Join(root, "fifolink"))

	refused := [][]string{
		{"head", "out"},
		{"head", "dev"},
		{"head", "pipe"},
		{"wc", "pipe"},
		{"head", "fifolink"},
		{"head", "/dev/null"},
	}
	for _, argv := range refused {
		if _, err := validateInspectShellArgv(root, argv); err == nil {
			t.Fatalf("validateInspectShellArgv(%q) admitted a link, FIFO or device", argv)
		}
	}

	tl := newInspectShellTestTool(t, root)
	done := make(chan error, 1)
	crashguard.Go(func() {
		_, err := runInspect(tl, "head", "pipe")
		done <- err
	})
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("head on a FIFO returned no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("head on a FIFO hung: the FIFO was not refused before execution")
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// TestInspectShell_MissingProgramIsToolError proves an allowlisted program that
// is not installed is a per-call tool error, and that inspect_shell stays
// registered when the toolbox is built.
func TestInspectShell_MissingProgramIsToolError(t *testing.T) {
	root := t.TempDir()
	tl := newInspectShellTestTool(t, root)
	tl.dirs = []string{t.TempDir()}
	for _, argv := range [][]string{{"go", "version"}, {"ls"}} {
		out, err := runInspect(tl, argv...)
		if err == nil {
			t.Fatalf("%v with no installed program returned %q, want a tool error", argv, out)
		}
		if !strings.Contains(err.Error(), "not installed") {
			t.Fatalf("%v error = %v, want a not-installed tool error", argv, err)
		}
	}

	box := &toolDigestToolbox{}
	if err := registerInspectShellTool(box, toolDigestSnapshot{WorkDir: root}); err != nil {
		t.Fatalf("registerInspectShellTool: %v", err)
	}
	defer box.Close()
	if _, ok := box.tools["inspect_shell"]; !ok {
		t.Fatal("inspect_shell was not registered although the sandbox is available")
	}
}

// TestInspectShell_GitHardenedPrefixAndEnv proves every admitted Git form starts
// with the exact prefix, diff carries the two forced flags, and the environment
// has the four GIT_* values with no routing variables inherited.
func TestInspectShell_GitHardenedPrefixAndEnv(t *testing.T) {
	root := t.TempDir()
	prefix := []string{
		"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.pager=cat",
		"-c", "diff.external=", "-c", "core.hooksPath=/dev/null",
		"-c", "core.attributesFile=/dev/null",
		"--attr-source=4b825dc642cb6eb9a060e54bf8d69288fbee4904",
	}
	forms := map[string][]string{
		"status":      {"git", "status"},
		"diff name":   {"git", "diff", "--name-only"},
		"diff cached": {"git", "diff", "--cached", "--stat"},
		"rev-parse":   {"git", "rev-parse", "--show-toplevel"},
		"ls-files":    {"git", "ls-files", "--cached"},
		"rev-verify":  {"git", "rev-parse", "--verify", "HEAD"},
		"diff ranged": {"git", "diff", "HEAD~1", "HEAD", "--numstat"},
		"diff pathed": {"git", "diff", "--name-status", "--", "sub/file.txt"},
	}
	for name, argv := range forms {
		t.Run(name, func(t *testing.T) {
			v, err := validateInspectShellArgv(root, argv)
			if err != nil {
				t.Fatalf("validate %v: %v", argv, err)
			}
			if len(v.Args) < len(prefix) || strings.Join(v.Args[:len(prefix)], "\x00") != strings.Join(prefix, "\x00") {
				t.Fatalf("argv %v does not start with the fixed Git prefix", v.Args)
			}
			if v.Args[0] == "-c" {
				t.Fatalf("model -c leaked into the argv: %v", v.Args)
			}
			if argv[1] == "diff" {
				want := []string{"diff", "--no-ext-diff", "--no-textconv"}
				got := v.Args[len(prefix) : len(prefix)+3]
				if strings.Join(got, " ") != strings.Join(want, " ") {
					t.Fatalf("diff argv %v lacks the forced flags", v.Args)
				}
			}
		})
	}

	t.Setenv("GIT_DIR", "/hostile/.git")
	t.Setenv("GIT_WORK_TREE", "/hostile")
	t.Setenv("GIT_INDEX_FILE", "/hostile/index")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.pager")
	t.Setenv("GIT_CONFIG_VALUE_0", "/hostile/pager")
	env := inspectShellEnv()
	need := map[string]bool{
		"GIT_CONFIG_GLOBAL=/dev/null": false,
		"GIT_CONFIG_NOSYSTEM=1":       false,
		"GIT_OPTIONAL_LOCKS=0":        false,
		"GIT_TERMINAL_PROMPT=0":       false,
	}
	for _, kv := range env {
		if _, ok := need[kv]; ok {
			need[kv] = true
		}
		if strings.HasPrefix(kv, "GIT_") && !need[kv] {
			t.Fatalf("inspection environment carries %q", strings.SplitN(kv, "=", 2)[0])
		}
	}
	for kv, seen := range need {
		if !seen {
			t.Fatalf("inspection environment lacks %q", kv)
		}
	}
}

// TestInspectShell_HostileRepoConfigAndAttributes runs the admitted Git forms
// against a repository whose config and attributes would run a marker script
// through every vector. It runs twice: under the real sandbox, and unconfined,
// so the argv and environment hardening is shown to stop execution by itself.
// Each marker must stay absent, and each form must succeed, so an absent marker
// cannot come from the form failing early.
func TestInspectShell_HostileRepoConfigAndAttributes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hostile scripts are POSIX shell")
	}
	forms := [][]string{
		{"status"},
		{"diff", "--name-only"},
		{"diff", "--name-status"},
		{"diff", "--numstat"},
		{"diff", "--stat"},
		{"diff", "--stat", "--", "tracked.txt"},
		{"diff", "--cached", "--numstat"},
		{"ls-files", "--cached"},
		{"rev-parse", "--show-toplevel"},
	}
	run := func(t *testing.T, tl tool.Tool, markers map[string]string) {
		t.Helper()
		for _, form := range forms {
			argv := append([]string{"git"}, form...)
			out, err := runInspect(tl, argv...)
			if err != nil {
				t.Fatalf("%v failed, so its marker check proves nothing: %v", argv, err)
			}
			// run reports a nonzero exit as text, not an error, so check it here.
			if strings.Contains(out, "[exit status") {
				t.Fatalf("%v exited nonzero, so its marker check proves nothing:\n%s", argv, out)
			}
		}
		assertInspectMarkersAbsent(t, markers)
	}

	t.Run("sandboxed", func(t *testing.T) {
		repo, markers := buildHostileInspectRepo(t)
		run(t, newInspectShellTestTool(t, repo), markers)
	})
	t.Run("unconfined argv and env only", func(t *testing.T) {
		withInspectShellSandbox(t, passthroughInspectWrapper{})
		repo, markers := buildHostileInspectRepo(t)
		run(t, newInspectShellTestTool(t, repo), markers)
	})
	t.Run("repo-local info attributes refused", func(t *testing.T) {
		withInspectShellSandbox(t, passthroughInspectWrapper{})
		repo, markers := buildHostileInspectRepo(t)
		writeInspectFile(t, repo, ".git/info/attributes", "tracked.txt filter=evil diff=evil\n")
		tl := newInspectShellTestTool(t, repo)
		if _, err := runInspect(tl, "git", "status"); err == nil {
			t.Fatal("git status ran in a repository with .git/info/attributes")
		}
		assertInspectMarkersAbsent(t, markers)
	})
	t.Run("repo config attributesFile", func(t *testing.T) {
		withInspectShellSandbox(t, passthroughInspectWrapper{})
		repo, markers := buildHostileInspectRepo(t)
		attrs := filepath.Join(t.TempDir(), "attributes")
		if err := os.WriteFile(attrs, []byte("tracked.txt filter=evil diff=evil\nother.txt filter=cleanonly\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runInspectTestGit(t, repo, "config", "core.attributesFile", attrs)
		run(t, newInspectShellTestTool(t, repo), markers)
	})
	t.Run("submodule attributes", func(t *testing.T) {
		withInspectShellSandbox(t, passthroughInspectWrapper{})
		// Negative control: an unhardened status recurses into the submodule and
		// runs its process filter, so the vector is live on this Git.
		control, controlMarkers := buildSubmoduleInspectRepo(t)
		cmd := exec.Command("git", "-C", control, "status", "--porcelain")
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			// Expected for the process fixture, which is not a real protocol peer.
			t.Logf("unhardened submodule status exited non-zero: %v: %s", err, out)
		}
		if _, err := os.Stat(controlMarkers["submodule-process"]); err != nil {
			t.Fatalf("negative control: unhardened status did not run the submodule's filter: %v", err)
		}
		repo, markers := buildSubmoduleInspectRepo(t)
		run(t, newInspectShellTestTool(t, repo), markers)
	})
}

// assertInspectMarkersAbsent fails if any hostile vector wrote its marker.
func assertInspectMarkersAbsent(t *testing.T, markers map[string]string) {
	t.Helper()
	for vector, marker := range markers {
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("hostile vector %q executed: marker %s exists", vector, marker)
		}
	}
}

// buildSubmoduleInspectRepo returns a repository whose checked-out submodule
// carries a process filter, its own .gitattributes naming that filter, and a
// modified file. Its marker shows whether the filter ran.
func buildSubmoduleInspectRepo(t *testing.T) (string, map[string]string) {
	t.Helper()
	base := t.TempDir()
	src := filepath.Join(base, "src")
	repo := filepath.Join(base, "repo")
	scripts := filepath.Join(base, "scripts")
	markersDir := filepath.Join(base, "markers")
	for _, d := range []string{src, repo, scripts, markersDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runInspectTestGit(t, src, "init", "-q")
	writeInspectFile(t, src, "s.txt", "s\n")
	runInspectTestGit(t, src, "add", ".")
	runInspectTestGit(t, src, "commit", "-q", "-m", "s")

	runInspectTestGit(t, repo, "init", "-q")
	writeInspectFile(t, repo, "x.txt", "x\n")
	runInspectTestGit(t, repo, "add", ".")
	runInspectTestGit(t, repo, "commit", "-q", "-m", "x")
	runInspectTestGit(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "sub")
	runInspectTestGit(t, repo, "commit", "-q", "-m", "add sub")

	sub := filepath.Join(repo, "sub")
	marker := filepath.Join(markersDir, "submodule-process.marker")
	script := filepath.Join(scripts, "submodule-process.sh")
	writeInspectScript(t, script, ": > '"+marker+"'")
	// The attribute is committed before the filter is configured: adding a file
	// that names a configured process filter would run it during setup.
	writeInspectFile(t, sub, ".gitattributes", "s.txt filter=evil\n")
	runInspectTestGit(t, sub, "add", ".gitattributes")
	runInspectTestGit(t, sub, "commit", "-q", "-m", "attr")
	runInspectTestGit(t, sub, "config", "filter.evil.process", script)
	runInspectTestGit(t, sub, "config", "filter.evil.required", "false")
	// Same length as "s\n": a size change would let git skip hashing, and the
	// filter would never run.
	writeInspectFile(t, sub, "s.txt", "t\n")
	// Record the submodule's new commit so the superproject's status examines
	// the submodule's worktree instead of reporting only the moved HEAD.
	runInspectTestGit(t, repo, "add", "sub")
	runInspectTestGit(t, repo, "commit", "-q", "-m", "bump sub")
	return repo, map[string]string{"submodule-process": marker}
}

// TestInspectShell_FullDiffNegativeControl proves the hostile vector is live: an
// unhardened git diff in the same repository creates the diff.external marker.
// Without this the hardened test's absent markers could be vacuous. The filter
// drivers are removed first: a configured process filter makes an unhardened
// diff exit before it reaches diff.external, so keeping it would hide the
// external vector rather than isolate it.
func TestInspectShell_FullDiffNegativeControl(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hostile scripts are POSIX shell")
	}
	repo, markers := buildHostileInspectRepo(t)
	for _, key := range []string{"filter.evil.smudge", "filter.evil.process", "filter.evil.required"} {
		runInspectTestGit(t, repo, "config", "--unset", key)
	}
	cmd := exec.Command("git", "-C", repo, "diff")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unhardened git diff: %v: %s", err, out)
	}
	if _, err := os.Stat(markers["external"]); err != nil {
		t.Fatalf("negative control: unhardened git diff did not run diff.external (marker %s missing); "+
			"the hostile vector is not live: %v", markers["external"], err)
	}

	// The process, fsmonitor and clean vectors are shown live by an unhardened
	// status on a fresh, unmodified-by-the-control repository. Its exit status is
	// not checked: the process filter's fixture is not a real protocol peer, so
	// git may exit non-zero after the marker is written.
	status, statusMarkers := buildHostileInspectRepo(t)
	sc := exec.Command("git", "-C", status, "status", "--porcelain")
	sc.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := sc.CombinedOutput(); err != nil {
		t.Logf("unhardened status exited non-zero, as expected for the process fixture: %v: %s", err, out)
	}
	for _, vector := range []string{"process", "fsmonitor", "clean"} {
		if _, err := os.Stat(statusMarkers[vector]); err != nil {
			t.Fatalf("negative control: unhardened status did not run %q (marker missing): %v", vector, err)
		}
	}
}

// TestInspectShell_GitMetadataFiltersSensitivePaths proves sensitive names are
// removed from every metadata form, including the rename record whose old name
// is sensitive, and that ordinary names survive, quoted ones included.
func TestInspectShell_GitMetadataFiltersSensitivePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sandbox fixtures")
	}
	root := t.TempDir()
	runInspectTestGit(t, root, "init", "-q")
	writeInspectFile(t, root, "plain.txt", "one\n")
	writeInspectFile(t, root, `we"ird name.txt`, "quoted\n")
	writeInspectFile(t, root, ".env", "TOKEN=1\n")
	writeInspectFile(t, root, "id_rsa", "KEY-ONE\n")
	writeInspectFile(t, root, "sub/.env", "NESTED=1\n")
	runInspectTestGit(t, root, "add", "-f", ".")
	runInspectTestGit(t, root, "commit", "-q", "-m", "one")

	// The second commit changes sensitive and ordinary files and renames the
	// sensitive id_rsa to an ordinary name, so the rename record has a
	// sensitive old path.
	writeInspectFile(t, root, "plain.txt", "two\n")
	writeInspectFile(t, root, ".env", "TOKEN=2\n")
	writeInspectFile(t, root, `we"ird name.txt`, "quoted two\n")
	runInspectTestGit(t, root, "mv", "id_rsa", "keyfile.txt")
	runInspectTestGit(t, root, "add", "-f", ".")
	runInspectTestGit(t, root, "commit", "-q", "-m", "two")

	// Unstaged and untracked entries cover the worktree forms.
	writeInspectFile(t, root, "plain.txt", "three\n")
	writeInspectFile(t, root, "secret.pem", "-----BEGIN PRIVATE KEY-----\n")
	writeInspectFile(t, root, "untracked.txt", "u\n")

	tl := newInspectShellTestTool(t, root)
	cases := []struct {
		name    string
		argv    []string
		want    []string
		notWant []string
	}{
		{"ls-files cached", []string{"git", "ls-files", "--cached"},
			[]string{"plain.txt", `we"ird name.txt`}, []string{".env", "id_rsa"}},
		{"ls-files others", []string{"git", "ls-files", "--others", "--exclude-standard"},
			[]string{"untracked.txt"}, []string{".env", "secret.pem"}},
		{"status", []string{"git", "status"},
			[]string{"plain.txt", "untracked.txt"}, []string{".env", "id_rsa", "secret.pem"}},
		{"diff name-only ranged", []string{"git", "diff", "HEAD~1", "HEAD", "--name-only"},
			[]string{"plain.txt"}, []string{".env", "id_rsa"}},
		{"diff name-status ranged", []string{"git", "diff", "HEAD~1", "HEAD", "--name-status"},
			[]string{"plain.txt"}, []string{".env", "id_rsa"}},
		{"diff numstat ranged", []string{"git", "diff", "HEAD~1", "HEAD", "--numstat"},
			[]string{"plain.txt"}, []string{".env", "id_rsa"}},
		{"diff stat ranged", []string{"git", "diff", "HEAD~1", "HEAD", "--stat"},
			[]string{"plain.txt", "ird name.txt"}, []string{".env", "id_rsa"}},
		{"diff stat worktree", []string{"git", "diff", "--stat"},
			[]string{"plain.txt"}, []string{".env", "id_rsa", "secret.pem"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runInspect(tl, tc.argv...)
			if err != nil {
				t.Fatalf("%v: %v", tc.argv, err)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Fatalf("%v output lacks %q:\n%s", tc.argv, w, out)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(out, nw) {
					t.Fatalf("%v output leaked %q:\n%s", tc.argv, nw, out)
				}
			}
		})
	}
}

// TestInspectShell_TimeoutKillsProcessGroup proves the deadline kills the whole
// process group, not just the leader. The fake ls is a shell that starts a
// uniquely named sleep. After the call, no such sleep may remain.
func TestInspectShell_TimeoutKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process groups")
	}
	root := t.TempDir()
	fakeDir := t.TempDir()
	writeInspectScript(t, filepath.Join(fakeDir, "ls"), "sleep 31.4159")
	tl := newInspectShellTestTool(t, root)
	tl.dirs = []string{fakeDir}

	oldDeadline := inspectShellDeadline
	inspectShellDeadline = 500 * time.Millisecond
	t.Cleanup(func() { inspectShellDeadline = oldDeadline })

	start := time.Now()
	_, err := runInspect(tl, "ls")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("ls past its deadline returned %v, want a timeout error", err)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("timeout took %s; the deadline did not bound the call", elapsed)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := exec.Command("pgrep", "-f", "sleep 31.4159").Run(); err != nil {
			// pgrep exits non-zero when nothing matches.
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return
			}
			t.Fatalf("pgrep: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the grandchild sleep survived the timeout: the process group was not killed")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestInspectShell_SandboxUnavailableFailsClosed proves that with no sandbox,
// or a sandbox that refuses to wrap, the tool is absent or refuses, and the
// program never runs unconfined.
func TestInspectShell_SandboxUnavailableFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executables and shell fixtures")
	}
	root := t.TempDir()
	fakeDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	writeInspectScript(t, filepath.Join(fakeDir, "ls"), ": > '"+marker+"'")

	withInspectShellSandbox(t, &fakeInspectWrapper{available: false})
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	if _, ok := newInspectShellTool(toolDigestSnapshot{WorkDir: root}, sup); ok {
		t.Fatal("inspect_shell was offered although the sandbox is unavailable")
	}

	unavailable := &fakeInspectWrapper{available: false}
	tl := &inspectShellTool{workDir: root, sup: sup, dirs: []string{fakeDir}, wrapper: unavailable}
	if _, err := runInspect(tl, "ls"); err == nil {
		t.Fatal("ls ran with the sandbox unavailable")
	}

	refusing := &fakeInspectWrapper{available: true, wrapErr: errors.New("no backend")}
	tl = &inspectShellTool{workDir: root, sup: sup, dirs: []string{fakeDir}, wrapper: refusing}
	if _, err := runInspect(tl, "ls"); err == nil {
		t.Fatal("ls ran although the sandbox refused to wrap it")
	}
	if refusing.wraps != 1 {
		t.Fatalf("Wrap called %d times, want exactly one refused attempt with no retry", refusing.wraps)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the program ran unconfined")
	}
}

// TestInspectShell_ToolCatalog proves the production runner catalog advertises
// six tools when inspect_shell is available and five when it is not.
func TestInspectShell_ToolCatalog(t *testing.T) {
	root := t.TempDir()
	payload, err := buildToolDigestPayload(ToolCallDigestInput{
		Tool:    "bash",
		Command: "ls",
		Args:    json.RawMessage(`{"command":"ls"}`),
	}, func(s string) string { return s })
	if err != nil {
		t.Fatal(err)
	}
	client := &digestLoopClient{}
	guard := newToolDigestContextGuard(client)
	snap := toolDigestSnapshot{WorkDir: root}
	names := func(box *toolDigestToolbox) map[string]bool {
		out := map[string]bool{}
		for _, def := range box.Definitions() {
			if n, ok := def["name"].(string); ok {
				out[n] = true
			}
		}
		return out
	}

	// Skip only when this host cannot offer the tool at all, decided by the same
	// constructor production uses.
	probe := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := probe.Shutdown(ctx); err != nil {
			t.Logf("probe supervisor shutdown: %v", err)
		}
	})
	if _, ok := newInspectShellTool(snap, probe); !ok {
		t.Skip("inspect_shell is unavailable on this host: the sandbox cannot confine argv-form commands")
	}
	box, err := newToolDigestRunnerToolbox(snap, payload, guard)
	if err != nil {
		t.Fatal(err)
	}
	got := names(box)
	box.Close()
	if len(got) != 6 || !got["inspect_shell"] {
		t.Fatalf("catalog with inspect_shell has %d tools %v, want 6 including inspect_shell", len(got), got)
	}

	withInspectShellSandbox(t, &fakeInspectWrapper{available: false})
	box, err = newToolDigestRunnerToolbox(snap, payload, guard)
	if err != nil {
		t.Fatal(err)
	}
	got = names(box)
	box.Close()
	if len(got) != 5 || got["inspect_shell"] {
		t.Fatalf("catalog without inspect_shell has %d tools %v, want 5 with no inspect_shell", len(got), got)
	}
}

// TestInspectShell_WindowsDisabled proves the tool has no Windows directories and
// is never offered there. On other hosts it checks the fixed list is present.
func TestInspectShell_WindowsDisabled(t *testing.T) {
	dirs := inspectShellBinaryDirs()
	if runtime.GOOS == "windows" {
		if len(dirs) != 0 {
			t.Fatalf("Windows has inspection directories %v, want none", dirs)
		}
		sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
		if _, ok := newInspectShellTool(toolDigestSnapshot{WorkDir: t.TempDir()}, sup); ok {
			t.Fatal("inspect_shell was offered on Windows")
		}
		return
	}
	if len(dirs) == 0 {
		t.Fatalf("%s has no fixed inspection directories", runtime.GOOS)
	}
}
