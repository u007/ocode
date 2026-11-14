package plugins

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// Claude Code command hooks for events other than SessionStart (PostToolUse
// today). The hook definitions come from the same hooks/hooks.json and
// manifest hooks field as SessionStart (pluginHookSources), run with the same
// command builder (hookCmd), and are limited to the same plugin set. The
// caller decides what a hook's reply means; this file only finds and runs
// them.

// PluginHook is one command hook a Claude Code-format plugin declares for an
// event. Matcher is the raw matcher string; see MatchesHook.
type PluginHook struct {
	Plugin  string
	Dir     string
	Matcher string
	Command hookCommand
}

// HookResult is the outcome of RunClaudeHook. ExitCode is -1 when the hook was
// killed (timeout or cancel) or never started; Err is non-nil for any exit
// other than 0, including the ctx error when the hook was cut short.
type HookResult struct {
	Stdout   string
	ExitCode int
	Err      error
}

// claudeHookEntry is one parsed command hook, for any event.
type claudeHookEntry struct {
	event   string
	matcher string
	cmd     hookCommand
}

var (
	claudeHookMu    sync.Mutex
	claudeHookCache = map[string][]claudeHookEntry{}
)

// invalidateClaudeHookCache drops the parsed hook definitions. Called from
// InvalidateSessionStartCache, so plugin install, update and remove reset both.
func invalidateClaudeHookCache() {
	claudeHookMu.Lock()
	claudeHookCache = map[string][]claudeHookEntry{}
	claudeHookMu.Unlock()
}

// ClaudeHooksFor returns the command hooks declared for event by the Claude
// Code-format plugins loaded for projectRoot, in plugin order and then
// declaration order. It uses the same plugin set as SessionStartContext, so an
// ocode-format plugin or a disabled plugin contributes nothing. Matchers are
// returned unevaluated; apply MatchesHook to each.
func ClaudeHooksFor(enabled map[string]bool, projectRoot, event string) []PluginHook {
	var out []PluginHook
	for _, p := range LoadPluginsForProject(enabled, projectRoot) {
		if !p.HasClaudeManifest {
			continue
		}
		for _, e := range cachedClaudeHooks(p.Dir, projectRoot) {
			if e.event != event {
				continue
			}
			out = append(out, PluginHook{Plugin: p.Name, Dir: p.Dir, Matcher: e.matcher, Command: e.cmd})
		}
	}
	return out
}

// cachedClaudeHooks parses the plugin's command hooks once per (project root,
// plugin dir) for the life of the process, so a tool call never re-reads the
// hook files.
func cachedClaudeHooks(pluginDir, projectRoot string) []claudeHookEntry {
	key := projectRoot + "\x00" + pluginDir
	claudeHookMu.Lock()
	entries, ok := claudeHookCache[key]
	claudeHookMu.Unlock()
	if ok {
		return entries
	}
	entries = parseClaudeHooks(pluginDir)
	claudeHookMu.Lock()
	claudeHookCache[key] = entries
	claudeHookMu.Unlock()
	return entries
}

// parseClaudeHooks keeps the command hooks of every event the plugin declares.
// Hooks of another type, and command hooks with no command, are dropped.
func parseClaudeHooks(pluginDir string) []claudeHookEntry {
	var entries []claudeHookEntry
	for _, src := range pluginHookSources(pluginDir) {
		for event, matchers := range src.Hooks {
			for _, m := range matchers {
				for _, h := range m.Hooks {
					if h.Type != "" && h.Type != "command" {
						continue
					}
					if strings.TrimSpace(h.Command) == "" {
						continue
					}
					entries = append(entries, claudeHookEntry{event: event, matcher: m.Matcher, cmd: h})
				}
			}
		}
	}
	return entries
}

// RunClaudeHook runs one hook with the same environment, cwd, shell selection
// and timeout as SessionStart hooks. ctx is the parent of the hook's timeout,
// so cancelling it kills the process. stdin is written to the hook when
// non-nil. Stdout is captured; stderr is discarded.
func RunClaudeHook(ctx context.Context, h PluginHook, projectRoot string, stdin []byte) HookResult {
	out, code, err := runHookProcess(ctx, h.Command, h.Dir, projectRoot, stdin)
	return HookResult{Stdout: out, ExitCode: code, Err: err}
}

// MatchesHook reports whether a hook matcher accepts name. Empty and "*" match
// everything. Anything else is a regular expression that must match the whole
// name, as in Claude Code; an invalid expression matches nothing. SessionStart
// uses it with the name "startup".
func MatchesHook(matcher, name string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return true
	}
	re, err := regexp.Compile("^(?:" + matcher + ")$")
	if err != nil {
		return false
	}
	return re.MatchString(name)
}
