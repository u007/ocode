package plugins

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClaudeHooksForPostToolUse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook fixtures use bash")
	}
	home := isolatePluginEnv(t)
	proj := t.TempDir()

	// hooks/hooks.json source: one PostToolUse command hook plus a SessionStart
	// hook that must not be returned for PostToolUse.
	jsonDir := filepath.Join(home, ".claude", "plugins", "cache", "m", "jsonhook", "1")
	writeClaudePlugin(t, jsonDir, "jsonhook")
	writeFile(t, filepath.Join(jsonDir, "hooks", "hooks.json"), `{"hooks":{
		"PostToolUse":[{"matcher":"Bash|Edit","hooks":[{"type":"command","command":"echo json-hook"}]}],
		"SessionStart":[{"hooks":[{"type":"command","command":"echo start-hook"}]}]
	}}`)

	// Manifest hooks field (inline object). A non-command type is skipped.
	manifestDir := filepath.Join(home, ".claude", "plugins", "cache", "m", "manifesthook", "1")
	writeFile(t, filepath.Join(manifestDir, ".claude-plugin", "plugin.json"), `{"name":"manifesthook","hooks":{
		"PostToolUse":[{"matcher":"Read","hooks":[
			{"type":"command","command":"echo manifest-hook"},
			{"type":"prompt","command":"echo not-a-command"}
		]}]
	}}`)

	// An ocode-format plugin with a hooks dir is never executed.
	ocodeDir := filepath.Join(home, ".config", "opencode", "plugins", "ocodeonly")
	writeFile(t, filepath.Join(ocodeDir, "plugin.json"), `{"name":"ocodeonly"}`)
	writeFile(t, filepath.Join(ocodeDir, "hooks", "hooks.json"),
		`{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"echo MUST_NOT_RUN"}]}]}}`)

	installInClaudeCode(t, home, map[string]string{"jsonhook@m": jsonDir, "manifesthook@m": manifestDir})

	byPlugin := func(hooks []PluginHook) map[string]PluginHook {
		m := map[string]PluginHook{}
		for _, h := range hooks {
			m[h.Plugin] = h
		}
		return m
	}

	got := ClaudeHooksFor(nil, proj, "PostToolUse")
	if len(got) != 2 {
		t.Fatalf("got %d PostToolUse hooks, want 2: %+v", len(got), got)
	}
	m := byPlugin(got)
	if h, ok := m["jsonhook"]; !ok || h.Matcher != "Bash|Edit" || h.Command.Command != "echo json-hook" || h.Dir != jsonDir {
		t.Errorf("jsonhook = %+v, ok=%v", h, ok)
	}
	if h, ok := m["manifesthook"]; !ok || h.Matcher != "Read" || h.Command.Command != "echo manifest-hook" || h.Dir != manifestDir {
		t.Errorf("manifesthook = %+v, ok=%v", h, ok)
	}

	if n := len(ClaudeHooksFor(nil, proj, "PreToolUse")); n != 0 {
		t.Errorf("PreToolUse returned %d hooks, want 0", n)
	}

	// A disabled plugin contributes nothing.
	off := byPlugin(ClaudeHooksFor(map[string]bool{"jsonhook": false}, proj, "PostToolUse"))
	if _, ok := off["jsonhook"]; ok {
		t.Errorf("disabled jsonhook still returned")
	}
	if _, ok := off["manifesthook"]; !ok {
		t.Errorf("manifesthook missing when only jsonhook is disabled")
	}

	// Cached: a rewritten hooks file is not re-read until the cache is dropped.
	writeFile(t, filepath.Join(jsonDir, "hooks", "hooks.json"),
		`{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"echo changed"}]}]}}`)
	if again := byPlugin(ClaudeHooksFor(nil, proj, "PostToolUse")); again["jsonhook"].Command.Command != "echo json-hook" {
		t.Fatalf("cache not used, jsonhook now %q", again["jsonhook"].Command.Command)
	}
	InvalidateSessionStartCache()
	if again := byPlugin(ClaudeHooksFor(nil, proj, "PostToolUse")); again["jsonhook"].Command.Command != "echo changed" {
		t.Fatalf("invalidate did not drop the hook cache, jsonhook now %q", again["jsonhook"].Command.Command)
	}
}

func TestRunClaudeHookStdinAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook fixtures use bash")
	}
	dir := t.TempDir()
	cat := PluginHook{Plugin: "p", Dir: dir, Command: hookCommand{Type: "command", Command: "cat"}}
	payload := []byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash"}`)
	res := RunClaudeHook(context.Background(), cat, dir, payload)
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("cat: err=%v exit=%d", res.Err, res.ExitCode)
	}
	if res.Stdout != string(payload) {
		t.Fatalf("stdin not passed through: got %q", res.Stdout)
	}

	fail := PluginHook{Plugin: "p", Dir: dir, Command: hookCommand{Type: "command", Command: "cat >/dev/null; echo partial; exit 3"}}
	res = RunClaudeHook(context.Background(), fail, dir, payload)
	if res.ExitCode != 3 || res.Err == nil {
		t.Fatalf("exit 3: exit=%d err=%v", res.ExitCode, res.Err)
	}
	if res.Stdout != "partial\n" {
		t.Errorf("stdout of failing hook = %q", res.Stdout)
	}
}

func TestRunClaudeHookCancelKillsHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook fixtures use bash")
	}
	dir := t.TempDir()
	// bash forks sleep, so killing bash alone would leave the pipe open; the
	// WaitDelay bound must still return promptly.
	slow := PluginHook{Plugin: "p", Dir: dir, Command: hookCommand{Type: "command", Command: "sleep 30; echo late"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(200*time.Millisecond, cancel)
	defer timer.Stop()

	start := time.Now()
	res := RunClaudeHook(ctx, slow, dir, nil)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("cancelled hook took %v to return", elapsed)
	}
	if res.Err == nil || res.ExitCode == 0 {
		t.Fatalf("cancelled hook reported success: exit=%d err=%v", res.ExitCode, res.Err)
	}
	if strings.Contains(res.Stdout, "late") {
		t.Errorf("hook ran to completion despite cancel: %q", res.Stdout)
	}
}

func TestMatchesHook(t *testing.T) {
	cases := []struct {
		matcher, name string
		want          bool
	}{
		{"", "Bash", true},
		{"  ", "Bash", true},
		{"*", "mcp__x__y", true},
		{"Bash", "Bash", true},
		{"Bash", "BashX", false},
		{"Bash", "Bas", false},
		{"Bash|EnterWorktree", "EnterWorktree", true},
		{"Bash|EnterWorktree", "Read", false},
		{"^(WebFetch|WebSearch|Bash|mcp__.*)$", "WebFetch", true},
		{"^(WebFetch|WebSearch|Bash|mcp__.*)$", "mcp__github__create_issue", true},
		{"^(WebFetch|WebSearch|Bash|mcp__.*)$", "Read", false},
		{"mcp__.*", "mcp__github__create_issue", true},
		{"mcp__.*", "xmcp__github", false},
		{"[", "Bash", false},
		{"(", "startup", false},
	}
	for _, c := range cases {
		if got := MatchesHook(c.matcher, c.name); got != c.want {
			t.Errorf("MatchesHook(%q, %q) = %v, want %v", c.matcher, c.name, got, c.want)
		}
	}
}
