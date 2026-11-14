package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Claude Code SessionStart hooks.
//
// A Claude Code plugin has no always-on instructions field; it injects its
// standing context from a SessionStart command hook in hooks/hooks.json or in
// the hooks field of its manifest
// (superpowers, for one, injects its using-superpowers skill this way). To
// give such a plugin the same behavior in ocode, SessionStartContext runs
// those hooks for the "startup" source and returns what they print, which
// LoadContext appends next to the ocode plugins' instructions.
//
// Only plugins that ship a Claude Code manifest are considered, and only
// command hooks run. Output is cached per (project root, plugin dir) for the
// life of the process: LoadContext can run every turn, and a hook re-run
// would both cost a subprocess and risk changing the cached prompt prefix.

// sessionStartDefaultTimeout bounds a hook that declares no timeout; a hung
// hook must not stall session start indefinitely.
const sessionStartDefaultTimeout = 30 * time.Second

// sessionStartMaxTimeout caps a hook's declared timeout.
const sessionStartMaxTimeout = 60 * time.Second

var (
	sessionStartMu    sync.Mutex
	sessionStartCache = map[string]string{}
)

type hooksFile struct {
	Hooks map[string][]hookMatcher `json:"hooks"`
}

type hookMatcher struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Shell   string   `json:"shell"`
	Timeout float64  `json:"timeout"`
}

// SessionStartContext returns the combined SessionStart output of the loaded
// Claude Code-format plugins for projectRoot, each block headed by its
// plugin name. Empty when no plugin injects anything.
func SessionStartContext(enabled map[string]bool, projectRoot string) string {
	var b strings.Builder
	for _, p := range LoadPluginsForProject(enabled, projectRoot) {
		if !p.HasClaudeManifest {
			continue
		}
		out := cachedSessionStart(p.Dir, projectRoot)
		if out == "" {
			continue
		}
		b.WriteString("\n--- Plugin: " + p.Name + " (SessionStart) ---\n")
		b.WriteString(out)
		b.WriteString("\n")
	}
	return b.String()
}

// InvalidateSessionStartCache drops cached hook output, e.g. after a plugin
// is installed, updated or removed.
func InvalidateSessionStartCache() {
	sessionStartMu.Lock()
	sessionStartCache = map[string]string{}
	sessionStartMu.Unlock()
	invalidateClaudeHookCache()
}

func cachedSessionStart(pluginDir, projectRoot string) string {
	key := projectRoot + "\x00" + pluginDir
	sessionStartMu.Lock()
	out, ok := sessionStartCache[key]
	sessionStartMu.Unlock()
	if ok {
		return out
	}
	out = runSessionStartHooks(pluginDir, projectRoot)
	sessionStartMu.Lock()
	sessionStartCache[key] = out
	sessionStartMu.Unlock()
	return out
}

