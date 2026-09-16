package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// rgBin is the ripgrep binary this tool shells out to.
const rgBin = "rg"

// rgMaxPatternLen mirrors GrepTool's pattern cap.
const rgMaxPatternLen = 500

// rgMaxLineLen caps each emitted content line. A single minified-JS match
// would otherwise eat the whole 30K truncateOutput budget and hide every
// other match in the result.
const rgMaxLineLen = 2000

// rgMaxRetainedOutput bounds how much of rg's stdout is kept for parsing —
// a pathological match flood is dropped at the buffer instead of being
// decoded and re-truncated.
const rgMaxRetainedOutput = 2 << 20 // 2MiB

// rgTimeout bounds a single search. Typical rg runs finish in well under a
// second; this is a runaway guard, not a working budget.
const rgTimeout = 60 * time.Second

// rgFallbackPaths are probed when `rg` is not on PATH. GUI launches on macOS
// inherit a minimal PATH (/usr/bin:/bin:/usr/sbin:/sbin) that misses
// /opt/homebrew/bin, where Homebrew installs ripgrep.
var rgFallbackPaths = []string{
	"/opt/homebrew/bin/rg",
	"/usr/local/bin/rg",
}

// rgBinResolver is an indirection point so tests can simulate a missing
// binary without touching the real PATH.
var rgBinResolver = resolveRgBin

// lookPathFn mirrors exec.LookPath's signature for test injection.
type lookPathFn func(name string) (string, error)

// resolveRgBin returns the ripgrep binary to invoke, or "" when unavailable.
func resolveRgBin() string {
	return resolveRgBinWith(rgFallbackPaths, exec.LookPath)
}

