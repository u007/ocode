package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rgAvailable reports whether a ripgrep binary resolves (PATH or fallback).
func rgAvailable() bool { return rgBinResolver() != "" }

func writeRgrepFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"kept.txt":          "needle here\nother line\n",
		"ignored.txt":       "needle in ignored\n",
		"sub/nested.txt":    "needle nested\n",
		".hidden":           "needle hidden\n",
		".git/config":       "needle in git\n",
		"node_modules/x.js": "needle in node_modules\n",
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// .gitignore excludes ignored.txt wherever it applies.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runRgrep(t *testing.T, ctx context.Context, dir string, args map[string]any) (string, error) {
	t.Helper()
	if args["path"] == "" && args["path"] == nil {
		// Default to the fixture dir unless the caller wants the escape-hatch
		// behavior; resolveSearchRoot needs WithWorkDir to anchor there.
		args["path"] = ""
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return (&RgrepTool{}).ExecuteCtx(WithWorkDir(ctx, dir), raw)
}

func TestRgrepTool_NameAndDefinition(t *testing.T) {
	tool := &RgrepTool{}
	if tool.Name() != "rgrep" {
		t.Fatalf("unexpected tool name: %s", tool.Name())
	}
	if !tool.Parallel() {
		t.Fatal("rgrep should be parallel-capable like grep")
	}
	def := tool.Definition()
	if name, _ := def["name"].(string); name != "rgrep" {
		t.Fatalf("definition name mismatch: got %q", name)
	}
	params, _ := def["parameters"].(map[string]any)
	required, _ := params["required"].([]string)
	if len(required) != 1 || required[0] != "pattern" {
		t.Fatalf("expected required [pattern], got %v", required)
	}
}

func TestRgrepTool_RequiresPattern(t *testing.T) {
	tool := &RgrepTool{}
	if _, err := tool.Execute(json.RawMessage(`{"pattern":"  "}`)); err == nil || !strings.Contains(err.Error(), "'pattern' is required") {
		t.Fatalf("expected missing-pattern error, got %v", err)
	}
	if _, err := tool.Execute(json.RawMessage(`{"pattern":"x","output_mode":"bogus"}`)); err == nil || !strings.Contains(err.Error(), "invalid output_mode") {
		t.Fatalf("expected invalid output_mode error, got %v", err)
	}
	if _, err := tool.Execute(json.RawMessage(`{"pattern":"` + strings.Repeat("a", 501) + `"}`)); err == nil || !strings.Contains(err.Error(), "pattern too long") {
		t.Fatalf("expected pattern-too-long error, got %v", err)
	}
}

func TestRgrepTool_MissingBinaryHint(t *testing.T) {
	if rgBinResolver() != "" {
		t.Skip("rg is installed on this host; hint path untestable")
	}
	_, err := (&RgrepTool{}).Execute(json.RawMessage(`{"pattern":"x"}`))
	var ne *NoticedError
	if err == nil || !errorsAs(err, &ne) || !strings.Contains(ne.Notice, "ripgrep (rg) is not installed") {
		t.Fatalf("expected NoticedError install hint, got %v", err)
	}
}

// errorsAs is a tiny helper so the test file does not import errors twice.
func errorsAs(err error, target **NoticedError) bool {
	for err != nil {
		if ne, ok := err.(*NoticedError); ok {
			*target = ne
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestRgrepTool_GitignoreExcluded(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available on PATH or fallback paths")
	}
	dir := writeRgrepFixture(t)
	out, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "needle"})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	for _, want := range []string{"kept.txt", "sub/nested.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"ignored.txt", "in git", "node_modules"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("gitignored/.git/node_modules file leaked into output:\n%s", out)
		}
	}
}

func TestRgrepTool_HiddenSearched(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	out, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "hidden"})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if !strings.Contains(out, ".hidden") {
		t.Errorf("hidden file should be searched (--hidden):\n%s", out)
	}
}

func TestRgrepTool_OutputModes(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)

	files, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "needle", "output_mode": "files_with_matches"})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if strings.Contains(files, ":") {
		t.Errorf("files_with_matches should not include line numbers:\n%s", files)
	}

	counts, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "needle", "output_mode": "count"})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if !strings.Contains(counts, "kept.txt: 1") {
		t.Errorf("count format mismatch, want \"kept.txt: 1\":\n%s", counts)
	}

	content, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "nested"})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if !strings.Contains(content, "sub/nested.txt:1:needle nested") {
		t.Errorf("content format mismatch (want path:line:text):\n%s", content)
	}
}