// runSessionStartHooks runs every SessionStart command hook the plugin declares
// (in hooks/hooks.json and in the hooks field of its Claude Code manifest)
// whose matcher accepts "startup", and joins their output. A failing hook
// contributes nothing; it never fails the session.
func runSessionStartHooks(pluginDir, projectRoot string) string {
	var parts []string
	for _, src := range pluginHookSources(pluginDir) {
		for _, m := range src.Hooks["SessionStart"] {
			if !sessionStartMatches(m.Matcher) {
				continue
			}
			for _, h := range m.Hooks {
				if h.Type != "" && h.Type != "command" {
					continue
				}
				if out := runHookCommand(h, pluginDir, projectRoot); out != "" {
					parts = append(parts, out)
				}
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// pluginHookSources returns every hook definition the plugin declares, in
// order: hooks/hooks.json first, then the manifest hooks field. Every event
// and matcher is included; callers pick the events they run.
func pluginHookSources(pluginDir string) []hooksFile {
	var sources []hooksFile
	if data, err := os.ReadFile(filepath.Join(pluginDir, "hooks", "hooks.json")); err == nil {
		var hf hooksFile
		if err := json.Unmarshal(data, &hf); err == nil {
			sources = append(sources, hf)
		}
	}
	return append(sources, manifestHookSources(pluginDir)...)
}

// manifestHookSources reads the "hooks" field of the plugin's Claude Code
// manifest: a path, an array of paths, an inline hooks object, or an array
// mixing paths and inline objects. Paths are relative to the plugin dir; a
// path that escapes it, does not exist, or names hooks/hooks.json (already
// read by the caller) is skipped. A missing or malformed manifest or entry
// contributes nothing.
func manifestHookSources(pluginDir string) []hooksFile {
	data, err := os.ReadFile(filepath.Join(pluginDir, claudeManifestRel))
	if err != nil {
		return nil
	}
	var manifest struct {
		Hooks json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || len(manifest.Hooks) == 0 {
		return nil
	}
	entries := []json.RawMessage{manifest.Hooks}
	var list []json.RawMessage
	if err := json.Unmarshal(manifest.Hooks, &list); err == nil {
		entries = list
	}

	seen := map[string]bool{filepath.Join(pluginDir, "hooks", "hooks.json"): true}
	var sources []hooksFile
	for _, e := range entries {
		var rel string
		if err := json.Unmarshal(e, &rel); err != nil {
			if hf, ok := parseHooksObject(e); ok {
				sources = append(sources, hf)
			}
			continue
		}
		p := filepath.Join(pluginDir, filepath.FromSlash(rel))
		if !isSubpath(pluginDir, p) || seen[p] {
			continue
		}
		seen[p] = true
		if raw, err := os.ReadFile(p); err == nil {
			if hf, ok := parseHooksObject(raw); ok {
				sources = append(sources, hf)
			}
		}
	}
	return sources
}

// parseHooksObject parses a hooks object in either shape Claude Code accepts:
// wrapped as {"hooks": {<event>: [...]}} like hooks/hooks.json, or bare as
// {<event>: [...]}. ok is false when neither shape parses to any event.
func parseHooksObject(raw []byte) (hooksFile, bool) {
	var wrapped hooksFile
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Hooks) > 0 {
		return wrapped, true
	}
	var bare map[string][]hookMatcher
	if err := json.Unmarshal(raw, &bare); err == nil && len(bare) > 0 {
		return hooksFile{Hooks: bare}, true
	}
	return hooksFile{}, false
}

// sessionStartMatches reports whether a SessionStart matcher accepts the
// "startup" source. Empty and "*" match everything; anything else is a
// regular expression over the source name, as in Claude Code.
func sessionStartMatches(matcher string) bool {
	return MatchesHook(matcher, "startup")
}

// hookWaitDelay bounds how long a killed hook's output pipes may stay open. A
// hook that spawned a child (bash -c "a; b") leaves the child holding stdout,
// and Wait would otherwise block on it after the timeout or cancel fired.
const hookWaitDelay = 2 * time.Second

// errEmptyHookCommand is returned for a hook that declares no command.
var errEmptyHookCommand = errors.New("hook has no command")

// hookTimeout is the declared timeout of h, defaulted and capped.
func hookTimeout(h hookCommand) time.Duration {
	timeout := sessionStartDefaultTimeout
	if h.Timeout > 0 {
		timeout = time.Duration(h.Timeout * float64(time.Second))
	}
	if timeout > sessionStartMaxTimeout {
		timeout = sessionStartMaxTimeout
	}
	return timeout
}

// hookCmd builds the command for h in the plugin's environment: the expanded
// command line (exec form, powershell, cmd or bash), CLAUDE_PLUGIN_ROOT and
// CLAUDE_PROJECT_DIR exported, and the project root as cwd. Shared by
// SessionStart and the PostToolUse runner.
func hookCmd(ctx context.Context, h hookCommand, pluginDir, projectRoot string) *exec.Cmd {
	expand := func(s string) string {
		s = strings.ReplaceAll(s, "${CLAUDE_PLUGIN_ROOT}", pluginDir)
		return strings.ReplaceAll(s, "${CLAUDE_PROJECT_DIR}", projectRoot)
	}

	var cmd *exec.Cmd
	switch {
	case len(h.Args) > 0:
		// Exec form: no shell, each arg passed verbatim.
		args := make([]string, len(h.Args))
		for i, a := range h.Args {
			args[i] = expand(a)
		}
		cmd = exec.CommandContext(ctx, expand(h.Command), args...)
	case h.Shell == "powershell":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", expand(h.Command))
	case runtime.GOOS == "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/C", expand(h.Command))
	default:
		cmd = exec.CommandContext(ctx, "bash", "-c", expand(h.Command))
	}
	cmd.Env = append(os.Environ(),
		"CLAUDE_PLUGIN_ROOT="+pluginDir,
		"CLAUDE_PROJECT_DIR="+projectRoot,
		"OCODE=1",
	)
	if projectRoot != "" {
		cmd.Dir = projectRoot
		// The inherited PWD names the ocode process's cwd, not the project.
		cmd.Env = append(cmd.Env, "PWD="+projectRoot)
	}
	cmd.WaitDelay = hookWaitDelay
	return cmd
}

// runHookProcess runs h under parent (bounded by its declared timeout) and
// returns its captured stdout and exit code. stdin is written to the process
// when non-nil. A non-zero exit, a timeout or a cancel is an error; ExitCode is
// -1 when the process was killed or never started.
func runHookProcess(parent context.Context, h hookCommand, pluginDir, projectRoot string, stdin []byte) (string, int, error) {
	if strings.TrimSpace(h.Command) == "" {
		return "", -1, errEmptyHookCommand
	}
	ctx, cancel := context.WithTimeout(parent, hookTimeout(h))
	defer cancel()

	cmd := hookCmd(ctx, h, pluginDir, projectRoot)
	// Captured, never inherited: a hook writing to the process's stdout or
	// stderr would paint over the TUI's alt-screen.
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Stdin = nil
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	err := cmd.Run()
	if err == nil {
		return stdout.String(), 0, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.String(), -1, ctxErr
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode(), err
	}
	return "", -1, err
}

// runHookCommand runs a SessionStart hook and returns the context it prints.
// A failing hook contributes nothing.
func runHookCommand(h hookCommand, pluginDir, projectRoot string) string {
	out, _, err := runHookProcess(context.Background(), h, pluginDir, projectRoot, nil)
	if err != nil {
		return ""
	}
	return parseHookOutput(out)
}

// parseHookOutput extracts the context a SessionStart hook injects: the
// hookSpecificOutput.additionalContext of a JSON reply (also accepting the
// top-level additionalContext / additional_context spellings other hosts
// use), or the plain text itself when stdout is not JSON.
func parseHookOutput(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	if !strings.HasPrefix(out, "{") {
		return out
	}
	var reply struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
		AdditionalContext      string `json:"additionalContext"`
		AdditionalContextSnake string `json:"additional_context"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		return out
	}
	for _, s := range []string{
		reply.HookSpecificOutput.AdditionalContext,
		reply.AdditionalContext,
		reply.AdditionalContextSnake,
	} {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
