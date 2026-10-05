package agent

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/u007/ocode/internal/redact"
)

// shellWrapperNames are interpreters that execute a local script file as their
// first non-flag operand (e.g. "bash script.sh", "sh ./tool.sh").
// Centralized here (not ad-hoc -c scans) and reused from permission context.
var shellWrapperNames = map[string]bool{
	"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true, "ash": true, "csh": true, "tcsh": true,
}

func isShellWrapper(bin string) bool {
	return shellWrapperNames[strings.ToLower(bin)]
}

// customScriptExtensions are filename extensions that indicate a script file
// even when invoked without a directory separator (e.g. "deploy.sh").
var customScriptExtensions = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".ksh": true, ".csh": true,
	".py": true, ".py3": true, ".rb": true, ".pl": true, ".php": true, ".lua": true,
	".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true, ".jsx": true, ".mts": true, ".cts": true,
	".r": true, ".jl": true, ".ps1": true,
}

func isCustomScriptExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return customScriptExtensions[ext]
}

// isValidTextContent reports whether s looks like readable text (not binary).
func isValidTextContent(s string) bool {
	if strings.IndexByte(s, 0) >= 0 {
		return false
	}
	if !utf8.ValidString(s) {
		return false
	}
	return true
}

func (a *Agent) resolveCustomScript(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	candidate = strings.Trim(candidate, "'\"")
	if candidate == "" || candidate == "-" {
		return ""
	}
	if strings.Contains(candidate, "$") || strings.Contains(candidate, "`") || strings.Contains(candidate, "*") || strings.Contains(candidate, "?") || strings.Contains(candidate, "(") {
		return ""
	}
	if strings.HasPrefix(candidate, "-") || strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
		return ""
	}
	var abs string
	if filepath.IsAbs(candidate) {
		abs = filepath.Clean(candidate)
	} else {
		abs = filepath.Clean(filepath.Join(a.effectiveWorkDir(), candidate))
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return ""
	}
	if a.permissions != nil {
		if !a.permissions.IsPathWithinAllowedRoots(abs) {
			return ""
		}
	} else {
		wd := a.effectiveWorkDir()
		underWd := abs == wd || strings.HasPrefix(abs, filepath.Clean(wd)+string(filepath.Separator))
		if !underWd {
			return ""
		}
	}
	return abs
}

// executedScriptContext is one executed script whose source travels to the
// TypeSafe judge as structured, untrusted state.
type executedScriptContext struct {
	Path       string
	Text       string
	SHA256     string
	TotalLines int
	Truncated  bool
}

// defaultExecutedScriptLines is the line ceiling for an executed script shown to
// the judge. It is deliberately NOT the 40-line chat-snippet cap: a real script
// is routinely longer than that, and a partial view can never be approved, so
// the small cap made every such script an unconditional deny. The 16 KiB byte
// ceiling (maxInterpreterSourceBytes) is the binding bound.
const defaultExecutedScriptLines = 1000

// executedScriptLineCap is the one line cap shared by the judge's view and
// verifyAutoGrant's truncation guard, so the two can never diverge. An explicit
// permissions.auto.max_context_lines_per_source still wins.
func (a *Agent) executedScriptLineCap() int {
	if auto := a.autoPermissionConfig(); auto != nil && auto.MaxContextLinesPerSource > 0 {
		return auto.MaxContextLinesPerSource
	}
	return defaultExecutedScriptLines
}

// executedScriptsForJudge returns structured source for the scripts a command
// actually EXECUTES, reusing detectExecutedCustomScripts so this path and the
// chat judge's prose context can never disagree about WHICH files run.
//
// Why it exists: the TypeSafe judge has no read_file tool, so a command like
//
//	chmod +x /tmp/x/s.sh && /tmp/x/s.sh "q" 2>&1 | head -5
//
// reached it as a bare opaque path. It could not determine the effect, named
// truncated_or_unknown, and returned a low-confidence allow that fell under the
// confidence floor — an ordinary command forwarded to a human for no safety
// gain. The chat judge already inlined these scripts (askPermissionModel); only
// this structured path omitted them.
//
// Bounds mirror verifyAutoGrant's truncation guard EXACTLY (same line cap, same
// byte ceiling), so what the judge is shown and what the deterministic guard
// enforces can never diverge. Note that guard REFUSES a truncated script, so a
// truncated entry here can never produce an auto-grant — it forces the human Ask
// this function exists to avoid only for scripts that fit.
//
// Additive: nothing is allowed or denied here. A read failure omits the entry,
// which leaves the pre-existing truncated_or_unknown deferral in place —
// fail-closed.
//
// Known fail-closed limitation: a relative script path that only resolves after a
// top-level `cd` is resolved against the pre-fold working directory by
// resolveCustomScript, so it is usually omitted rather than mis-attributed.
func (a *Agent) executedScriptsForJudge(command string, maxLines, maxSources int) []executedScriptContext {
	if command == "" || maxSources <= 0 {
		return nil
	}
	var out []executedScriptContext
	for _, script := range a.detectExecutedCustomScripts(command) {
		if len(out) >= maxSources {
			break
		}
		// The judge is an LLM: never ship a credential-bearing file into its prompt.
		// Same predicate the chat judge's context applies.
		if redact.IsSensitiveFile(script) || isSecretMaterialPath(script) {
			continue
		}
		content, totalLines, err := readFileSnippet(script, maxLines)
		if err != nil || !isValidTextContent(content) {
			continue
		}
		truncated := totalLines > maxLines
		if len(content) > maxInterpreterSourceBytes {
			content = content[:maxInterpreterSourceBytes]
			truncated = true
		}
		clean, valid := sanitizeSource(content)
		if !valid || strings.TrimSpace(clean) == "" {
			continue
		}
		out = append(out, executedScriptContext{
			Path:       script,
			Text:       clean,
			SHA256:     hashBytes([]byte(clean)),
			TotalLines: totalLines,
			Truncated:  truncated,
		})
	}
	return out
}

