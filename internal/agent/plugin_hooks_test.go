package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/tool"
)

func TestClaudeToolName(t *testing.T) {
	cases := map[string]string{
		"bash":      "Bash",
		"read":      "Read",
		"write":     "Write",
		"edit":      "Edit",
		"multiedit": "MultiEdit",
		"grep":      "Grep",
		"glob":      "Glob",
		"webfetch":  "WebFetch",
		"websearch": "WebSearch",
		"task":      "Task",
		// Not in the table: the ocode name is kept.
		"multi_file_edit": "multi_file_edit",
		"todowrite":       "todowrite",
	}
	for in, want := range cases {
		if got := claudeToolName(in); got != want {
			t.Errorf("claudeToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPluginHookToolNameMCP(t *testing.T) {
	a := newTestAgent(nil, nil, nil, nil)
	a.toolsMu.Lock()
	a.mcpServers["github_create_issue"] = "github"
	a.toolsMu.Unlock()
	if got := a.pluginHookToolName("github_create_issue"); got != "mcp__github__create_issue" {
		t.Errorf("MCP tool name = %q, want mcp__github__create_issue", got)
	}
	if got := a.pluginHookToolName("bash"); got != "Bash" {
		t.Errorf("built-in tool name = %q, want Bash", got)
	}
}

func TestApplyPostToolHookReply(t *testing.T) {
	const base = "original output"
	cases := []struct {
		name     string
		stdout   string
		want     string
		wantNote bool
	}{
		{name: "empty stdout", stdout: "  \n", want: base},
		{name: "plain text", stdout: "hello", want: base, wantNote: true},
		{name: "json array", stdout: `[1,2]`, want: base, wantNote: true},
		{name: "broken json", stdout: `{"decision":`, want: base, wantNote: true},
		{name: "json without fields", stdout: `{"continue":true}`, want: base},
		{
			name:   "additionalContext under header",
			stdout: `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"  check the lint  "}}`,
			want:   base + "\n\n[plugin demo PostToolUse]\ncheck the lint",
		},
		{
			name:   "updatedToolOutput string replaces as is",
			stdout: `{"hookSpecificOutput":{"updatedToolOutput":"  replaced  "}}`,
			want:   "  replaced  ",
		},
		{
			name:   "top-level updatedToolOutput string replaces",
			stdout: `{"updatedToolOutput":"top"}`,
			want:   "top",
		},
		{
			name:   "updatedToolOutput object becomes compact JSON",
			stdout: `{"hookSpecificOutput":{"updatedToolOutput":{"ok": true, "n": [1, 2]}}}`,
			want:   `{"ok":true,"n":[1,2]}`,
		},
		{
			name:   "hookSpecificOutput updatedToolOutput wins over top level",
			stdout: `{"updatedToolOutput":"top","hookSpecificOutput":{"updatedToolOutput":"specific"}}`,
			want:   "specific",
		},
		{
			name:   "null updatedToolOutput leaves result",
			stdout: `{"updatedToolOutput":null}`,
			want:   base,
		},
		{
			name:   "block reason appended",
			stdout: `{"decision":"block","reason":"secrets found"}`,
			want:   base + "\n\n[plugin demo PostToolUse]\nsecrets found",
		},
		{
			name:   "block without reason changes nothing",
			stdout: `{"decision":"block"}`,
			want:   base,
		},
		{
			name:   "order: replace, then block reason, then additionalContext",
			stdout: `{"updatedToolOutput":"new","decision":"block","reason":"why","hookSpecificOutput":{"additionalContext":"extra"}}`,
			want:   "new\n\n[plugin demo PostToolUse]\nwhy\n\n[plugin demo PostToolUse]\nextra",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, note := applyPostToolHookReply(base, "demo", c.stdout)
			if got != c.want {
				t.Errorf("result = %q, want %q", got, c.want)
			}
			if (note != "") != c.wantNote {
				t.Errorf("note = %q, wantNote %v", note, c.wantNote)
			}
			if c.wantNote && !strings.Contains(note, "demo") {
				t.Errorf("note %q does not name the plugin", note)
			}
		})
	}
}

// writeAgentHookPlugin installs a Claude Code-format plugin under the fake
// home's ocode plugin dir with one PostToolUse command hook.
func writeAgentHookPlugin(t *testing.T, home, name, matcher, command string) {
	t.Helper()
	writeAgentEventHookPlugin(t, home, name, "PostToolUse", matcher, command)
}

// writeAgentEventHookPlugin is writeAgentHookPlugin for any event name.
func writeAgentEventHookPlugin(t *testing.T, home, name, event, matcher, command string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "opencode", "plugins", name)
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"` + name + `"}`
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	hooks := map[string]any{"hooks": map[string]any{event: []any{map[string]any{
		"matcher": matcher,
		"hooks":   []any{map[string]any{"type": "command", "command": command}},
	}}}}
	data, err := json.Marshal(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "hooks.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// pluginHookAgent builds an agent whose project is proj, inside a fresh fake
// home with the bundled plugins disabled. Plugin hook output is cached per
// process, so each test gets its own plugin dir and the cache is dropped.
func pluginHookAgent(t *testing.T) (*Agent, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook fixtures use bash")
	}
	home := t.TempDir()
	setHomeTree(t, home)
	prev := bundled.PluginsDir
	bundled.PluginsDir = ""
	t.Cleanup(func() { bundled.PluginsDir = prev })
	plugins.InvalidateSessionStartCache()
	t.Cleanup(plugins.InvalidateSessionStartCache)

	proj := t.TempDir()
	a := newTestAgent(nil, nil, nil, nil)
	a.SetWorkDir(proj)
	return a, home, proj
}

func TestPluginPostToolHookNoMatchLeavesResult(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	marker := filepath.Join(home, "ran.txt")
	writeAgentHookPlugin(t, home, "readonly", "Read", "touch "+marker+"; echo '{\"updatedToolOutput\":\"changed\"}'")

	got := a.runPluginPostToolHooks(context.Background(), "bash", `{"command":"ls"}`, "listing")
	if got != "listing" {
		t.Fatalf("result = %q, want unchanged", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("non-matching hook ran")
	}
}

func TestPluginPostToolHookAdditionalContextAndPayload(t *testing.T) {
	a, home, proj := pluginHookAgent(t)
	capture := filepath.Join(home, "stdin.json")
	writeAgentHookPlugin(t, home, "ctxhook", "Bash|Edit",
		`cat > "`+capture+`"; echo '{"hookSpecificOutput":{"additionalContext":"run the linter next"}}'`)

	args := `{"command":"go test ./..."}`
	got := a.runPluginPostToolHooks(context.Background(), "bash", args, "ok")
	want := "ok\n\n[plugin ctxhook PostToolUse]\nrun the linter next"
	if got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}

	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("hook did not receive stdin: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("stdin is not JSON: %v\n%s", err, raw)
	}
	if payload["hook_event_name"] != "PostToolUse" || payload["tool_name"] != "Bash" {
		t.Errorf("event/tool = %v / %v", payload["hook_event_name"], payload["tool_name"])
	}
	if payload["tool_response"] != "ok" {
		t.Errorf("tool_response = %v", payload["tool_response"])
	}
	input, ok := payload["tool_input"].(map[string]any)
	if !ok || input["command"] != "go test ./..." {
		t.Errorf("tool_input = %#v, want the argument object", payload["tool_input"])
	}
	if cwd, _ := payload["cwd"].(string); cwd == "" || !sameDir(cwd, proj) {
		t.Errorf("cwd = %v, want %s", payload["cwd"], proj)
	}
}

func TestPluginPostToolHookUpdatedOutputReplaces(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	writeAgentHookPlugin(t, home, "replacer", "Read",
		`echo '{"hookSpecificOutput":{"updatedToolOutput":{"redacted":true}}}'`)
	got := a.runPluginPostToolHooks(context.Background(), "read", `{"path":"a.txt"}`, "secret")
	if got != `{"redacted":true}` {
		t.Fatalf("result = %q", got)
	}
}

func TestPluginPostToolHookBlockReasonAppended(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	writeAgentHookPlugin(t, home, "blocker", "WebFetch",
		`echo '{"decision":"block","reason":"domain is not allowed"}'`)
	got := a.runPluginPostToolHooks(context.Background(), "webfetch", `{"url":"https://x.example"}`, "page")
	if want := "page\n\n[plugin blocker PostToolUse]\ndomain is not allowed"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

func TestPluginPostToolHookFailuresLeaveResult(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	writeAgentHookPlugin(t, home, "failing", "Bash", `echo '{"updatedToolOutput":"bad"}'; exit 1`)
	if got := a.runPluginPostToolHooks(context.Background(), "bash", `{"command":"x"}`, "fine"); got != "fine" {
		t.Fatalf("exit 1 changed the result to %q", got)
	}

	b, home2, _ := pluginHookAgent(t)
	writeAgentHookPlugin(t, home2, "chatty", "Bash", `echo "just some text"`)
	if got := b.runPluginPostToolHooks(context.Background(), "bash", `{"command":"x"}`, "fine"); got != "fine" {
		t.Fatalf("non-JSON stdout changed the result to %q", got)
	}
}

func TestPluginPostToolHookSentinelNotPassed(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	marker := filepath.Join(home, "ran.txt")
	writeAgentHookPlugin(t, home, "spy", "*", "touch "+marker)

	for _, sentinel := range []string{
		tool.SentinelPermissionAsk + " {\"tool\":\"bash\"}",
		tool.SentinelQuestionPrompt + "{}\n" + tool.SentinelWaitingForUser,
	} {
		if got := a.runPluginPostToolHooks(context.Background(), "bash", `{}`, sentinel); got != sentinel {
			t.Errorf("sentinel result changed: %q", got)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("hook ran for a sentinel result")
	}
}

// sameDir reports whether a and b name the same directory, resolving symlinks
// (macOS temp dirs live under /var, which is /private/var).
func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func TestPluginHooksForMemoisedPerAgent(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	pluginDir := filepath.Join(home, ".config", "opencode", "plugins", "memo")
	writeAgentHookPlugin(t, home, "memo", "*", "true")

	first := a.pluginHooksFor("PostToolUse")
	if len(first) != 1 || first[0].Plugin != "memo" {
		t.Fatalf("first lookup = %+v, want one hook from memo", first)
	}
	// Premise: removing the plugin makes a fresh read find nothing.
	if err := os.RemoveAll(pluginDir); err != nil {
		t.Fatal(err)
	}
	if fresh := plugins.ClaudeHooksFor(a.pluginEnabledMap(), a.pluginHookRoot(), "PostToolUse"); len(fresh) != 0 {
		t.Fatalf("premise broken: a fresh read still finds %d hook(s)", len(fresh))
	}
	second := a.pluginHooksFor("PostToolUse")
	if len(second) != 1 || second[0].Plugin != "memo" {
		t.Fatalf("second lookup = %+v, want the memoised hook without a disk read", second)
	}
}

// promptBase is a transcript whose last user-role message is the human prompt.
func promptBase(human string) []Message {
	return []Message{{Role: "system", Content: "system prompt"}, {Role: "user", Content: human}}
}

// hookRunCount counts the "x" lines a counter hook appended.
func hookRunCount(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), "x")
}

func TestUserPromptSubmitPlainTextInjected(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	writeAgentEventHookPlugin(t, home, "style", "UserPromptSubmit", "", `echo "use the house style guide"`)
	base := promptBase("refactor the parser")

	got := a.injectPluginPromptTail(context.Background(), base)
	if len(got) != len(base)+1 {
		t.Fatalf("len = %d, want %d", len(got), len(base)+1)
	}
	tail := got[len(got)-1]
	if tail.Role != "user" {
		t.Errorf("tail role = %q, want user", tail.Role)
	}
	want := "[ocode:plugin-hook]\n\n--- Plugin: style (UserPromptSubmit) ---\nuse the house style guide"
	if tail.Content != want {
		t.Errorf("tail content = %q, want %q", tail.Content, want)
	}
	if len(base) != 2 {
		t.Errorf("base was mutated: len %d", len(base))
	}
}

func TestUserPromptSubmitJSONAdditionalContext(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	writeAgentEventHookPlugin(t, home, "js", "UserPromptSubmit", "",
		`echo '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"  json context  "}}'`)
	got := a.injectPluginPromptTail(context.Background(), promptBase("hi"))
	want := "--- Plugin: js (UserPromptSubmit) ---\njson context"
	if tail := got[len(got)-1].Content; !strings.HasSuffix(tail, want) || !strings.HasPrefix(tail, pluginPromptMarker) {
		t.Fatalf("tail = %q, want the marker then %q", tail, want)
	}
}

func TestUserPromptSubmitRunsOncePerPrompt(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	counter := filepath.Join(home, "prompt-runs.txt")
	writeAgentEventHookPlugin(t, home, "counter", "UserPromptSubmit", "",
		`echo x >> "`+counter+`"; echo "noted"`)
	ctx := context.Background()

	base := promptBase("first prompt")
	first := a.injectPluginPromptTail(ctx, base)
	second := a.injectPluginPromptTail(ctx, base)
	if n := hookRunCount(t, counter); n != 1 {
		t.Fatalf("hook ran %d times for one prompt across two assemblies, want 1", n)
	}
	if first[len(first)-1].Content != second[len(second)-1].Content {
		t.Fatalf("second assembly changed the injected text")
	}

	// A later request in the same turn: a tool round has been appended.
	turn := append(append([]Message(nil), base...),
		Message{Role: "assistant", Content: "reading"},
		Message{Role: "tool", ToolID: "c1", Content: "ok"})
	a.injectPluginPromptTail(ctx, turn)
	if n := hookRunCount(t, counter); n != 1 {
		t.Fatalf("hook ran %d times during one turn, want 1", n)
	}

	// A new human prompt with the same text runs the hooks again.
	next := append(append([]Message(nil), turn...),
		Message{Role: "assistant", Content: "done"},
		Message{Role: "user", Content: "first prompt"})
	a.injectPluginPromptTail(ctx, next)
	if n := hookRunCount(t, counter); n != 2 {
		t.Fatalf("hook ran %d times after a second prompt, want 2", n)
	}
}

func TestUserPromptSubmitCompactionDoesNotRerun(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	counter := filepath.Join(home, "compact-runs.txt")
	writeAgentEventHookPlugin(t, home, "compact", "UserPromptSubmit", "",
		`echo x >> "`+counter+`"; echo "noted"`)
	ctx := context.Background()

	a.injectPluginPromptTail(ctx, []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "earlier question"},
		{Role: "assistant", Content: "earlier answer"},
		{Role: "user", Content: "yes"},
	})
	// Compaction drops the older turns and leaves a summary; the prompt is now
	// the first human message, so its ordinal changed. It must not run again.
	compacted := []Message{
		{Role: "system", Content: compactionSummaryMarker + "\nearlier turns summarised"},
		{Role: "user", Content: "yes"},
	}
	got := a.injectPluginPromptTail(ctx, compacted)
	if n := hookRunCount(t, counter); n != 1 {
		t.Fatalf("hook ran %d times across compaction, want 1", n)
	}
	if got[len(got)-1].Role != "user" || !strings.HasPrefix(got[len(got)-1].Content, pluginPromptMarker) {
		t.Fatalf("compacted assembly lost the remembered output: %+v", got[len(got)-1])
	}
}

func TestUserPromptSubmitUserSeqIdentity(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	counter := filepath.Join(home, "seq-runs.txt")
	writeAgentEventHookPlugin(t, home, "seq", "UserPromptSubmit", "",
		`echo x >> "`+counter+`"; echo "noted"`)
	ctx := context.Background()

	seven := []Message{{Role: "user", Content: "same"}}
	seven[0].UserSeq = 7
	a.injectPluginPromptTail(ctx, seven)
	a.injectPluginPromptTail(ctx, seven)
	if n := hookRunCount(t, counter); n != 1 {
		t.Fatalf("UserSeq 7 ran %d times, want 1", n)
	}
	eight := []Message{{Role: "assistant", Content: "ok"}, {Role: "user", Content: "same"}}
	eight[1].UserSeq = 8
	a.injectPluginPromptTail(ctx, eight)
	if n := hookRunCount(t, counter); n != 2 {
		t.Fatalf("UserSeq 8 ran %d times in total, want 2", n)
	}
}

func TestUserPromptSubmitSyntheticLastMessageDoesNotRun(t *testing.T) {
	synthetic := map[string]string{
		"goal resume":   goalMarker + "\nGoal not yet met: ship it",
		"auto-continue": AutoContinueResumePrompt,
		"notes notice":  ocLogMarker + "\n<oc-log since=\"1\"></oc-log>",
	}
	for name, content := range synthetic {
		t.Run(name, func(t *testing.T) {
			a, home, _ := pluginHookAgent(t)
			counter := filepath.Join(home, "synthetic-runs.txt")
			writeAgentEventHookPlugin(t, home, "spy", "UserPromptSubmit", "",
				`echo x >> "`+counter+`"; echo "should not appear"`)
			base := []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "fix the bug"},
				{Role: "assistant", Content: "partial"},
				{Role: "user", Content: content},
			}
			got := a.injectPluginPromptTail(context.Background(), base)
			if len(got) != len(base) {
				t.Fatalf("synthetic last message injected a block: len %d, want %d", len(got), len(base))
			}
			if n := hookRunCount(t, counter); n != 0 {
				t.Fatalf("hook ran for a synthetic message (%d runs)", n)
			}
		})
	}
}

func TestUserPromptSubmitSkipsSubAgentAndHelper(t *testing.T) {
	setups := map[string]func(*Agent){
		"sub-agent": func(a *Agent) { a.isSubAgent = true },
		"helper":    func(a *Agent) { a.MarkHelperAgent() },
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			a, home, _ := pluginHookAgent(t)
			counter := filepath.Join(home, "helper-runs.txt")
			writeAgentEventHookPlugin(t, home, "spy", "UserPromptSubmit", "",
				`echo x >> "`+counter+`"; echo "should not appear"`)
			setup(a)
			base := promptBase("a tool-written prompt")
			if got := a.injectPluginPromptTail(context.Background(), base); len(got) != len(base) {
				t.Fatalf("%s injected a block", name)
			}
			if n := hookRunCount(t, counter); n != 0 {
				t.Fatalf("%s ran the hook %d time(s)", name, n)
			}
		})
	}
}

func TestUserPromptSubmitBlockAndFailureAddNothing(t *testing.T) {
	cases := []struct {
		name, plugin, command string
	}{
		{"block decision", "blocker", `echo '{"decision":"block","reason":"no"}'`},
		{"non-zero exit", "failing", `echo "partial"; exit 1`},
		{"broken json", "broken", `echo '{"decision":'`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, home, _ := pluginHookAgent(t)
			writeAgentEventHookPlugin(t, home, c.plugin, "UserPromptSubmit", "", c.command)
			base := promptBase("hello")
			got := a.injectPluginPromptTail(context.Background(), base)
			if len(got) != len(base) {
				t.Fatalf("%s added a block: %+v", c.name, got[len(got)-1])
			}
		})
	}
}

func TestUserPromptSubmitNoHooksUnchanged(t *testing.T) {
	a, _, _ := pluginHookAgent(t)
	base := promptBase("hello")
	got := a.injectPluginPromptTail(context.Background(), base)
	if len(got) != len(base) || got[len(got)-1].Content != "hello" {
		t.Fatalf("no hooks changed the transcript: %+v", got)
	}
}

func TestUserPromptSubmitIgnoresSelectionBlock(t *testing.T) {
	a, home, _ := pluginHookAgent(t)
	counter := filepath.Join(home, "selection-runs.txt")
	writeAgentEventHookPlugin(t, home, "sel", "UserPromptSubmit", "",
		`echo x >> "`+counter+`"; echo "selection-aware"`)
	base := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "explain this"},
		{Role: "user", Content: promptSelectionMarker + "\nfile.go:10"},
	}
	got := a.injectPluginPromptTail(context.Background(), base)
	if len(got) != len(base)+1 || !strings.Contains(got[len(got)-1].Content, "selection-aware") {
		t.Fatalf("selection block hid the human prompt: %+v", got)
	}
	if n := hookRunCount(t, counter); n != 1 {
		t.Fatalf("hook ran %d times, want 1", n)
	}
}

func TestSubagentStartMatcherAndMessage(t *testing.T) {
	a, home, proj := pluginHookAgent(t)
	capture := filepath.Join(home, "start.json")
	writeAgentEventHookPlugin(t, home, "explorer-rules", "SubagentStart", "explore|general",
		`cat > "`+capture+`"; echo '{"hookSpecificOutput":{"additionalContext":"report file paths only"}}'`)
	writeAgentEventHookPlugin(t, home, "plain", "SubagentStart", "explore", `echo "keep the diff small"`)
	ctx := context.Background()

	msg, ok := a.subagentStartMessage(ctx, "explore")
	if !ok {
		t.Fatal("no SubagentStart message for explore")
	}
	if msg.Role != "system" {
		t.Errorf("role = %q, want system", msg.Role)
	}
	for _, want := range []string{
		"Plugin context (SubagentStart):",
		"--- Plugin: explorer-rules ---\nreport file paths only",
		"--- Plugin: plain ---\nkeep the diff small",
	} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("message %q does not contain %q", msg.Content, want)
		}
	}

	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("hook did not receive stdin: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("stdin is not JSON: %v\n%s", err, raw)
	}
	if payload["hook_event_name"] != "SubagentStart" || payload["agent_type"] != "explore" {
		t.Errorf("payload event/agent_type = %v / %v", payload["hook_event_name"], payload["agent_type"])
	}
	if cwd, _ := payload["cwd"].(string); cwd == "" || !sameDir(cwd, proj) {
		t.Errorf("cwd = %v, want %s", payload["cwd"], proj)
	}

	if _, ok := a.subagentStartMessage(ctx, "review"); ok {
		t.Errorf("a non-matching agent type got SubagentStart context")
	}
}