func TestRgrepTool_IncludeGlob(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	out, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "needle", "include": "*.txt"})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if !strings.Contains(out, "kept.txt") || strings.Contains(out, ".hidden") {
		t.Errorf("include glob should filter hidden files:\n%s", out)
	}
}

func TestRgrepTool_NoMatches(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	out, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "zzznotfoundzzz"})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if strings.TrimSpace(out) != "No matches found" {
		t.Errorf("expected \"No matches found\", got %q", out)
	}
}

func TestRgrepTool_DashPattern(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "flags.txt"), []byte("--dashflag\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "-dashflag"})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if !strings.Contains(out, "flags.txt") {
		t.Errorf("dash-leading pattern must still match (-e flag):\n%s", out)
	}
}

func TestRgrepTool_PathOutsideWorkdirRejected(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	// Temp-dir siblings are deliberately walkable (pathscope.IsTempDir —
	// tests and /tmp are considered safe, shared with grep/glob/list). The
	// real boundary this tool must hold is non-temp, non-allowed absolute
	// paths such as the home directory and system dirs.
	for _, outside := range []string{
		filepath.Join(homeDirForTest(), "somewhere-else"),
		"/usr/share",
		"/etc",
	} {
		if _, err := os.Stat(outside); err != nil {
			continue
		}
		_, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "needle", "path": outside})
		if err == nil || !strings.Contains(err.Error(), "outside the working directory") {
			t.Fatalf("path %q: expected confinement error, got %v", outside, err)
		}
	}
}

// homeDirForTest returns the user's real home directory (a non-temp,
// non-allowed root for confinement assertions).
func homeDirForTest() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "/Users"
}

func TestRgrepTool_WorkdirAnchoring(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	// No path param at all: anchored on the workDir context.
	raw, _ := json.Marshal(map[string]any{"pattern": "needle"})
	out, err := (&RgrepTool{}).ExecuteCtx(WithWorkDir(context.Background(), dir), raw)
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if !strings.Contains(out, "kept.txt") {
		t.Errorf("workdir-anchored search failed:\n%s", out)
	}
}

func TestRgrepTool_SingleFileTarget(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	target := filepath.Join(dir, "kept.txt")
	out, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "needle", "path": target})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if !strings.Contains(out, "kept.txt:1:needle here") {
		t.Errorf("single-file search mismatch:\n%s", out)
	}
}

func TestRgrepTool_Multiline(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "multi.txt"), []byte("start\nmiddle needle end\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "start.*needle", "multiline": true})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if !strings.Contains(out, "multi.txt") {
		t.Errorf("multiline search should match across lines:\n%s", out)
	}
}

func TestRgrepTool_IgnoreExtraGlobs(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not available")
	}
	dir := writeRgrepFixture(t)
	out, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "needle", "ignore": []string{"nested.txt"}})
	if err != nil {
		t.Fatalf("rgrep failed: %v", err)
	}
	if strings.Contains(out, "nested.txt") {
		t.Errorf("extra ignore glob should exclude nested.txt:\n%s", out)
	}
	if !strings.Contains(out, "kept.txt") {
		t.Errorf("other files should still match:\n%s", out)
	}
}

func TestResolveRgBin_FallbackStat(t *testing.T) {
	// When PATH lookup fails but a fallback file exists, resolveRgBin
	// returns it. Simulate with a temp file in a patched fallback list.
	fake := filepath.Join(t.TempDir(), "rg")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := resolveRgBinWith([]string{fake, "/nonexistent/rg"}, func(string) (string, error) {
		return "", exec.ErrNotFound
	})
	if got != fake {
		t.Fatalf("expected fallback %q, got %q", fake, got)
	}
	if got := resolveRgBinWith([]string{"/nonexistent/rg"}, func(string) (string, error) {
		return "", exec.ErrNotFound
	}); got != "" {
		t.Fatalf("expected empty result with no fallbacks, got %q", got)
	}
}

func TestRgFirstLines(t *testing.T) {
	msg := strings.Repeat("a\n", 10)
	out := rgFirstLines(msg)
	if len(strings.Split(out, "\n")) != 5 {
		t.Fatalf("expected 5 lines max, got %q", out)
	}
	if len(rgFirstLines(strings.Repeat("x", 600))) > 500 {
		t.Fatal("stderr error text must be bounded")
	}
}