// scriptRunnerPrefixes are runner wrappers whose first non-flag operand is
// another executable — an interpreter ("uv run python x.py", "npx tsx x.ts")
// or a script file directly ("uv run x.py"). One prefix is stripped per pass
// and the remainder is re-classified.
var scriptRunnerPrefixes = map[string]bool{
	"npx": true, "bunx": true,
	"uv run": true, "poetry run": true, "pipenv run": true,
	"npm exec": true, "pnpm exec": true, "pnpm dlx": true, "bun x": true,
}

// extraScriptInterpreters extends interpreterLanguages (which drives the
// structured interpreter path) for script-context detection only.
var extraScriptInterpreters = map[string]bool{
	"php": true, "lua": true, "rscript": true, "julia": true, "ts-node": true, "pwsh": true,
}

func isScriptInterpreter(bin string) bool {
	if _, ok := interpreterLanguages[bin]; ok {
		return true
	}
	return extraScriptInterpreters[strings.ToLower(bin)]
}

// stripScriptRunner removes one leading runner prefix (see scriptRunnerPrefixes)
// and returns the wrapped command words. ok=false when no prefix matched.
func stripScriptRunner(words []string) ([]string, bool) {
	if len(words) == 0 {
		return words, false
	}
	bin := filepath.Base(words[0])
	if len(words) >= 2 && scriptRunnerPrefixes[bin+" "+words[1]] {
		return words[2:], true
	}
	if scriptRunnerPrefixes[bin] {
		return words[1:], true
	}
	return words, false
}

// interpreterScriptEntrypoint returns the script-file operand of an interpreter
// invocation ("python x.py" → "x.py"), or "" when nothing on disk is executed:
// inline eval (-c/-e), -m module, bun/deno built-in subcommands, bare REPL.
// Mirrors classifyInterpreterExecution's rules so both paths agree.
func interpreterScriptEntrypoint(bin string, args []string) string {
	if _, found := inlineEvalCode(args); found {
		return ""
	}
	rest := args
	switch {
	case (bin == "bun" || bin == "deno") && len(rest) > 0 && rest[0] == "run":
		rest = rest[1:]
		if bin == "bun" {
			// `bun run <name>` with a bare name runs a package.json script.
			if entry := firstNonFlagArg(rest); entry == "" || !isPathLikeScript(entry) {
				return ""
			}
		}
	case bin == "bun" && len(rest) > 0 && bunBuiltinSubcommands[rest[0]]:
		return ""
	case bin == "deno" && len(rest) > 0 && denoBuiltinSubcommands[rest[0]]:
		return ""
	}
	if hasModuleFlag(rest) {
		return ""
	}
	entry := firstNonFlagArg(rest)
	if entry == "-" {
		return ""
	}
	return entry
}

// detectExecutedCustomScripts returns absolute paths of local script files that
// are actually EXECUTED by the bash command, not merely mentioned as data.
// Covers direct execution (./x.sh), shell wrappers (bash x.sh, source x.sh),
// interpreters (python x.py, node x.js, bun run x.ts) and runner-wrapped forms
// (uv run x.py, npx tsx x.ts). It reuses parseShellCommandLine (proper shell
// tokenisation) and classifyInterpreterExecution's tables to avoid ad-hoc scans.
func (a *Agent) detectExecutedCustomScripts(command string) []string {
	header, _ := extractHeredocs(command)
	cmds, err := parseShellCommandLine(header)
	if err != nil || len(cmds) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	add := func(candidate string) {
		if candidate == "" || candidate == "-" || strings.HasPrefix(candidate, "-") {
			return
		}
		if resolved := a.resolveCustomScript(candidate); resolved != "" && !seen[resolved] {
			seen[resolved] = true
			out = append(out, resolved)
		}
	}
	for _, pc := range cmds {
		words := pc.cmdWords
		for {
			stripped, ok := stripScriptRunner(words)
			if !ok {
				break
			}
			words = stripped
		}
		if len(words) == 0 {
			continue
		}
		binBase := filepath.Base(words[0])
		lowerBin := strings.ToLower(binBase)
		if lowerBin == "source" || binBase == "." {
			if len(words) >= 2 {
				add(words[1])
			}
			continue
		}
		if isShellWrapper(binBase) {
			hasC := false
			for _, w := range words[1:] {
				if w == "-c" || w == "--command" {
					hasC = true
					break
				}
			}
			if hasC {
				continue
			}
			add(firstNonFlagArg(words[1:]))
			continue
		}
		if isScriptInterpreter(binBase) {
			add(interpreterScriptEntrypoint(binBase, words[1:]))
			continue
		}
		first := words[0]
		if strings.Contains(first, "$") || strings.Contains(first, "*") {
			continue
		}
		hasSlash := strings.Contains(first, "/")
		hasScriptExt := isCustomScriptExtension(first)
		if !hasSlash && !hasScriptExt {
			continue
		}
		add(first)
	}
	return out
}
