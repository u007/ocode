package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Claude Code SessionStart hooks.
//
// A Claude Code plugin has no always-on instructions field; it injects its
// standing context from a SessionStart command hook in hooks/hooks.json
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

// runSessionStartHooks runs every SessionStart command hook in the plugin's
// hooks/hooks.json whose matcher accepts "startup", and joins their output.
// A failing hook contributes nothing; it never fails the session.
func runSessionStartHooks(pluginDir, projectRoot string) string {
	data, err := os.ReadFile(filepath.Join(pluginDir, "hooks", "hooks.json"))
	if err != nil {
		return ""
	}
	var hf hooksFile
	if err := json.Unmarshal(data, &hf); err != nil {
		return ""
	}
	var parts []string
	for _, m := range hf.Hooks["SessionStart"] {
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
	return strings.Join(parts, "\n\n")
}

// sessionStartMatches reports whether a SessionStart matcher accepts the
// "startup" source. Empty and "*" match everything; anything else is a
// regular expression over the source name, as in Claude Code.
func sessionStartMatches(matcher string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return true
	}
	re, err := regexp.Compile("^(?:" + matcher + ")$")
	if err != nil {
		return false
	}
	return re.MatchString("startup")
}

func runHookCommand(h hookCommand, pluginDir, projectRoot string) string {
	if strings.TrimSpace(h.Command) == "" {
		return ""
	}
	timeout := sessionStartDefaultTimeout
	if h.Timeout > 0 {
		timeout = time.Duration(h.Timeout * float64(time.Second))
	}
	if timeout > sessionStartMaxTimeout {
		timeout = sessionStartMaxTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
	// Captured, never inherited: a hook writing to the process's stdout or
	// stderr would paint over the TUI's alt-screen.
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	return parseHookOutput(stdout.String())
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