// resolveRgBinWith is the injectable core: PATH lookup first, then exec-bit
// probe of the fallback list (GUI launches inherit a minimal PATH that
// misses the Homebrew install dir). LookPath checks the exec bit even for
// slash-containing paths, so a non-executable file at a fallback location
// is skipped here rather than failing later at Execute time. The os.Stat
// fallback covers injected look functions in tests that only stub PATH
// lookup (exec.ErrNotFound for everything) while the file itself exists.
func resolveRgBinWith(fallbacks []string, look lookPathFn) string {
	if _, err := look(rgBin); err == nil {
		return rgBin
	}
	for _, p := range fallbacks {
		if _, err := look(p); err == nil {
			return p
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// RgrepTool runs content search by shelling out to ripgrep (rg). Unlike the
// pure-Go GrepTool it gets ripgrep's built-in exclusion rules for free:
// .gitignore/.ignore/.rgignore files at every directory level are honored
// even outside a git repo (--no-require-git), hidden files are searched
// (--hidden) while .git and node_modules stay hard-excluded, and the walk
// is parallel with deterministic --sort path ordering. Registered whenever
// the rg binary is available at session start; grep remains the
// zero-dependency fallback.
//
// Read-only by construction (rg never writes), so the tool rides the same
// read-only, path-scoped permission path as grep/glob.
type RgrepTool struct{}

func (t *RgrepTool) Name() string { return "rgrep" }
func (t *RgrepTool) Description() string {
	return "Fast ripgrep-backed text/regex search with built-in exclusion of git-ignored files"
}
func (t *RgrepTool) Parallel() bool { return true }

func (t *RgrepTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "rgrep",
		"description": "Fast plain text/regex search across file contents, backed by ripgrep (rg). " +
			"Files matched by .gitignore/.ignore at any directory depth are excluded automatically " +
			"(even outside git repos), as are .git and node_modules; hidden files are searched. " +
			"Use this for exact strings, logs, config keys, comments, and non-structural matches. " +
			"For symbol-name semantic queries (references/definition/callers), use the 'ast' tool when enabled. " +
			"To search for multiple distinct patterns, call this tool once per pattern as separate parallel calls in the same message.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Regular expression pattern (ripgrep regex syntax)",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional path to search in (default: project root)",
				},
				"include": map[string]interface{}{
					"type":        "string",
					"description": "Optional glob to filter searched files (e.g. *.go, **/*.tsx)",
				},
				"output_mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"files_with_matches", "content", "count"},
					"description": "Output format: files_with_matches (paths only), content (lines with matches), count (matching-line count per file). Default: content.",
				},
				"multiline": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable multiline matching where . matches newlines (default: false)",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *RgrepTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t *RgrepTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var p grepParams

	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("rgrep: invalid params: %w", err)
	}
	if strings.TrimSpace(p.Pattern) == "" {
		return "", fmt.Errorf("rgrep: 'pattern' is required")
	}
	if len(p.Pattern) > rgMaxPatternLen {
		return "", fmt.Errorf("pattern too long (max %d characters)", rgMaxPatternLen)
	}
	switch p.OutputMode {
	case "":
		p.OutputMode = "content"
	case "content", "files_with_matches", "count":
	default:
		return "", fmt.Errorf("rgrep: invalid output_mode %q", p.OutputMode)
	}

	// Same anchoring as grep/glob/list: relative paths join the session's
	// project root, absolute paths are validated against it (plus extra
	// allowed roots, temp dirs, the config dir, and the global gitignore
	// files), so rg cannot walk outside the project.
	searchRoot, err := resolveSearchRoot(ctx, p.Path)
	if err != nil {
		return "", err
	}

	bin := rgBinResolver()
	if bin == "" {
		return "", &NoticedError{
			Err: fmt.Errorf("%s not found on PATH", rgBin),
			Notice: "ripgrep (rg) is not installed. To install:\n" +
				"  brew install ripgrep                    # macOS\n" +
				"  apt install ripgrep                     # Debian/Ubuntu\n" +
				"  dnf install ripgrep                     # Fedora\n" +
				"  pacman -S ripgrep                       # Arch\n" +
				"  winget install BurntSushi.ripgrep.MSVC  # Windows\n" +
				"Then restart ocode, or use the built-in grep tool meanwhile.",
		}
	}

	// cmd.Dir anchors the run at the search root and the search target is
	// always "." (or the base name for a single-file root), so rg's output
	// paths are stable and relative regardless of the process cwd — the
	// desktop shell launches with cwd "/" and must not leak into results.
	runDir, searchArg := rgSearchTarget(searchRoot)

	argv := []string{
		"--no-config",      // ignore RIPGREP_CONFIG_PATH: deterministic flag set
		"--no-require-git", // honor .gitignore even outside a git repo
		"--hidden",         // search dotfiles (.github, config files); .git excluded below
		"--glob", "!.git",
		"--glob", "!node_modules",
		"--color", "never",
		"--sort", "path", // deterministic result order
	}
	if p.Include != "" {
		argv = append(argv, "--glob", p.Include)
	}
	for _, ig := range p.Ignore {
		if strings.TrimSpace(ig) == "" {
			continue
		}
		argv = append(argv, "--glob", "!"+ig)
	}
	switch p.OutputMode {
	case "files_with_matches":
		argv = append(argv, "--files-with-matches")
	case "count":
		argv = append(argv, "--count")
	default:
		argv = append(argv, "--json") // structured records: robust path:line parsing
	}
	if p.Multiline {
		argv = append(argv, "--multiline")
	}
	pattern := p.Pattern
	if p.Multiline {
		pattern = `(?s)` + pattern // mirror GrepTool: dot spans newlines
	}
	// -e keeps dash-leading patterns out of the flag parser; the trailing
	// positional is the search target.
	argv = append(argv, "-e", pattern, "--", searchArg)

	runCtx, cancel := context.WithTimeout(context.Background(), rgTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, argv...)
	cmd.Dir = runDir
	stdout := &cappedBuffer{limit: rgMaxRetainedOutput}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("rgrep: timed out after %s", rgTimeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// rg exits 1 when there are simply no matches (per-file read
			// problems are stderr noise; per rg semantics they are not fatal).
			return "No matches found", nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("rgrep failed: %s", rgFirstLines(msg))
	}

	out := t.format(p, runDir, stdout)
	if stdout.truncated {
		out += "\n\n[output capped — narrow with \"path\"/\"include\" or a more specific pattern]"
	}
	if strings.TrimSpace(out) == "" {
		return "No matches found", nil
	}
	return truncateOutput(strings.TrimRight(out, "\n")), nil
}

// rgSearchTarget maps a resolved search root to the (cmd.Dir, rg target)
// pair: a directory root searches "." with cmd.Dir at the root; a
// single-file root (e.g. an exact global gitignore file) searches its base
// name with cmd.Dir at its parent. Keeping the target relative to cmd.Dir
// makes rg's printed paths stable and display-normalizable.
func rgSearchTarget(searchRoot string) (string, string) {
	if st, err := os.Stat(searchRoot); err == nil && !st.IsDir() {
		return filepath.Dir(searchRoot), filepath.Base(searchRoot)
	}
	return searchRoot, "."
}

// displayPath converts an rg output path (relative to runDir, e.g.
// "./kept.txt") to the same display convention the other search tools use:
// relative to the project root when no path was given, re-prefixed with the
// original relative path param, or absolute for an absolute param.
func rgDisplayPath(out, pathParam, runDir string) string {
	rel := strings.TrimPrefix(out, "./")
	// Normalize Windows separators before re-joining with OS semantics.
	rel = filepath.FromSlash(strings.ReplaceAll(rel, "\\", "/"))
	if pathParam == "" {
		return rel
	}
	if filepath.IsAbs(pathParam) {
		return filepath.Join(runDir, rel)
	}
	return filepath.Join(pathParam, rel)
}

type rgJSONRecord struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

// format turns rg's output into the shared search-tool display format.
func (t *RgrepTool) format(p grepParams, runDir string, stdout *cappedBuffer) string {
	var b strings.Builder
	switch p.OutputMode {
	case "content":
		dec := json.NewDecoder(bytes.NewReader(stdout.buf.Bytes()))
		for {
			var rec rgJSONRecord
			if err := dec.Decode(&rec); err != nil {
				break // clean EOF or a truncated tail from the cap
			}
			if rec.Type != "match" {
				continue
			}
			line := strings.TrimRight(rec.Data.Lines.Text, "\n")
			if len(line) > rgMaxLineLen {
				line = line[:rgMaxLineLen] + " …[line truncated]"
			}
			fmt.Fprintf(&b, "%s:%d:%s\n", rgDisplayPath(rec.Data.Path.Text, p.Path, runDir), rec.Data.LineNumber, line)
		}
	case "count":
		// rg prints "path:N" per file; reformat to the shared "path: N".
		for _, line := range strings.Split(string(stdout.buf.Bytes()), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			idx := strings.LastIndex(line, ":")
			if idx < 0 {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", rgDisplayPath(line[:idx], p.Path, runDir), line[idx+1:])
		}
	case "files_with_matches":
		for _, line := range strings.Split(string(stdout.buf.Bytes()), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			b.WriteString(rgDisplayPath(line, p.Path, runDir) + "\n")
		}
	}
	return b.String()
}

// rgFirstLines bounds an rg stderr blob surfaced in an error.
func rgFirstLines(msg string) string {
	lines := strings.Split(msg, "\n")
	if len(lines) > 5 {
		lines = lines[:5]
	}
	out := strings.Join(lines, "\n")
	if len(out) > 500 {
		out = out[:500]
	}
	return out
}

// cappedBuffer keeps at most limit bytes; the rest is counted and dropped.
// rg finishes normally (it is fast); we simply stop retaining.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.limit {
		c.truncated = true
		if room := c.limit - c.buf.Len(); room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }
