package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type retryTestClient struct{}

func (retryTestClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return nil, context.DeadlineExceeded
}

func (retryTestClient) GetProvider() string { return "test" }

func (retryTestClient) GetModel() string { return "test-model" }

type nestedTaskClient struct {
	responses []*agent.Message
	mu        sync.Mutex
	idx       int
}

func (c *nestedTaskClient) Chat(messages []agent.Message, tools []map[string]interface{}) (*agent.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idx >= len(c.responses) {
		return &agent.Message{Role: "assistant", Content: "done"}, nil
	}
	r := c.responses[c.idx]
	c.idx++
	return r, nil
}

func (c *nestedTaskClient) GetProvider() string { return "test" }

func (c *nestedTaskClient) GetModel() string { return "test-model" }

type askOnlyTool struct{}

func (askOnlyTool) Name() string        { return "ask_tool" }
func (askOnlyTool) Description() string { return "requires permission" }
func (askOnlyTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "ask_tool"}
}
func (askOnlyTool) Execute(args json.RawMessage) (string, error) { return "executed", nil }
func (askOnlyTool) Parallel() bool                               { return false }

func makeAgentToolCall(id, name, args string) agent.ToolCall {
	tc := agent.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

func TestLeaderTimeoutClearsActiveState(t *testing.T) {
	m := model{leaderActive: true, leaderSeq: 1}

	updated, _ := m.Update(leaderTimeoutMsg{seq: 1})
	got := updated.(model)

	if got.leaderActive {
		t.Fatal("expected leader mode to clear after timeout")
	}
}

func TestInitialToolsIncludesList(t *testing.T) {
	m := model{}
	tools := m.getInitialTools()
	for _, tool := range tools {
		if tool.Name() == "list" {
			return
		}
	}

	t.Fatal("expected default tools to include list")
}

func TestFormatReadToolCallHintShowsLineParams(t *testing.T) {
	var tc agent.ToolCall
	tc.Function.Name = "read"
	tc.Function.Arguments = `{"filePath":"/tmp/model.go","offset":400,"limit":51}`

	got := formatToolCallHint(tc)
	want := "📖 read /tmp/model.go offset=400 limit=51"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestShellExecCommandUsesPlatformShell(t *testing.T) {
	cmd := shellExecCommand("echo hello")
	if runtime.GOOS == "windows" {
		if cmd.Path == "" || len(cmd.Args) != 3 || cmd.Args[1] != "/C" || cmd.Args[2] != "echo hello" {
			t.Fatalf("expected cmd /C invocation, got path=%q args=%v", cmd.Path, cmd.Args)
		}
		return
	}
	if cmd.Path == "" || len(cmd.Args) != 3 || cmd.Args[1] != "-c" || cmd.Args[2] != "echo hello" {
		t.Fatalf("expected bash -c invocation, got path=%q args=%v", cmd.Path, cmd.Args)
	}
}

func TestShellFinishedMessageIsRecorded(t *testing.T) {
	m := model{
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-shell",
	}

	updated, _ := m.Update(shellFinishedMsg{command: "echo hello", output: "hello\n", toolCallID: "shell-test"})
	got := updated.(model)
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "hello") {
		t.Fatalf("expected shell output recorded as tool result, got %#v", got.messages)
	}
	last := got.messages[len(got.messages)-1].raw
	if last == nil || last.Role != "tool" || last.ToolID != "shell-test" {
		t.Fatalf("expected raw tool message with matching ToolID, got %#v", last)
	}
}

func TestCommandRunningCounterTracksOverlappingCompletions(t *testing.T) {
	m := model{
		input:    newTestTextarea(),
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:   ApplyThemeColors("tokyonight"),
	}
	m.markCmdStarted()
	m.markCmdStarted()

	updated, _ := m.Update(shellFinishedMsg{command: "echo one", output: "one\n", toolCallID: "shell-1"})
	got := updated.(model)
	if !got.cmdRunning() {
		t.Fatal("expected command-running state to remain active after first completion")
	}

	updated, _ = got.Update(cmdFinishedMsg{})
	got = updated.(model)
	if got.cmdRunning() {
		t.Fatal("expected command-running state to clear after all completions")
	}
}

func TestRenderStatusReflectsCommandRunningState(t *testing.T) {
	m := model{
		ready:     true,
		width:     120,
		activeTab: tabChat,
		styles:    ApplyThemeColors("tokyonight"),
		input:     newTestTextarea(),
	}

	m.markCmdStarted()
	status := m.renderStatus()
	if strings.Contains(status, "LLM: ○ idle") {
		t.Fatalf("expected non-idle status while command is running, got %q", status)
	}

	m.markCmdFinished()
	status = m.renderStatus()
	if !strings.Contains(status, "LLM: ○ idle") {
		t.Fatalf("expected idle status after command completion, got %q", status)
	}
}

func TestRenderStatusUsesActiveAgentSpec(t *testing.T) {
	m := model{
		agent:     agent.NewAgent(retryTestClient{}, nil, nil),
		ready:     true,
		width:     140,
		activeTab: tabChat,
		styles:    ApplyThemeColors("tokyonight"),
		input:     newTestTextarea(),
	}

	m.switchAgent("explore")

	status := m.renderStatus()
	if !strings.Contains(status, "explore") {
		t.Fatalf("expected status to reflect active agent spec, got %q", status)
	}
}

func TestSwitchAgentRejectsHiddenHelper(t *testing.T) {
	m := model{
		agent:     agent.NewAgent(retryTestClient{}, nil, nil),
		ready:     true,
		width:     140,
		activeTab: tabChat,
		styles:    ApplyThemeColors("tokyonight"),
		input:     newTestTextarea(),
	}

	m.switchAgent("explore")
	before := m.agent.Spec()
	if before == nil || before.Name != "explore" {
		t.Fatalf("setup: expected active spec to be explore, got %+v", before)
	}

	beforeMsgs := len(m.messages)
	m.switchAgent("title")
	after := m.agent.Spec()
	if after == nil || after.Name != "explore" {
		t.Fatalf("expected hidden helper %q not to replace active spec, got %+v", "title", after)
	}
	if got := len(m.messages) - beforeMsgs; got != 1 {
		t.Fatalf("expected one rejection message, got %d new messages", got)
	}
	if !strings.Contains(m.messages[len(m.messages)-1].text, "title") {
		t.Fatalf("expected rejection message to mention hidden agent name, got %q", m.messages[len(m.messages)-1].text)
	}
}

func TestHandleCommandSlashDispatchRejectsHiddenHelper(t *testing.T) {
	m := model{
		agent:     agent.NewAgent(retryTestClient{}, nil, nil),
		ready:     true,
		width:     140,
		activeTab: tabChat,
		styles:    ApplyThemeColors("tokyonight"),
		input:     newTestTextarea(),
	}

	m.switchAgent("explore")
	updated, _ := m.handleCommand("/compaction")
	got, ok := updated.(*model)
	if !ok {
		t.Fatalf("expected *model from handleCommand, got %T", updated)
	}
	if got.agent.Spec() == nil || got.agent.Spec().Name != "explore" {
		t.Fatalf("expected /compaction to leave active spec as explore, got %+v", got.agent.Spec())
	}
	last := got.messages[len(got.messages)-1].text
	if !strings.Contains(last, "/compaction") {
		t.Fatalf("expected rejection message to mention /compaction, got %q", last)
	}
}

func TestRunAgentCmdRejectsHiddenHelper(t *testing.T) {
	m := model{
		agent:     agent.NewAgent(retryTestClient{}, nil, nil),
		ready:     true,
		width:     140,
		activeTab: tabChat,
		styles:    ApplyThemeColors("tokyonight"),
		input:     newTestTextarea(),
	}

	m.switchAgent("explore")
	runAgentCmd(&m, []string{"title"})
	if m.agent.Spec() == nil || m.agent.Spec().Name != "explore" {
		t.Fatalf("expected /agent title to leave active spec as explore, got %+v", m.agent.Spec())
	}
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "title") {
		t.Fatalf("expected rejection message to mention hidden agent, got %q", last)
	}
}

func TestRenderStatusShowsActiveSubagentModel(t *testing.T) {
	mainAgent := agent.NewAgent(retryTestClient{}, nil, nil)
	run := mainAgent.Runs().New("explore")
	run.Sub = agent.NewAgent(retryTestClient{}, nil, nil)

	m := model{
		agent:     mainAgent,
		ready:     true,
		width:     140,
		activeTab: tabChat,
		styles:    ApplyThemeColors("tokyonight"),
		input:     newTestTextarea(),
	}

	status := m.renderStatus()
	if !strings.Contains(status, "subagent: test/test-model") {
		t.Fatalf("expected status to include active subagent model, got %q", status)
	}
}

func TestPermissionViewportIsSyncedDuringLayout(t *testing.T) {
	req := agent.PermissionRequest{
		ToolName: "read",
		Args:     json.RawMessage(`{"path":"notes.txt"}`),
	}
	m := model{
		ready:             true,
		width:             120,
		height:            40,
		showPermDialog:    true,
		pendingPermission: req,
		styles:            ApplyThemeColors("tokyonight"),
		input:             newTestTextarea(),
		permViewport:      viewport.New(viewport.WithWidth(1), viewport.WithHeight(1)),
	}

	m.layout()

	contentWidth := max(0, m.panelWidth()-4)
	want := permissionDialogVisibleBodyLines(renderPermissionRequestBody(req), contentWidth)
	if got := m.permViewport.VisibleLineCount(); got != want {
		t.Fatalf("expected permission viewport to be synced during layout, want %d visible lines, got %d", want, got)
	}
	if got := m.permViewport.TotalLineCount(); got == 0 {
		t.Fatal("expected permission viewport content to be populated during layout")
	}
}

// TestAlwaysAllowConfirmationDefersPersist verifies the two-step always-allow
// flow: pressing "t" (or "a") opens a confirmation step and persists nothing;
// the rule is written only after the user confirms with "y"; backing out with
// "n" returns to step 1 having persisted nothing.
func TestAlwaysAllowConfirmationDefersPersist(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate persistPermissions disk writes

	newModel := func() model {
		a := agent.NewAgent(retryTestClient{}, []tool.Tool{askOnlyTool{}}, nil)
		a.Permissions().SetRule("ask_tool", agent.PermissionAsk)
		m := model{
			agent:             a,
			ready:             true,
			width:             120,
			height:            40,
			showPermDialog:    true,
			pendingToolName:   "ask_tool",
			pendingPermission: agent.PermissionRequest{ToolName: "ask_tool", Scope: agent.PermissionScopeTool, Rule: "ask_tool"},
			styles:            ApplyThemeColors("tokyonight"),
			input:             newTestTextarea(),
			permViewport:      viewport.New(viewport.WithWidth(1), viewport.WithHeight(1)),
		}
		m.layout()
		return m
	}

	// Step 1: pressing "t" enters confirmation, persists nothing.
	m := newModel()
	cmd, closed := m.permDialogInput("t")
	if closed {
		t.Fatal(`pressing "t" should not close the dialog`)
	}
	if cmd != nil {
		t.Fatal(`pressing "t" should not run a command yet`)
	}
	if m.permConfirm != "t" {
		t.Fatalf("permConfirm = %q, want t", m.permConfirm)
	}
	if !m.showPermDialog {
		t.Fatal("dialog should remain open in confirmation step")
	}
	if got := m.agent.Permissions().Check("ask_tool"); got != agent.PermissionAsk {
		t.Fatalf("nothing should persist before confirm; Check = %q, want ask", got)
	}

	// The confirmation body must describe the tool-level rule that will persist.
	body := renderPermConfirmBody(m.pendingPermission, m.pendingToolName, m.permConfirm)
	if !strings.Contains(body, "ask_tool") {
		t.Fatalf("confirm body should name the tool, got %q", body)
	}

	// Backing out with "n" returns to step 1, still nothing persisted.
	if _, closed := m.permDialogInput("n"); closed {
		t.Fatal(`pressing "n" in confirm step should not close the dialog`)
	}
	if m.permConfirm != "" {
		t.Fatalf("permConfirm should clear after back, got %q", m.permConfirm)
	}
	if got := m.agent.Permissions().Check("ask_tool"); got != agent.PermissionAsk {
		t.Fatalf("backing out must not persist; Check = %q, want ask", got)
	}

	// Now confirm: "t" then "y" persists the tool rule and closes the dialog.
	m = newModel()
	m.permDialogInput("t")
	_, closed = m.permDialogInput("y")
	if !closed {
		t.Fatal("confirming should close the dialog")
	}
	if m.permConfirm != "" {
		t.Fatalf("permConfirm should clear after confirm, got %q", m.permConfirm)
	}
	if got := m.agent.Permissions().Check("ask_tool"); got != agent.PermissionAllow {
		t.Fatalf("confirm should persist allow; Check = %q, want allow", got)
	}
}

func TestRenderAgentStripShowsRunModelLabel(t *testing.T) {
	mainAgent := agent.NewAgent(retryTestClient{}, nil, nil)
	run := mainAgent.Runs().New("explore")
	run.Sub = agent.NewAgent(retryTestClient{}, nil, nil)

	m := model{
		agent:  mainAgent,
		ready:  true,
		width:  140,
		styles: ApplyThemeColors("tokyonight"),
		input:  newTestTextarea(),
	}
	m.layout()

	strip, _ := m.renderAgentStrip()
	if !strings.Contains(strip, "[test/test-model]") {
		t.Fatalf("expected agent strip to include run model label, got %q", strip)
	}
}

func TestUpdatePermButtonRegionsUsesRenderedBodyHeight(t *testing.T) {
	outOfScopePath := filepath.Join(t.TempDir(), "outside.txt")
	tests := []struct {
		name string
		req  agent.PermissionRequest
	}{
		{
			name: "short body",
			req: agent.PermissionRequest{
				ToolName: "read",
				Args:     json.RawMessage(`{"path":"notes.txt"}`),
			},
		},
		{
			name: "wrapped body",
			req: agent.PermissionRequest{
				ToolName: "read",
				Args:     json.RawMessage(`{"path":"` + outOfScopePath + `"}`),
				Scope:    agent.PermissionScopeTool,
				Rule:     "tool.read.out_of_scope",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				ready:             true,
				width:             120,
				height:            40,
				showPermDialog:    true,
				pendingPermission: tt.req,
				styles:            ApplyThemeColors("tokyonight"),
				input:             newTestTextarea(),
			}

			m.updatePermButtonRegions()
			if len(m.permButtonRegions) != len(permBtnDefs) {
				t.Fatalf("expected %d permission button regions, got %d", len(permBtnDefs), len(m.permButtonRegions))
			}

			contentWidth := max(0, m.panelWidth()-4)
			body := renderPermissionRequestBody(tt.req)
			visibleBodyLines := permissionDialogVisibleBodyLines(body, contentWidth)
			wantY := m.inputAreaTopY() + 4 + visibleBodyLines
			if got := m.permButtonRegions[0].y1; got != wantY {
				t.Fatalf("expected permission buttons to start at y=%d, got %d", wantY, got)
			}
			buttonHeight := lipgloss.Height(permBtnStyle.Render(permBtnDefs[0].label + " " + permBtnDefs[0].desc))
			if got, want := m.permButtonRegions[0].y2, wantY+buttonHeight-1; got != want {
				t.Fatalf("expected permission button height %d at y=%d, got y2=%d", buttonHeight, wantY, got)
			}
		})
	}
}

func TestNestedSubagentPermissionPromptSurfacesToMainTUI(t *testing.T) {
	client := &nestedTaskClient{responses: []*agent.Message{
		{Role: "assistant", ToolCalls: []agent.ToolCall{makeAgentToolCall("call-parent-task", "task", `{"prompt":"spawn nested"}`)}},
		{Role: "assistant", ToolCalls: []agent.ToolCall{makeAgentToolCall("call-child-task", "task", `{"prompt":"use ask tool"}`)}},
		{Role: "assistant", ToolCalls: []agent.ToolCall{makeAgentToolCall("call-ask", "ask_tool", `{}`)}},
		{Role: "assistant", Content: "nested complete"},
		{Role: "assistant", Content: "child complete"},
		{Role: "assistant", Content: "parent complete"},
	}}
	a := agent.NewAgent(client, []tool.Tool{askOnlyTool{}}, nil)
	a.Permissions().SetRule("task", agent.PermissionAllow)
	a.Permissions().SetRule("ask_tool", agent.PermissionAsk)

	m := model{
		agent:          a,
		input:          newTestTextarea(),
		viewport:       viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:         ApplyThemeColors("tokyonight"),
		subAgentPermCh: make(chan subAgentPermRequest),
		subAgentPermMu: &sync.Mutex{},
		messages: []message{{
			role: roleUser,
			text: "start",
			raw:  &agent.Message{Role: "user", Content: "start"},
		}},
	}
	m.layout()
	m.wireCompactCallbacks()

	stepDone := make(chan []agent.Message, 1)
	stepErr := make(chan error, 1)
	go func() {
		msgs, err := a.Step([]agent.Message{{Role: "user", Content: "start"}})
		if err != nil {
			stepErr <- err
			return
		}
		stepDone <- msgs
	}()

	listenCmd := listenSubAgentPerm(m.subAgentPermCh)
	if listenCmd == nil {
		t.Fatal("expected permission listener command")
	}
	msg := listenCmd()
	permMsg, ok := msg.(subAgentPermAskMsg)
	if !ok {
		t.Fatalf("expected subAgentPermAskMsg, got %T", msg)
	}

	updated, _ := m.Update(permMsg)
	got := derefTestModel(t, updated)
	if !got.showPermDialog {
		t.Fatal("expected permission dialog to be shown")
	}
	if got.pendingSubAgentResp == nil {
		t.Fatal("expected pending sub-agent response channel")
	}
	if got.pendingToolName != "ask_tool" {
		t.Fatalf("pending tool = %q, want ask_tool", got.pendingToolName)
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "sub-agent") {
		t.Fatalf("expected transcript to mention sub-agent permission prompt, got %#v", got.messages)
	}

	cmd := got.handlePermissionChoice("y")
	if cmd == nil {
		t.Fatal("expected re-arm command after sub-agent permission choice")
	}
	if got.pendingSubAgentResp != nil {
		t.Fatal("expected pending sub-agent response to clear after approval")
	}

	select {
	case err := <-stepErr:
		t.Fatalf("Step err: %v", err)
	case msgs := <-stepDone:
		joined := ""
		for _, am := range msgs {
			joined += am.Content + "\n"
		}
		if !strings.Contains(joined, "parent complete") {
			t.Fatalf("expected parent completion after permission approval, got %q", joined)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for nested subagent step to finish")
	}

	if got.agent.Permissions().Check("ask_tool") != agent.PermissionAsk {
		t.Fatalf("ask_tool permission = %q, want ask", got.agent.Permissions().Check("ask_tool"))
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "Allowed sub-agent \"ask_tool\" once.") {
		t.Fatalf("expected approval acknowledgement, got %#v", got.messages)
	}
}

func TestRenderPermissionPromptIsConciseAndNotJSON(t *testing.T) {
	req := agent.PermissionRequest{
		ToolName: "read",
		Args:     json.RawMessage(`{"path":"internal/tui/model.go","start_line":1,"end_line":5}`),
		Scope:    agent.PermissionScopeTool,
		Rule:     "tool.read.out_of_scope",
	}

	got := renderPermissionPrompt(req)

	if strings.Contains(got, `{"path"`) {
		t.Fatalf("expected permission prompt to avoid raw JSON, got %q", got)
	}
	if !strings.Contains(got, "📖 read internal/tui/model.go") {
		t.Fatalf("expected concise tool summary, got %q", got)
	}
	if !strings.Contains(got, "[y] once  [n] deny  [a] always this rule  [t] always this tool") {
		t.Fatalf("expected concise permission choices, got %q", got)
	}
}

func TestRenderPermissionRequestBodyIncludesBashPrefixScope(t *testing.T) {
	req := agent.PermissionRequest{
		ToolName: "bash",
		Args:     json.RawMessage(`{"command":"git push origin main"}`),
		Command:  "git push origin main",
		Prefix:   "git",
		Scope:    agent.PermissionScopeBashPrefix,
		Rule:     "bash.prefix.git",
	}

	got := renderPermissionRequestBody(req)

	if !strings.Contains(got, "$ git push origin main") {
		t.Fatalf("expected bash command summary, got %q", got)
	}
	if !strings.Contains(got, "Always-rule scope: bash prefix \"git\" (all `git ...` commands)") {
		t.Fatalf("expected bash prefix scope summary, got %q", got)
	}
}

func TestRenderPermissionRequestBodyClarifiesOutOfScopePathBehavior(t *testing.T) {
	outsideRoot := t.TempDir()
	target := filepath.Join(outsideRoot, "file.txt")
	req := agent.PermissionRequest{
		ToolName: "read",
		Args:     json.RawMessage(`{"path":"` + target + `"}`),
		Scope:    agent.PermissionScopeTool,
		Rule:     "tool.read.out_of_scope",
	}

	got := renderPermissionRequestBody(req)
	if !strings.Contains(got, "Path scope: target is outside the workspace") {
		t.Fatalf("expected out-of-scope hint, got %q", got)
	}
	if !strings.Contains(got, "[y] once = temporary path access for this one call") {
		t.Fatalf("expected temporary once hint, got %q", got)
	}
	if !strings.Contains(got, "[a] always this rule = also persists this path root") {
		t.Fatalf("expected persist hint for always rule, got %q", got)
	}
	if !strings.Contains(got, "[t] always this tool = remembers tool permission; path root is not persisted") {
		t.Fatalf("expected tool-scope hint, got %q", got)
	}
}

func TestExecuteApprovedTool_UsesTemporaryOutOfScopePathAllowance(t *testing.T) {
	workspace := t.TempDir()
	outsideRoot := t.TempDir()
	target := filepath.Join(outsideRoot, "allowed.txt")
	if err := os.WriteFile(target, []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	_, _ = tool.LoadBuiltins(nil)
	if tool.HasExtraAllowedPath(outsideRoot) {
		t.Fatalf("did not expect %q to be pre-allowed", outsideRoot)
	}

	a := agent.NewAgent(nil, []tool.Tool{&tool.ReadTool{}}, nil)
	m := model{agent: a, pendingToolCallID: "tc-1"}
	args := json.RawMessage(`{"path":"` + target + `"}`)

	cmd := m.executeApprovedTool("read", args, outsideRoot)
	msg := cmd()
	out, ok := msg.([]agent.Message)
	if !ok || len(out) != 1 {
		t.Fatalf("expected one tool message, got %#v", msg)
	}
	if !strings.Contains(out[0].Content, "ok") {
		t.Fatalf("expected read output to include file contents, got %q", out[0].Content)
	}
	if tool.HasExtraAllowedPath(outsideRoot) {
		t.Fatalf("expected temporary path allowance for %q to be removed", outsideRoot)
	}
}

func TestDoubleEscDisablesShellMode(t *testing.T) {
	m := model{
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-shell",
	}
	m.input.SetValue("!echo hello")
	m.escPressed = true
	m.escPressTime = time.Now()

	updated, cmd := m.handleEscKey()
	if cmd != nil {
		t.Fatalf("expected no command, got %T", cmd)
	}
	got := updated.(model)
	if got.input.Value() != "echo hello" {
		t.Fatalf("expected shell prefix to be removed, got %q", got.input.Value())
	}
	if got.showPalette {
		t.Fatal("expected double-esc in shell mode to not open the message picker")
	}
	if got.escPressed {
		t.Fatal("expected esc state to clear after disabling shell mode")
	}
}

func TestRenderAssistantTextThinkingToggle(t *testing.T) {
	m := model{styles: ApplyThemeColors("tokyonight"), showThinking: true}
	shown := m.renderAssistantText("before <think>hidden</think> after")
	if !strings.Contains(shown, "before ") || !strings.Contains(shown, "hidden") || !strings.Contains(shown, " after") {
		t.Fatalf("expected visible thinking and normal text, got %q", shown)
	}
	if strings.Contains(shown, "<think>") || strings.Contains(shown, "</think>") {
		t.Fatalf("expected thinking tags to be removed, got %q", shown)
	}

	m.showThinking = false
	hidden := m.renderAssistantText("before <think>hidden</think> after")
	if strings.Contains(hidden, "hidden") {
		t.Fatalf("expected thinking content hidden, got %q", hidden)
	}
	if !strings.Contains(hidden, "before ") || !strings.Contains(hidden, " after") {
		t.Fatalf("expected normal text preserved, got %q", hidden)
	}
}

func TestThinkingStreamStartsCollapsed(t *testing.T) {
	m := model{
		viewport:     viewport.New(viewport.WithWidth(40), viewport.WithHeight(6)),
		styles:       ApplyThemeColors("tokyonight"),
		showThinking: true,
		streaming:    true,
	}

	m.applyThinkingDelta("reasoning", strings.Repeat("line\n", 12))

	if m.streamingThinkingIdx < 0 {
		t.Fatal("expected thinking message to be created")
	}
	if m.expandedThinking[m.streamingThinkingIdx] {
		t.Fatal("expected streaming thinking to stay collapsed by default")
	}
	plain := stripANSI(m.transcriptContent)
	if !strings.Contains(plain, "click to expand") {
		t.Fatalf("expected collapsed thinking affordance, got %q", plain)
	}
	// Collapsed shows last 8 lines of 12. The full 12 lines should NOT appear.
	if strings.Contains(plain, "line\nline\nline\nline\nline\nline\nline\nline\nline\n") {
		t.Fatalf("expected streaming thinking to show ≤8 lines when collapsed, got %q", plain)
	}
}

func TestLateThinkingDeltaAfterAssistantMessageIsIgnored(t *testing.T) {
	m := model{
		viewport:             viewport.New(viewport.WithWidth(60), viewport.WithHeight(10)),
		styles:               ApplyThemeColors("tokyonight"),
		showThinking:         true,
		streaming:            true,
		streamingThinkingIdx: -1,
	}

	m.applyThinkingDelta("reasoning", "draft reasoning")
	if len(m.messages) != 1 || m.messages[0].role != roleThinking {
		t.Fatalf("expected initial streamed thinking message, got %#v", m.messages)
	}

	m.appendAgentMessage(agent.Message{Role: "assistant", ReasoningContent: "final reasoning", Content: "done"})
	if got := len(m.messages); got != 2 {
		t.Fatalf("expected thinking + assistant after final message, got %d messages", got)
	}

	m.applyThinkingDelta("reasoning", " late tail")
	if got := len(m.messages); got != 2 {
		t.Fatalf("expected late delta to be ignored, got %d messages", got)
	}
	if got := m.messages[0].text; got != "final reasoning" {
		t.Fatalf("expected canonical reasoning to remain unchanged, got %q", got)
	}
	if m.messages[len(m.messages)-1].role != roleAssistant {
		t.Fatalf("expected assistant message to remain last, got %#v", m.messages[len(m.messages)-1])
	}
}

func TestThinkingDeltaStreamsWithPriorAssistantHistory(t *testing.T) {
	m := model{
		viewport:             viewport.New(viewport.WithWidth(60), viewport.WithHeight(10)),
		styles:               ApplyThemeColors("tokyonight"),
		showThinking:         true,
		streaming:            true,
		streamingThinkingIdx: -1,
		messages: []message{
			{role: roleAssistant, text: "previous assistant turn"},
			{role: roleUser, text: "new user turn"},
		},
	}

	m.applyThinkingDelta("reasoning", "live reasoning")

	if got := len(m.messages); got != 3 {
		t.Fatalf("expected reasoning message to be appended, got %d messages", got)
	}
	if m.messages[2].role != roleThinking {
		t.Fatalf("expected appended roleThinking message, got role %v", m.messages[2].role)
	}
	if got := m.messages[2].text; got != "live reasoning" {
		t.Fatalf("expected streamed reasoning text, got %q", got)
	}
}

func TestThinkingDeltaIgnoredWhenNotStreaming(t *testing.T) {
	m := model{
		viewport:             viewport.New(viewport.WithWidth(60), viewport.WithHeight(10)),
		styles:               ApplyThemeColors("tokyonight"),
		showThinking:         true,
		streaming:            false,
		streamingThinkingIdx: -1,
		messages: []message{
			{role: roleAssistant, text: "previous assistant turn"},
			{role: roleUser, text: "new user turn"},
		},
	}

	m.applyThinkingDelta("reasoning", "late reasoning")

	if got := len(m.messages); got != 2 {
		t.Fatalf("expected no new message when not streaming, got %d messages", got)
	}
}

func TestThinkingDeltaContinuesAfterAssistantToolCallMessage(t *testing.T) {
	m := model{
		viewport:             viewport.New(viewport.WithWidth(60), viewport.WithHeight(10)),
		styles:               ApplyThemeColors("tokyonight"),
		showThinking:         true,
		streaming:            true,
		streamingThinkingIdx: -1,
	}

	m.appendAgentMessage(agent.Message{
		Role: "assistant",
		ToolCalls: []agent.ToolCall{
			makeAgentToolCall("call-1", "bash", `{"command":"echo hi"}`),
		},
	})

	m.applyThinkingDelta("reasoning", "post-toolcall reasoning")

	if got := len(m.messages); got != 2 {
		t.Fatalf("expected assistant tool-call message + streamed thinking, got %d messages", got)
	}
	if m.messages[1].role != roleThinking {
		t.Fatalf("expected second message to be roleThinking, got %v", m.messages[1].role)
	}
	if got := m.messages[1].text; got != "post-toolcall reasoning" {
		t.Fatalf("expected streamed reasoning after tool-call assistant, got %q", got)
	}
}

func TestRenderUserTextUsesThemeBox(t *testing.T) {
	m := model{styles: ApplyThemeColors("tokyonight")}
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	rendered := m.renderUserText("hello world")
	plain := stripANSI(rendered)
	if !strings.Contains(plain, "hello world") {
		t.Fatalf("expected rendered user text to contain content, got %q", rendered)
	}
	if !strings.Contains(plain, "┃") {
		t.Fatalf("expected rendered user text to include accent rail, got %q", rendered)
	}
	if !strings.HasPrefix(strings.Split(plain, "\n")[0], "┃ ") {
		t.Fatalf("expected rendered user text to be indented, got %q", plain)
	}
}

func TestRenderUserTextConstrainsBubbleWidth(t *testing.T) {
	m := model{styles: ApplyThemeColors("tokyonight")}
	m.viewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))
	rendered := stripANSI(m.renderUserText(strings.Repeat("word ", 20)))
	for _, line := range strings.Split(rendered, "\n") {
		if got := lipgloss.Width(line); got > 40 {
			t.Fatalf("expected user bubble line width <= viewport width, got %d: %q", got, line)
		}
	}
}

func TestLeaderSTogglesSidebar(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)), leaderActive: true}

	consumed, updated, _ := m.handleModalKeys(tea.KeyPressMsg{Code: 's'})
	if !consumed {
		t.Fatal("expected leader+s to be consumed")
	}
	got := updated.(model)
	if !got.showSidebar {
		t.Fatal("expected leader+s to toggle sidebar on")
	}

	got.leaderActive = true
	consumed, updated, _ = got.handleModalKeys(tea.KeyPressMsg{Code: 's'})
	if !consumed {
		t.Fatal("expected leader+s to be consumed on second toggle")
	}
	got = updated.(model)
	if got.showSidebar {
		t.Fatal("expected leader+s to toggle sidebar off")
	}
}

func TestCtrlBMovesForegroundBashToBackgroundBeforeTogglingSidebar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX bash command setup")
	}
	a := agent.NewAgent(nil, nil, nil)
	cmd := exec.Command("bash", "-c", "sleep 30")
	if _, err := a.Procs().RegisterForeground("sleep 30", cmd, time.Now(), nil); err != nil {
		t.Fatalf("RegisterForeground error: %v", err)
	}
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)), agent: a}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	got := updated.(model)
	if got.showSidebar {
		t.Fatal("expected Ctrl+B to background bash instead of toggling sidebar")
	}
	if len(got.messages) == 0 || !strings.Contains(stripANSI(got.messages[len(got.messages)-1].text), "moved bash to background") {
		t.Fatalf("expected backgrounding hint message, got %#v", got.messages)
	}
	if id, _, ok := a.Procs().RequestBackgroundLatest(); ok || id != "" {
		t.Fatal("expected foreground bash to already be promoted")
	}
}

func TestCtrlBWithoutRunningBashDoesNotToggleSidebar(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	got := updated.(model)
	if got.showSidebar {
		t.Fatal("expected Ctrl+B not to toggle sidebar when no bash is running")
	}
	if len(got.messages) == 0 || !strings.Contains(stripANSI(got.messages[len(got.messages)-1].text), "no running bash command") {
		t.Fatalf("expected no-running-bash hint, got %#v", got.messages)
	}
}

func TestCtrlOTogglesYoloMode(t *testing.T) {
	m := model{
		input:    textarea.New(),
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		agent:    agent.NewAgent(nil, nil, nil),
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("expected Ctrl+O to return no command, got %T", cmd)
	}
	got := updated.(*model)
	if got.agent.Permissions().Mode() != agent.PermissionModeYOLO {
		t.Fatalf("expected Ctrl+O to enable YOLO, got %s", got.agent.Permissions().Mode())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	got2 := updated.(*model)
	if got2.agent.Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("expected Ctrl+O to disable YOLO, got %s", got2.agent.Permissions().Mode())
	}
}

func TestRunOptionsYOLOSetsPermissionMode(t *testing.T) {
	// The wiring under test: newModel with YOLO: true should call
	// SetMode(PermissionModeYOLO) on the constructed agent's permissions.
	// We exercise that path on a manually-constructed model so this test
	// is independent of NewClient's model/credential resolution.
	m := model{agent: agent.NewAgent(retryTestClient{}, nil, nil)}
	if m.agent.Permissions() == nil {
		t.Fatal("expected constructed agent to have permissions")
	}
	// Replicate the newModel wire: if YOLO and pm != nil, set YOLO.
	pm := m.agent.Permissions()
	if pm != nil {
		pm.SetMode(agent.PermissionModeYOLO)
	}
	if got := pm.Mode(); got != agent.PermissionModeYOLO {
		t.Fatalf("expected YOLO mode, got %s", got)
	}
}

func TestRunOptionsPermissionModeOffDisablesAutoPermission(t *testing.T) {
	// Seed config so the agent gets constructed and the Auto block is
	// wired through. We isolate HOME to a temp dir so SaveOcodeConfig
	// does not pollute the user's real config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("APPDATA", home)

	// Pre-seed a config with auto permission on. The newModel call with
	// PermissionMode: "off" must flip auto to off and persist the change.
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(cfgDir, "ocodeconfig.json"),
		[]byte(`{"permissions":{"auto":{"enabled":true}}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_ = newModel(RunOptions{PermissionMode: "off"})

	// Verify the change was persisted to disk; the in-memory agent wire is
	// tested separately via PermissionManager.SetAutoPermissionEnabled.
	data, err := os.ReadFile(filepath.Join(cfgDir, "ocodeconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	perms, ok := parsed["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions block in persisted config, got %v", parsed)
	}
	auto, ok := perms["auto"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions.auto block in persisted config, got %v", perms)
	}
	if enabled, _ := auto["enabled"].(bool); enabled {
		t.Fatal("expected persisted permissions.auto.enabled = false")
	}
}

func TestRunOptionsPermissionModeAutoEnablesAutoPermission(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("APPDATA", home)

	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(cfgDir, "ocodeconfig.json"),
		[]byte(`{"permissions":{"auto":{"enabled":false}}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_ = newModel(RunOptions{PermissionMode: "auto"})

	data, err := os.ReadFile(filepath.Join(cfgDir, "ocodeconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	perms, ok := parsed["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions block in persisted config, got %v", parsed)
	}
	auto, ok := perms["auto"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions.auto block in persisted config, got %v", perms)
	}
	if enabled, _ := auto["enabled"].(bool); !enabled {
		t.Fatal("expected persisted permissions.auto.enabled = true")
	}
}

func TestRunOptionsPermissionModeInvalidDoesNotMutateConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("APPDATA", home)

	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(cfgDir, "ocodeconfig.json"),
		[]byte(`{"permissions":{"auto":{"enabled":true}}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// main.go rejects unknown values before reaching newModel, but
	// defense in depth: an unknown value reaching newModel must not
	// flip the persisted permissions.auto.enabled bit. Other fields
	// (e.g. small_model) may be re-saved as part of normal startup.
	_ = newModel(RunOptions{PermissionMode: "bogus"})

	data, err := os.ReadFile(filepath.Join(cfgDir, "ocodeconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	perms, ok := parsed["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions block in persisted config, got %v", parsed)
	}
	auto, ok := perms["auto"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions.auto block in persisted config, got %v", perms)
	}
	if enabled, _ := auto["enabled"].(bool); !enabled {
		t.Fatal("expected permissions.auto.enabled to remain true for invalid PermissionMode")
	}
}

func TestPersistPermissionsPreservesAutoBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("APPDATA", home)

	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{"permissions":{"auto":{"enabled":false,"model":"anthropic/claude-sonnet-4-6","allow_destructive":true,"prompt":"keep me","max_context_bytes":123,"max_context_sources":4,"max_context_lines_per_source":9}}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "ocodeconfig.json"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	if err := config.LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}

	m := model{config: &cfg, agent: agent.NewAgent(nil, nil, &cfg)}
	m.agent.Permissions().SetAutoPermissionEnabled(true)
	m.agent.Permissions().SetRule("ask_tool", agent.PermissionAllow)
	m.persistPermissions()

	data, err := os.ReadFile(filepath.Join(cfgDir, "ocodeconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	perms, ok := parsed["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions block in persisted config, got %v", parsed)
	}
	auto, ok := perms["auto"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions.auto block in persisted config, got %v", perms)
	}
	if enabled, _ := auto["enabled"].(bool); !enabled {
		t.Fatal("expected persisted permissions.auto.enabled = true")
	}
	if got := auto["model"]; got != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("expected auto model to be preserved, got %v", got)
	}
	if got, _ := auto["allow_destructive"].(bool); !got {
		t.Fatal("expected auto.allow_destructive to be preserved")
	}
	if got := auto["prompt"]; got != "keep me" {
		t.Fatalf("expected auto prompt to be preserved, got %v", got)
	}
}

func TestMCPCmdListsConfiguredServers(t *testing.T) {
	m := model{
		config: &config.Config{MCP: map[string]config.MCPConfig{
			"demo": {Type: "local", Enabled: true},
		}},
		agent: agent.NewAgent(nil, nil, nil),
	}
	m.agent.RestoreMCPToolNames([]string{"demo_search"})

	runMCPCmd(&m, nil)

	if len(m.messages) != 1 {
		t.Fatalf("expected one message, got %d", len(m.messages))
	}
	text := m.messages[0].text
	for _, want := range []string{"MCP servers:", "demo", "enabled", "1 tools"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in MCP output, got %q", want, text)
		}
	}
}

func TestCtrlCClearsNonEmptyInputBeforeQuitConfirmation(t *testing.T) {
	m := model{
		input:             textarea.New(),
		viewport:          viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		inputHistoryIndex: 2,
		ctrlCPressed:      true,
		showSlashPopup:    true,
	}
	m.input.SetValue("draft message")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("expected Ctrl+C with input to return no command, got %T", cmd)
	}
	got := updated.(model)
	if got.input.Value() != "" {
		t.Fatalf("expected Ctrl+C to clear input, got %q", got.input.Value())
	}
	if got.ctrlCPressed {
		t.Fatal("expected Ctrl+C clearing input to reset quit confirmation")
	}
	if got.inputHistoryIndex != -1 {
		t.Fatalf("expected input history index reset, got %d", got.inputHistoryIndex)
	}
	if got.showSlashPopup {
		t.Fatal("expected slash popup to close when input is cleared")
	}
}

func TestRunExitCmdUsesSharedCleanupPath(t *testing.T) {
	cleanupCalls := 0
	m := model{
		cleanupState: &modelCleanupState{
			onCleanup: func() { cleanupCalls++ },
		},
	}

	cmd := runExitCmd(&m, nil)
	if cmd == nil {
		t.Fatal("expected /exit to return quit command")
	}
	if cleanupCalls != 1 {
		t.Fatalf("expected /exit to use shared cleanup once, got %d", cleanupCalls)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected /exit to quit, got %T", cmd())
	}
}

func TestCleanupCurrentSessionDeduplicatesRepeatedCalls(t *testing.T) {
	cleanupCalls := 0
	shutdownCalls := 0
	a := agent.NewAgent(nil, nil, nil)
	m := model{
		agent: a,
		cleanupState: &modelCleanupState{
			shutdown:  make(map[*agent.Agent]struct{}),
			onCleanup: func() { cleanupCalls++ },
			shutdownAgent: func(target *agent.Agent) {
				shutdownCalls++
				if target != a {
					t.Fatalf("expected cleanup to target original agent")
				}
			},
		},
	}

	m.cleanupCurrentSession()
	m.cleanupCurrentSession()

	if cleanupCalls != 1 {
		t.Fatalf("expected repeated cleanup to run hook once, got %d", cleanupCalls)
	}
	if shutdownCalls != 1 {
		t.Fatalf("expected repeated cleanup to shut down agent once, got %d", shutdownCalls)
	}
}

func TestCleanupCurrentSessionDeduplicatesNilAgent(t *testing.T) {
	cleanupCalls := 0
	m := model{
		cleanupState: &modelCleanupState{
			shutdown:  make(map[*agent.Agent]struct{}),
			onCleanup: func() { cleanupCalls++ },
		},
	}

	m.cleanupCurrentSession()
	m.cleanupCurrentSession()

	if cleanupCalls != 1 {
		t.Fatalf("expected repeated nil-agent cleanup to run hook once, got %d", cleanupCalls)
	}
}

func TestCtrlCTwiceUsesSharedCleanupPath(t *testing.T) {
	cleanupCalls := 0
	m := model{
		input: newTestTextarea(),
		cleanupState: &modelCleanupState{
			onCleanup: func() { cleanupCalls++ },
		},
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected first Ctrl+C to arm quit confirmation")
	}
	if cleanupCalls != 0 {
		t.Fatalf("expected first Ctrl+C to skip cleanup, got %d calls", cleanupCalls)
	}

	updated, cmd = updated.(model).Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected second Ctrl+C to return quit command")
	}
	if cleanupCalls != 1 {
		t.Fatalf("expected second Ctrl+C to use shared cleanup once, got %d", cleanupCalls)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected second Ctrl+C to quit, got %T", cmd())
	}
}

func TestLeaderQuitUsesSharedCleanupPath(t *testing.T) {
	cleanupCalls := 0
	m := model{
		input:        newTestTextarea(),
		leaderActive: true,
		cleanupState: &modelCleanupState{
			onCleanup: func() { cleanupCalls++ },
		},
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Text: "q"})
	if cmd == nil {
		t.Fatal("expected leader q to return quit command")
	}
	if cleanupCalls != 1 {
		t.Fatalf("expected leader q to use shared cleanup once, got %d", cleanupCalls)
	}
	got := updated.(model)
	if got.leaderActive {
		t.Fatal("expected leader mode to clear after quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected leader q to quit, got %T", cmd())
	}
}

func TestSidebarViewUsesSplitLayoutWhenWide(t *testing.T) {
	snapshot.Reset()
	defer snapshot.Reset()

	spend := 0.1234
	m := model{
		ready:       true,
		width:       140,
		height:      40,
		showSidebar: true,
		sessionID:   "session-123",
		input:       textarea.New(),
		viewport:    viewport.New(viewport.WithWidth(100), viewport.WithHeight(20)),
		config:      &config.Config{Model: "gpt-4o"},
		messages: []message{{
			role: roleAssistant,
			text: "hello",
			raw: &agent.Message{
				Role:  "assistant",
				Usage: &agent.TokenUsage{PromptTokens: int64Ptr(1000), CompletionTokens: int64Ptr(2000)},
				Spend: &spend,
			},
		}},
	}

	view := m.View().Content
	for _, want := range []string{"gpt-4o", "$0.1234", "Tools", "TODO", "Git", "Files", "Ctrl+B", "run", "lint", "build"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected wide view to include %q, got %q", want, view)
		}
	}
}

func TestBuildSidebarRenderDataShowsCacheAndSpendInline(t *testing.T) {
	snapshot.Reset()
	defer snapshot.Reset()
	tool.ResetTodoState()

	spend := 0.1234
	m := model{
		config: &config.Config{Model: "gpt-4o"},
		sessionTelemetry: sidebarTelemetry{
			inputTokens:  1000,
			outputTokens: 2000,
			totalTokens:  3000,
			cachedTokens: 300,
			spend:        &spend,
		},
	}

	got := strings.Join(m.buildSidebarRenderData().bottomLines, "\n")
	for _, want := range []string{"In 1k  Cache 300  Out 2k", "$0.1234"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected sidebar usage block to include %q, got %q", want, got)
		}
	}
}

func TestSidebarMiddleSectionsCapAtFortyPercent(t *testing.T) {
	m := model{height: 100}
	data := sidebarRenderData{
		topLines:    make([]string, 10),
		bottomLines: make([]string, 2),
	}

	got := m.sidebarScrollBoxHeight(data, 1)
	want := 38 // (height 100 - outer border 2 - header 1) * 40%.
	if got != want {
		t.Fatalf("expected sidebar middle section height %d, got %d", want, got)
	}
}

func TestSidebarViewHidesOnNarrowTerminals(t *testing.T) {
	m := model{
		ready:       true,
		width:       80,
		height:      30,
		showSidebar: true,
		input:       textarea.New(),
		viewport:    viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
	}

	view := m.View().Content
	if strings.Contains(view, "No live session todo state yet.") || strings.Contains(view, "Ctrl+X then S sidebar") {
		t.Fatalf("expected narrow view to hide sidebar, got %q", view)
	}
}

func TestLayoutKeepsInputAndStatusWithinTerminalHeight(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		sessionID: strings.Repeat("session-", 12),
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line that should stay in the viewport\n", 80),
		}},
	}
	m.input.SetValue(strings.Repeat("draft input ", 12))

	m.layout()
	content := m.renderContent()

	if got := lipgloss.Height(content); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d", got, m.height)
	}
	if !strings.Contains(content, "draft input") {
		t.Fatalf("expected input to remain visible, got %q", content)
	}
	if !strings.Contains(content, "Agent:") {
		t.Fatalf("expected status to remain visible, got %q", content)
	}
}

func TestLayoutHeightDoesNotChangeWhenTranscriptScrolls(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		sessionID: strings.Repeat("session-", 12),
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line that should stay in the viewport\n", 80),
		}},
	}
	m.input.SetValue("draft input")
	m.layout()

	m.viewport.GotoTop()
	top := m.renderContent()
	m.viewport.GotoBottom()
	bottom := m.renderContent()

	if topHeight, bottomHeight := lipgloss.Height(top), lipgloss.Height(bottom); topHeight != bottomHeight {
		t.Fatalf("expected scroll position not to change layout height, top=%d bottom=%d", topHeight, bottomHeight)
	}
	if got := lipgloss.Height(bottom); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d", got, m.height)
	}
	for _, content := range []string{top, bottom} {
		if !strings.Contains(content, "draft input") || !strings.Contains(content, "Agent:") {
			t.Fatalf("expected input and status to remain visible, got %q", content)
		}
	}
}

func TestInputNavigationDoesNotScrollTranscript(t *testing.T) {
	m := model{
		input:    textarea.New(),
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(6)),
		styles:   ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line\n", 40),
		}},
	}
	m.input.SetValue("first line\nsecond line")
	m.renderTranscript()
	m.viewport.GotoBottom()
	offset := m.viewport.YOffset()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	got := updated.(model)
	if got.viewport.YOffset() != offset {
		t.Fatalf("expected input up key not to scroll transcript, before=%d after=%d", offset, got.viewport.YOffset())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	got = updated.(model)
	if got.viewport.YOffset() != offset {
		t.Fatalf("expected input down key not to scroll transcript, before=%d after=%d", offset, got.viewport.YOffset())
	}
}

func TestLayoutConstrainsLongTranscriptLines(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		sessionID: "session-1",
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: "PERMISSION_ASK:" + strings.Repeat(`{"very_long_argument":`, 20),
		}},
	}
	m.input.SetValue("draft input")
	m.layout()
	content := m.renderContent()

	if got := lipgloss.Height(content); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d", got, m.height)
	}
	for _, line := range strings.Split(content, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("rendered line width %d exceeds terminal width %d: %q", got, m.width, line)
		}
	}
	if !strings.Contains(content, "draft input") || !strings.Contains(content, "Agent:") {
		t.Fatalf("expected input and status to remain visible, got %q", content)
	}
}

func TestLayoutWrapsLongInputLines(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		sessionID: "session-1",
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: "ready",
		}},
	}
	m.input.Prompt = "▍ "
	m.input.SetValue(strings.Repeat("unbroken-input", 20))
	m.layout()
	content := m.renderContent()

	for _, line := range strings.Split(content, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("rendered line width %d exceeds terminal width %d: %q", got, m.width, line)
		}
	}
	if got := m.input.MaxWidth; got <= 0 {
		t.Fatalf("expected input max width to be constrained, got %d", got)
	}
}

func TestLayoutAccountsForSlashPopupAndActivityRow(t *testing.T) {
	m := model{
		ready:               true,
		width:               80,
		height:              24,
		sessionID:           strings.Repeat("session-", 12),
		input:               newTestTextarea(),
		viewport:            viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:              ApplyThemeColors("tokyonight"),
		showSlashPopup:      true,
		slashPopupItems:     []slashSuggestion{{name: "/compact", display: "/compact", desc: "Reduce context"}},
		activityRowReserved: true,
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line that should stay in the viewport\n", 80),
		}},
	}
	m.input.SetValue("/co")

	m.layout()
	content := m.renderContent()

	if got := lipgloss.Height(content); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d", got, m.height)
	}
	for _, want := range []string{"/compact", "/co", "Agent:"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q to remain visible, got %q", want, content)
		}
	}
}

func TestSidebarViewShowsChangedFilesAndTodoState(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	snapshot.Reset()
	defer snapshot.Reset()
	tool.SetTodoSession("session-1")
	tool.ResetTodoState()

	if err := os.WriteFile("changed.go", []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Backup("changed.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := (tool.TodoWriteTool{}).Execute(mustJSON(t, map[string]string{"todoText": "- [ ] ship task 4"})); err != nil {
		t.Fatal(err)
	}

	m := model{
		ready:         true,
		width:         140,
		height:        40,
		showSidebar:   true,
		sidebarScroll: 2,
		input:         textarea.New(),
		viewport:      viewport.New(viewport.WithWidth(100), viewport.WithHeight(20)),
	}

	view := stripANSI(m.View().Content)
	for _, want := range []string{"Files", "changed.go", "TODO", "[○] ship task 4"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected sidebar to include %q, got %q", want, view)
		}
	}
}

func TestFormatSidebarFilePathStripsProjectPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	absPath := filepath.Join(tmpDir, "internal", "tui", "model.go")

	if got := formatSidebarFilePath(absPath, tmpDir, 80); got != "internal/tui/model.go" {
		t.Fatalf("expected project-relative path, got %q", got)
	}
	if got := formatSidebarFilePath("./internal/tui/model.go", tmpDir, 80); got != "internal/tui/model.go" {
		t.Fatalf("expected ./ prefix stripped, got %q", got)
	}
}

func TestFormatSidebarFilePathTruncatesMiddlePreservingFilename(t *testing.T) {
	path := "very/long/path/to/important.go"
	got := formatSidebarFilePath(path, "", 24)

	if len(got) > 24 {
		t.Fatalf("expected truncated path to fit width, got %q len=%d", got, len(got))
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("expected middle truncation marker, got %q", got)
	}
	if !strings.HasSuffix(got, "important.go") {
		t.Fatalf("expected filename ending to be preserved, got %q", got)
	}

	longFile := "absurdly_long_generated_filename_with_suffix_test.go"
	got = formatSidebarFilePath("nested/"+longFile, "", 24)
	if len(got) > 24 {
		t.Fatalf("expected long filename to fit width, got %q len=%d", got, len(got))
	}
	if !strings.HasSuffix(got, "suffix_test.go") {
		t.Fatalf("expected long filename ending to be prioritized, got %q", got)
	}
}

func TestSidebarFileClickLaunchesEditor(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	snapshot.Reset()
	tool.SetTodoSession("session-1")
	tool.ResetTodoState()

	if err := os.WriteFile("changed.go", []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Backup("changed.go"); err != nil {
		t.Fatal(err)
	}

	prev := openSidebarFileInEditor
	defer func() { openSidebarFileInEditor = prev }()
	var gotPath string
	openSidebarFileInEditor = func(path string) tea.Cmd {
		gotPath = path
		return func() tea.Msg { return nil }
	}

	m := model{ready: true, width: 140, height: 40, showSidebar: true, input: textarea.New(), viewport: viewport.New(viewport.WithWidth(100), viewport.WithHeight(20))}
	// Press starts sidebar selection (no file opening yet)
	// Y accounts for: appHeader(2) + sidebar_border(1) + topLines(7) +
	// no-title-pad(1) + git_title(1) + git_branch(1) + blank(1) + files_title(1)
	// = 2 + 1 + 7 + 1 + 1 + 1 + 1 + 1 = 15
	updated, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 120, Y: 15})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("expected press to start selection only, got stray command")
	}
	// Release on same position triggers file open (simple click, no drag)
	updated, cmd = m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: 120, Y: 15})
	_ = updated
	if cmd == nil {
		t.Fatal("expected release on file line to return editor command")
	}
	cmd()

	if gotPath != "changed.go" {
		t.Fatalf("expected clicked file to open, got %q", gotPath)
	}
}

// TestSidebarHoverAndSelectUseScreenY is a regression test for the chat sidebar
// mouse Y math. The sidebar is rendered below the app header (appHeaderHeight
// rows) and inside a bordered box, but the original hit-test helpers used a
// box-relative Y while mouse.Y is a screen-Y. That made the hover underline
// appear 5-6 rows above the actual file the user was mousing over, and made
// drag-selections start on the wrong row.
//
// This test pins down the correct screen-Y for the first file row by reusing
// the model's own buildSidebarRenderData to derive the offset, and asserts:
//  1. Hover at the on-screen file Y sets hoverSidebarFile to the file path.
//  2. A press at the on-screen file Y places sidebarSel.startLine on the
//     raw selectable buffer entry that contains that file path.
//  3. The same motion at the pre-fix (box-relative) Y does NOT trigger a
//     file hover — i.e. the offset is required.
func TestSidebarHoverAndSelectUseScreenY(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	snapshot.Reset()
	tool.SetTodoSession("session-1")
	tool.ResetTodoState()
	if err := os.WriteFile("changed.go", []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Backup("changed.go"); err != nil {
		t.Fatal(err)
	}

	m := model{ready: true, width: 140, height: 40, showSidebar: true, input: textarea.New(), viewport: viewport.New(viewport.WithWidth(100), viewport.WithHeight(20))}

	// Derive the on-screen Y of the "changed.go" row from buildSidebarRenderData
	// + the helpers the click path uses, so this test doesn't have to hard-code
	// topLines counts (those change as the sidebar evolves).
	data := m.buildSidebarRenderData()
	scrollIdx := -1
	for i := range data.scrollLines {
		if data.fileScrollLinePaths[i] == "changed.go" {
			scrollIdx = i
			break
		}
	}
	if scrollIdx < 0 {
		t.Fatal("could not find changed.go in sidebar scroll data")
	}
	visible := m.sidebarVisibleScrollLines(data, m.sidebarHeaderHeight())
	if scrollIdx >= visible {
		t.Fatalf("file row scrollIdx=%d not in first visible scroll window (visible=%d); raise test height",
			scrollIdx, visible)
	}
	raw, contentTopY := m.sidebarSelectableLines()
	// raw[0] is the first topLine, the file line sits at len(data.topLines) +
	// scrollIdx in the composed buffer.
	fileRowInBuffer := len(data.topLines) + scrollIdx
	if fileRowInBuffer >= len(raw) {
		t.Fatalf("computed file row %d past end of raw buffer (%d)", fileRowInBuffer, len(raw))
	}
	if !strings.Contains(raw[fileRowInBuffer], "changed.go") {
		t.Fatalf("raw[%d] = %q, expected it to contain changed.go", fileRowInBuffer, raw[fileRowInBuffer])
	}
	// Screen-Y of the on-screen file row = contentTopY + len(topLines) + scrollIdx.
	fileScreenY := contentTopY + len(data.topLines) + scrollIdx

	// (1) Hover at the on-screen file row sets hoverSidebarFile.
	updated, _ := m.Update(tea.MouseMotionMsg{X: 120, Y: fileScreenY})
	hovered := updated.(model).hoverSidebarFile
	if hovered != "changed.go" {
		t.Fatalf("expected hover at on-screen Y=%d to set hoverSidebarFile=changed.go, got %q",
			fileScreenY, hovered)
	}

	// (2) Press at the on-screen file row sets sidebarSel.startLine to the
	// line in the raw selectable buffer that contains the file.
	updated, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 120, Y: fileScreenY})
	sel := updated.(model).sidebarSel
	if sel.startLine != fileRowInBuffer {
		t.Fatalf("expected press at Y=%d to start selection on raw line %d, got %d",
			fileScreenY, fileRowInBuffer, sel.startLine)
	}

	// (3) Sanity: at the pre-fix (box-relative) Y, no file hover registers.
	// The buggy code used contentTopY = 1 + sidebarHeaderHeight() (no
	// appHeaderHeight), so the pre-fix fileScreenY was contentTopY-2.
	preFixY := fileScreenY - appHeaderHeight
	if preFixY > 0 {
		updated, _ := m.Update(tea.MouseMotionMsg{X: 120, Y: preFixY})
		hoveredAtPreFix := updated.(model).hoverSidebarFile
		if hoveredAtPreFix == "changed.go" {
			t.Fatalf("regression: hover at pre-fix Y=%d set hoverSidebarFile=changed.go "+
				"(should be empty; the fix must add appHeaderHeight=%d)",
				preFixY, appHeaderHeight)
		}
	}
}

// TestSidebarHoverAtBottomVisibleScrollRow uses the actual rendered sidebar row
// to guard against hit-test helpers that trim the visible scroll window too
// aggressively. The target file is placed on the last visible scroll row, which
// is the spot that regressed when the hit-test math was two rows short.
func TestSidebarHoverAtBottomVisibleScrollRow(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	snapshot.Reset()
	tool.SetTodoSession("session-2")
	tool.ResetTodoState()

	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("file-%02d.go", i)
		if err := os.WriteFile(name, []byte("package main\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := snapshot.Backup(name); err != nil {
			t.Fatal(err)
		}
	}

	m := model{ready: true, width: 140, height: 20, showSidebar: true, input: textarea.New(), viewport: viewport.New(viewport.WithWidth(100), viewport.WithHeight(20))}
	data := m.buildSidebarRenderData()
	targetPath := "file-11.go"
	scrollIdx := -1
	for i, path := range data.fileScrollLinePaths {
		if path == targetPath {
			scrollIdx = i
			break
		}
	}
	if scrollIdx < 0 {
		t.Fatalf("could not find %s in sidebar scroll data", targetPath)
	}

	headerHeight := m.sidebarHeaderHeight()
	effectiveHeaderHeight := maxInt(1, headerHeight)
	contentHeight := m.height - 2 - effectiveHeaderHeight
	spaceForScroll := maxInt(3, contentHeight-len(data.topLines)-len(data.bottomLines))
	scrollBoxHeight := m.sidebarScrollBoxHeight(data, headerHeight)
	visible := minInt(scrollBoxHeight, spaceForScroll)
	if visible < 1 {
		t.Fatal("expected a visible sidebar scroll window")
	}
	if scrollIdx < visible-1 {
		t.Fatalf("target scrollIdx=%d cannot be placed on the last visible row (visible=%d)", scrollIdx, visible)
	}

	m.sidebarScroll = scrollIdx - (visible - 1)
	contentTopY := appHeaderHeight + 1 + effectiveHeaderHeight
	fileScreenY := contentTopY + len(data.topLines) + (scrollIdx - m.sidebarScroll)

	rendered := strings.Split(stripANSI(m.renderSidebar()), "\n")
	sidebarLine := fileScreenY - appHeaderHeight
	if sidebarLine < 0 || sidebarLine >= len(rendered) {
		t.Fatalf("computed sidebar row %d out of range for rendered sidebar with %d rows", sidebarLine, len(rendered))
	}
	if !strings.Contains(rendered[sidebarLine], targetPath) {
		t.Fatalf("expected rendered sidebar row %d to contain %q, got %q", sidebarLine, targetPath, rendered[sidebarLine])
	}

	updated, _ := m.Update(tea.MouseMotionMsg{X: m.panelWidth() + 1, Y: fileScreenY})
	if got := updated.(model).hoverSidebarFile; got != targetPath {
		t.Fatalf("expected hover on rendered bottom row Y=%d to set hoverSidebarFile=%q, got %q", fileScreenY, targetPath, got)
	}
}

// TestSidebarHitTestMatchesWrappedPinnedRows guards the invariant that the file
// hover/click hit-test (sidebarFileForClick) lands on the SAME screen row the
// sidebar actually renders the file on. The pinned header and topLines are
// rendered inside a width-constrained, padded border (inner width
// sidebarColumnWidth-4) that wraps long content to multiple visual rows, while
// the hit-test counts them as a single logical row each. When a pinned row
// wraps, every scroll row shifts down and the hit-test points N rows too high
// (the "hover 2 lines above the file" bug). Both sub-cases place a wrapping
// pinned row and assert the rendered row == the hit-tested row.
func TestSidebarHitTestMatchesWrappedPinnedRows(t *testing.T) {
	longTitle := strings.Repeat("wrap-me-title ", 6) // ~84 cols, wraps in a 34-col box
	longModel := strings.Repeat("x", 60)             // forces the advisor: topLine to wrap

	cases := []struct {
		name  string
		setup func(m *model)
	}{
		{
			name:  "long session title wraps the header",
			setup: func(m *model) { m.sessionTitle = longTitle },
		},
		{
			name: "long advisor model wraps a topLine",
			setup: func(m *model) {
				m.config = &config.Config{Ocode: config.OcodeConfig{Advisor: config.AdvisorConfig{Model: longModel}}}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			origWd, _ := os.Getwd()
			defer os.Chdir(origWd)
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			snapshot.Reset()
			tool.SetTodoSession("session-wrap")
			tool.ResetTodoState()

			const target = "zzz_unique_target.go"
			if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := snapshot.Backup(target); err != nil {
				t.Fatal(err)
			}

			m := model{ready: true, width: 140, height: 40, showSidebar: true, input: textarea.New(), viewport: viewport.New(viewport.WithWidth(100), viewport.WithHeight(20))}
			tc.setup(&m)

			// Find the file's ACTUAL rendered row within the sidebar box.
			rendered := strings.Split(stripANSI(m.renderSidebar()), "\n")
			renderedRow := -1
			for i, line := range rendered {
				if strings.Contains(line, target) {
					renderedRow = i
					break
				}
			}
			if renderedRow < 0 {
				t.Fatalf("target %q not found in rendered sidebar", target)
			}

			// Convert to a full-view screen Y (the sidebar is joined below the app
			// header) and ask the production hit-test what file lives there.
			fileScreenY := renderedRow + appHeaderHeight
			sidebarX := m.panelWidth() + 1
			got, ok := m.sidebarFileForClick(tea.Mouse{X: sidebarX, Y: fileScreenY})
			if !ok || got != target {
				t.Fatalf("hit-test/render mismatch: file renders on screen row %d but sidebarFileForClick there returned (%q, %v); pinned-row wrap offset not accounted for",
					fileScreenY, got, ok)
			}
		})
	}
}

func TestSidebarContextWindowLookup(t *testing.T) {
	if got, ok := modelContextWindow("gpt-4o"); !ok || got == 0 {
		t.Fatalf("expected known context window for gpt-4o, got %d ok=%v", got, ok)
	}

	if _, ok := modelContextWindow("made-up-model"); ok {
		t.Fatal("expected unknown model to have no context window")
	}
}

func TestHandleModelCmdUpdatesCurrentModel(t *testing.T) {
	m := model{config: &config.Config{
		Model: "gpt-4o",
		Provider: map[string]interface{}{
			"custom": map[string]interface{}{
				"options": map[string]interface{}{
					"baseURL": "https://example.invalid",
				},
			},
		},
	}}
	m.handleModelCmd([]string{"custom:demo"})

	if got := m.currentModelName(); got != "custom:demo" {
		t.Fatalf("expected active model to update, got %q", got)
	}
	if len(m.messages) != 1 {
		t.Fatalf("expected one switch notice, got %#v", m.messages)
	}
	if !m.messages[0].transient {
		t.Fatalf("expected switch notice to be transient, got %#v", m.messages[0])
	}
}

func TestHandleModelCmdSwitchNoticeStaysOutOfLLMPayload(t *testing.T) {
	m := model{config: &config.Config{
		Model: "gpt-4o",
		Provider: map[string]interface{}{
			"custom": map[string]interface{}{
				"options": map[string]interface{}{
					"baseURL": "https://example.invalid",
				},
			},
		},
	}}
	m.handleModelCmd([]string{"custom:demo"})

	const notice = "Switching to model custom:demo"
	for _, msg := range m.persistedAgentMessages() {
		if msg.Content == notice {
			t.Fatalf("expected switch notice to stay out of persisted messages, got %#v", m.persistedAgentMessages())
		}
	}
	snap, _ := m.buildAgentMessagesSnapshot()
	for _, msg := range snap {
		if msg.Content == notice {
			t.Fatalf("expected switch notice to stay out of llm snapshot, got %#v", snap)
		}
	}
}

func TestModelPickerShowsFavoritesAndRecentsFirst(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := config.SaveRecentModel("openai/gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveRecentModel("anthropic/claude-sonnet-4-20250514"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveFavoriteModel("openai/gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}

	m := model{}
	m.openModelPicker()

	if len(m.pickerItems) < 4 {
		t.Fatalf("expected grouped picker items, got %#v", m.pickerItems)
	}
	if m.pickerItems[0] != "★ Favorites" {
		t.Fatalf("expected favorites header first, got %#v", m.pickerItems[:4])
	}
	if !strings.Contains(m.pickerItems[1], "gpt-4o-mini") || m.pickerValues[1] != "openai/gpt-4o-mini" {
		t.Fatalf("expected favorite model first, got items=%#v values=%#v", m.pickerItems[:4], m.pickerValues[:4])
	}
	if !containsString(m.pickerItems, "Recently Used") {
		t.Fatalf("expected recent section, got %#v", m.pickerItems)
	}
	for i, value := range m.pickerValues {
		if value == "openai/gpt-4o-mini" && i != 1 {
			t.Fatalf("favorite should not be duplicated in recents/providers, got duplicate at %d in %#v", i, m.pickerValues)
		}
	}
}

func TestModelPickerToggleFavorite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := model{showPicker: true, pickerKind: "model", pickerItems: []string{"openai/gpt-4o-mini"}, pickerValues: []string{"openai/gpt-4o-mini"}}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := derefTestModel(t, updated)
	if !config.IsFavorite("openai/gpt-4o-mini") {
		t.Fatal("expected f to add selected model to favorites")
	}
	if !got.showPicker || got.pickerKind != "model" {
		t.Fatalf("expected model picker to remain open, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}

	got.pickerIndex = 1
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	_ = updated
	if config.IsFavorite("openai/gpt-4o-mini") {
		t.Fatal("expected f to remove selected model from favorites")
	}
}

func TestModelPickerFilterDebounces(t *testing.T) {
	m := model{showPicker: true, pickerKind: "model", pickerItems: []string{"openai/gpt-4o-mini"}, pickerValues: []string{"openai/gpt-4o-mini"}}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got := derefTestModel(t, updated)

	if cmd == nil {
		t.Fatal("expected debounce cmd after filter keypress, got nil")
	}
	// pending input visible immediately, applied filter deferred
	if got.pickerFilterPending != "g" {
		t.Fatalf("expected pickerFilterPending=%q, got %q", "g", got.pickerFilterPending)
	}
	if got.pickerFilter != "" {
		t.Fatalf("expected pickerFilter to remain empty before debounce fires, got %q", got.pickerFilter)
	}
}

func TestModelPickerFilterPreservesGrouping(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := config.SaveRecentModel("openai/gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveFavoriteModel("openai/gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}

	m := model{}
	m.openModelPicker()
	m.pickerFilter = "gpt-4o-mini"

	items, values := m.pickerVisibleItems()
	if len(items) < 2 {
		t.Fatalf("expected grouped filtered items, got items=%#v values=%#v", items, values)
	}
	if items[0] != "★ Favorites" {
		t.Fatalf("expected favorites header to be preserved, got %#v", items)
	}
	if values[0] != "" {
		t.Fatalf("expected header to remain unselectable, got values=%#v", values)
	}
	if values[1] != "openai/gpt-4o-mini" {
		t.Fatalf("expected matching model under preserved group, got items=%#v values=%#v", items, values)
	}
}

// TestModelPickerFilterWithSeparatorsEndToEnd exercises pickerVisibleItems with
// a separator-containing filter ("gpt 5.4") to verify the keyword-splitting
// path through the full section-grouping pipeline. The previous grouping test
// only used a single keyword with no separators, which didn't exercise the
// splitting path that production users hit when typing "gpt 4o" or "gpt-4o".
func TestModelPickerFilterWithSeparatorsEndToEnd(t *testing.T) {
	// Isolate both favorites/recent storage and the registry disk cache so
	// the picker falls through to the embedded snapshot deterministically.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)
	t.Setenv("APPDATA", home)

	if err := config.SaveRecentModel("openai/gpt-5.4-mini"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveFavoriteModel("openai/gpt-5.4-mini"); err != nil {
		t.Fatal(err)
	}

	m := model{}
	m.openModelPicker()
	m.pickerFilter = "gpt 5.4"

	items, values := m.pickerVisibleItems()
	if len(items) < 2 {
		t.Fatalf("expected grouped filtered items, got items=%#v values=%#v", items, values)
	}
	if items[0] != "★ Favorites" {
		t.Fatalf("expected favorites header to be preserved across keyword-splitting path, got %#v", items)
	}
	if values[0] != "" {
		t.Fatalf("expected header to remain unselectable, got values=%#v", values)
	}
	if values[1] != "openai/gpt-5.4-mini" {
		t.Fatalf("expected openai/gpt-5.4-mini to match 'gpt 5.4' under preserved group, got items=%#v values=%#v", items, values)
	}
	// Ensure the unrelated gpt-4o model was filtered out by the keyword match.
	for _, v := range values {
		if v == "openai/gpt-4o" {
			t.Fatalf("expected gpt-4o to be excluded by 'gpt 5.4' filter, got values=%#v", values)
		}
	}
}

func TestModelPickerFilterKeywordSplitting(t *testing.T) {
	cases := []struct {
		name     string
		filter   string
		contains []string
		excludes []string
	}{
		{
			name:     "space separated keywords AND-match across dashes",
			filter:   "gpt 4o",
			contains: []string{"openai/gpt-4o-mini", "openai/gpt-4o"},
			excludes: []string{"openai/gpt-5"},
		},
		{
			name:     "dash separated keywords match space form",
			filter:   "claude-opus-4",
			contains: []string{"anthropic/claude-opus-4-7"},
			excludes: []string{"anthropic/claude-3-5-sonnet"},
		},
		{
			name:     "single keyword substring match",
			filter:   "sonnet",
			contains: []string{"anthropic/claude-3-5-sonnet"},
			excludes: []string{"openai/gpt-4o-mini"},
		},
		{
			name:     "provider prefix matches via value",
			filter:   "anthropic",
			contains: []string{"anthropic/claude-3-5-sonnet"},
		},
		{
			name:     "underscore treated as separator",
			filter:   "gpt_4o",
			contains: []string{"openai/gpt-4o-mini"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := modelPickerKeywords(tc.filter)
			if len(got) == 0 {
				t.Fatalf("expected at least one keyword from %q", tc.filter)
			}
			for _, haystack := range tc.contains {
				if !modelPickerMatches(strings.ToLower(haystack), tc.filter) {
					t.Errorf("expected %q to match filter %q (keywords=%v)", haystack, tc.filter, got)
				}
			}
			for _, haystack := range tc.excludes {
				if modelPickerMatches(strings.ToLower(haystack), tc.filter) {
					t.Errorf("expected %q NOT to match filter %q (keywords=%v)", haystack, tc.filter, got)
				}
			}
		})
	}
}

func TestModelPickerFilterEmptyAndWhitespace(t *testing.T) {
	for _, q := range []string{"", " ", "   ", "-", " - - "} {
		if !modelPickerMatches("openai/gpt-4o-mini", q) {
			t.Errorf("expected empty/whitespace filter %q to match everything", q)
		}
	}
}

func TestModelPickerKeywordSplitting(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"gpt 4o", []string{"gpt", "4o"}},
		{"gpt-4o", []string{"gpt", "4o"}},
		{"gpt_4o", []string{"gpt", "4o"}},
		{"  gpt   4o  ", []string{"gpt", "4o"}},
		{"claude-opus-4-7", []string{"claude", "opus", "4", "7"}},
		{"", nil},
		{"   ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := modelPickerKeywords(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("keywords(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("keywords(%q) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestPickerSelectsSessionByValue(t *testing.T) {
	m := model{
		input:        textarea.New(),
		viewport:     viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		showPicker:   true,
		pickerKind:   "session",
		pickerItems:  []string{"session-1  First session"},
		pickerValues: []string{"session-1"},
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)
	if got.showPicker {
		t.Fatal("expected session picker to close")
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "Error loading session") {
		t.Fatalf("expected picker to load selected session id, got %#v", got.messages)
	}
}

func TestOpenSessionPickerLoadsRefsAsync(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	if err := session.Save("ses_2026-06-01-120000", "First session", []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatal(err)
	}

	m := model{input: textarea.New()}
	cmd := m.openSessionPicker()
	if !m.showPicker || m.pickerKind != "session" || !m.pickerSessionLoading {
		t.Fatalf("expected session picker to open in loading state, got %+v", m)
	}
	if m.pickerSessionPage != 0 || len(m.pickerItems) != 0 || len(m.pickerValues) != 0 {
		t.Fatalf("expected empty picker before refs load, got page=%d items=%v values=%v", m.pickerSessionPage, m.pickerItems, m.pickerValues)
	}
	if cmd == nil {
		t.Fatal("expected async load command from openSessionPicker")
	}

	updated, _ := m.Update(cmd())
	got := updated.(model)
	if got.pickerSessionLoading {
		t.Fatal("expected loading flag to clear after refs load")
	}
	if got.pickerSessionTotal != 1 || got.pickerSessionPage != 1 || got.pickerSessionMore {
		t.Fatalf("unexpected session paging state after load: total=%d page=%d more=%v", got.pickerSessionTotal, got.pickerSessionPage, got.pickerSessionMore)
	}
	if len(got.pickerItems) != 1 || len(got.pickerValues) != 1 {
		t.Fatalf("expected one session item after load, got items=%v values=%v", got.pickerItems, got.pickerValues)
	}
	if got.pickerValues[0] != "ses_2026-06-01-120000" {
		t.Fatalf("expected picker value to match session id, got %q", got.pickerValues[0])
	}
	if !strings.Contains(got.pickerItems[0], "First session") {
		t.Fatalf("expected picker item to include title, got %q", got.pickerItems[0])
	}
}

func TestSessionPickerLoadsAllRefsWhenFilterActive(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	total := sessionPickerPageSize + 2
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("ses_2026-06-01-1200%02d", i)
		title := fmt.Sprintf("Session %02d", i)
		if err := session.Save(id, title, []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
			t.Fatal(err)
		}
	}

	m := model{input: textarea.New()}
	m.openSessionPicker()
	m.pickerFilter = "session"
	cmd := loadSessionRefsCmd(m.pickerSessionLoadSeq)
	updated, _ := m.Update(cmd())
	got := updated.(model)
	if got.pickerSessionLoading {
		t.Fatal("expected loading flag to clear after refs load")
	}
	if got.pickerSessionPage != (total+sessionPickerPageSize-1)/sessionPickerPageSize {
		t.Fatalf("expected filter to expand session pages, got page=%d total=%d pageSize=%d", got.pickerSessionPage, total, sessionPickerPageSize)
	}
	if got.pickerSessionMore {
		t.Fatal("expected filtered session picker to load all refs")
	}
	if len(got.pickerItems) != total {
		t.Fatalf("expected all sessions to be loaded for active filter, got %d want %d", len(got.pickerItems), total)
	}
}

func TestSessionPickerLoadResultIgnoredAfterClose(t *testing.T) {
	m := model{input: textarea.New()}
	_ = m.openSessionPicker()
	seq := m.pickerSessionLoadSeq
	m.closePicker()

	updated, _ := m.Update(sessionRefsLoadedMsg{seq: seq, refs: []session.Ref{{ID: "ses_1", Title: "ignored"}}})
	got := updated.(model)
	if got.showPicker {
		t.Fatal("expected closed picker to stay closed when stale load arrives")
	}
	if got.pickerSessionLoading || got.pickerSessionLoadErr != "" || len(got.pickerSessionRefs) != 0 {
		t.Fatalf("expected stale load to be ignored, got loading=%v err=%q refs=%v", got.pickerSessionLoading, got.pickerSessionLoadErr, got.pickerSessionRefs)
	}
}

func TestSessionPickerIgnoresStaleLoadAfterReopen(t *testing.T) {
	m := model{input: textarea.New()}
	firstCmd := m.openSessionPicker()
	if firstCmd == nil {
		t.Fatal("expected first session load command")
	}
	firstSeq := m.pickerSessionLoadSeq
	secondCmd := m.openSessionPicker()
	if secondCmd == nil {
		t.Fatal("expected second session load command")
	}
	secondSeq := m.pickerSessionLoadSeq
	if secondSeq <= firstSeq {
		t.Fatalf("expected reopen to advance load sequence, got first=%d second=%d", firstSeq, secondSeq)
	}

	updated, _ := m.Update(sessionRefsLoadedMsg{seq: firstSeq, refs: []session.Ref{{ID: "ses_old", Title: "old"}}})
	got := updated.(model)
	if got.pickerSessionLoadSeq != secondSeq {
		t.Fatalf("expected active load sequence to stay on reopen, got %d want %d", got.pickerSessionLoadSeq, secondSeq)
	}
	if !got.pickerSessionLoading {
		t.Fatal("expected reopened picker to remain loading after stale result")
	}
	if len(got.pickerSessionRefs) != 0 || len(got.pickerItems) != 0 {
		t.Fatalf("expected stale load to be ignored, got refs=%v items=%v", got.pickerSessionRefs, got.pickerItems)
	}
}

func TestMessagePickerOnlyListsActualUserInputs(t *testing.T) {
	m := model{
		messages: []message{
			{
				role: roleUser,
				text: "include raw user",
				raw:  &agent.Message{Role: "user", Content: "include raw user"},
			},
			{
				role: roleUser,
				text: "exclude restored system context",
				raw:  &agent.Message{Role: "system", Content: "exclude restored system context"},
			},
			{
				role: roleAssistant,
				text: "exclude assistant",
				raw:  &agent.Message{Role: "assistant", Content: "exclude assistant"},
			},
			{
				role: roleUser,
				text: "include live user",
			},
		},
	}

	m.openMessagePicker()

	if got, want := len(m.pickerItems), 2; got != want {
		t.Fatalf("expected %d revert items, got %d: %#v", want, got, m.pickerItems)
	}
	if !strings.Contains(m.pickerItems[0], "include raw user") || !strings.Contains(m.pickerItems[1], "include live user") {
		t.Fatalf("expected picker to list user inputs, got %#v", m.pickerItems)
	}
	for _, item := range m.pickerItems {
		if strings.Contains(item, "system") || strings.Contains(item, "assistant") {
			t.Fatalf("expected picker to exclude non-user messages, got %#v", m.pickerItems)
		}
	}
	if got, want := strings.Join(m.pickerValues, ","), "0,3"; got != want {
		t.Fatalf("expected picker values to retain transcript indexes, got %q", got)
	}
}

func TestMessagePickerRestoresBeforeSelectedInputAndPrefillsIt(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(".ocode/sessions", 0755); err != nil {
		t.Fatal(err)
	}

	m := model{
		sessionID: "restore-test",
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		messages: []message{
			{role: roleUser, text: "first request"},
			{role: roleAssistant, text: "first answer"},
			{role: roleUser, text: "retry this request"},
			{role: roleAssistant, text: "failed answer"},
		},
	}
	m.openMessagePicker()
	m.pickerIndex = 1

	updated, cmd := m.selectPickerIndex(1)
	if cmd != nil {
		t.Fatalf("expected no command, got %T", cmd)
	}
	got := derefTestModel(t, updated)

	if len(got.messages) != 2 {
		t.Fatalf("expected transcript before selected input, got %#v", got.messages)
	}
	if got.input.Value() != "retry this request" {
		t.Fatalf("expected selected input to be prefixed, got %q", got.input.Value())
	}
}

func TestCtrlYRetriesLastRetryableLLMError(t *testing.T) {
	errText := "Error: context deadline exceeded"
	m := model{
		input:               textarea.New(),
		viewport:            viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		agent:               agent.NewAgent(retryTestClient{}, nil, nil),
		lastRetryableLLMErr: errText,
		messages: []message{
			{role: roleUser, text: "please retry"},
			{role: roleAssistant, text: errText},
		},
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	got := derefTestModel(t, updated)

	if cmd == nil {
		t.Fatal("expected retry command")
	}
	if got.lastRetryableLLMErr != "" {
		t.Fatalf("expected retryable error state to clear, got %q", got.lastRetryableLLMErr)
	}
	if len(got.messages) != 1 || got.messages[0].text != "please retry" {
		t.Fatalf("expected transient error removed before retry, got %#v", got.messages)
	}
}

func TestMouseWheelScrollsTranscriptOnlyWhenOverMessages(t *testing.T) {
	lines := strings.Repeat("message line\n", 20)
	m := model{
		width:       80,
		height:      24,
		input:       newTestTextarea(),
		viewport:    viewport.New(viewport.WithWidth(40), viewport.WithHeight(3)),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
	}
	m.viewport.SetContent(lines)

	updated, _ := m.Update(tea.MouseWheelMsg{X: 2, Y: 2, Button: tea.MouseWheelDown})
	got := derefTestModel(t, updated)
	if got.viewport.YOffset() == 0 {
		t.Fatal("expected mouse wheel over transcript to scroll messages")
	}

	before := got.viewport.YOffset()
	updated, _ = got.Update(tea.MouseWheelMsg{X: 2, Y: 8, Button: tea.MouseWheelDown})
	got = derefTestModel(t, updated)
	if got.viewport.YOffset() != before {
		t.Fatalf("expected wheel outside transcript to leave messages offset at %d, got %d", before, got.viewport.YOffset())
	}
}

func TestMouseWheelScrollsAgentDetailView(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	run := a.Runs().New("worker")
	msgs := make([]agent.Message, 0, 80)
	for i := 0; i < 40; i++ {
		msgs = append(msgs,
			agent.Message{Role: "user", Content: "task line"},
			agent.Message{Role: "assistant", Content: "reply line"},
		)
	}
	setRunTranscriptForTest(run, msgs...)

	m := model{
		ready:       true,
		width:       80,
		height:      24,
		activeTab:   tabChat,
		input:       newTestTextarea(),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
		agent:       a,
	}
	m.openAgentDetail(run.ID)

	updated, _ := m.Update(tea.MouseWheelMsg{X: 2, Y: m.detailViewportContentTopY(), Button: tea.MouseWheelDown})
	got := derefTestModel(t, updated)
	if len(got.detail) == 0 || got.detail[len(got.detail)-1].vp.YOffset() == 0 {
		t.Fatal("expected mouse wheel to scroll agent detail view")
	}
	before := got.detail[len(got.detail)-1].vp.YOffset()
	updated, _ = got.Update(tea.MouseWheelMsg{X: 2, Y: got.height - 1, Button: tea.MouseWheelDown})
	got = derefTestModel(t, updated)
	if got.detail[len(got.detail)-1].vp.YOffset() != before {
		t.Fatalf("expected wheel outside detail viewport to keep offset %d, got %d", before, got.detail[len(got.detail)-1].vp.YOffset())
	}
}

func TestAgentDetailScrollbarTrackClickJumpsWithoutStartingDrag(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	run := a.Runs().New("worker")
	msgs := make([]agent.Message, 0, 120)
	for i := 0; i < 60; i++ {
		msgs = append(msgs, agent.Message{Role: "assistant", Content: "detail line"})
	}
	setRunTranscriptForTest(run, msgs...)

	m := model{
		ready:     true,
		width:     80,
		height:    24,
		activeTab: tabChat,
		input:     newTestTextarea(),
		styles:    ApplyThemeColors("tokyonight"),
		agent:     a,
	}
	m.openAgentDetail(run.ID)
	m.detail[len(m.detail)-1].vp.SetYOffset(30)
	before := m.detail[len(m.detail)-1].vp.YOffset()

	top := m.detail[len(m.detail)-1]
	trackTop, trackHeight := m.detailScrollbarMetrics()
	thumbTop, thumbSize, ok := scrollbarThumbMetrics(trackHeight, top.vp.TotalLineCount(), top.vp.VisibleLineCount(), before)
	if !ok {
		t.Fatal("expected scrollable detail viewport")
	}
	trackRow := 0
	if trackRow >= thumbTop && trackRow < thumbTop+thumbSize {
		trackRow = thumbTop + thumbSize
	}
	if trackRow >= trackHeight {
		trackRow = 0
	}

	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.detailScrollbarX(), Y: trackTop + trackRow})
	got := derefTestModel(t, updated)
	if got.detail[len(got.detail)-1].vp.YOffset() == before {
		t.Fatalf("expected detail scrollbar track click to jump offset from %d", before)
	}
	if got.scrollbarDrag != scrollbarDragNone {
		t.Fatalf("expected detail scrollbar track click not to start drag, got %v", got.scrollbarDrag)
	}
}

func TestAgentDetailClickOpensNestedSubAgent(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	run := a.Runs().New("worker")
	run.Sub = agent.NewAgent(nil, nil, nil)
	child := run.Sub.Runs().New("child")
	setRunTranscriptForTest(run, agent.Message{Role: "assistant", Content: "root"})
	setRunTranscriptForTest(child, agent.Message{Role: "assistant", Content: "child"})

	m := model{ready: true, width: 100, height: 28, activeTab: tabChat, input: newTestTextarea(), styles: ApplyThemeColors("tokyonight"), agent: a}
	m.openAgentDetail(run.ID)
	top := m.detail[len(m.detail)-1]
	var row int
	found := false
	for _, b := range top.runs {
		if b.runPath != top.runPath {
			row = b.rowStart
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected child run block in detail view")
	}

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: 2, Y: m.detailViewportContentTopY() + row})
	got := derefTestModel(t, updated)
	if len(got.detail) < 2 {
		t.Fatal("expected clicking child run to push nested detail view")
	}
	if got.detail[len(got.detail)-1].runID != child.ID {
		t.Fatalf("expected nested detail for %q, got %q", child.ID, got.detail[len(got.detail)-1].runID)
	}
}

func TestMouseWheelScrollsAgentDetailViewport(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	run := a.Runs().New("worker")
	msgs := make([]agent.Message, 0, 60)
	for i := 0; i < 60; i++ {
		msgs = append(msgs, agent.Message{Role: "assistant", Content: "detail line"})
	}
	setRunTranscriptForTest(run, msgs...)

	m := model{
		ready:       true,
		width:       80,
		height:      24,
		activeTab:   tabChat,
		input:       newTestTextarea(),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
		agent:       a,
	}
	m.openAgentDetail(run.ID)

	updated, _ := m.Update(tea.MouseWheelMsg{X: 2, Y: m.detailViewportContentTopY(), Button: tea.MouseWheelDown})
	got := derefTestModel(t, updated)
	if got.detail[len(got.detail)-1].vp.YOffset() == 0 {
		t.Fatal("expected mouse wheel over detail viewport to scroll")
	}

	before := got.detail[len(got.detail)-1].vp.YOffset()
	updated, _ = got.Update(tea.MouseWheelMsg{X: 2, Y: got.height - 1, Button: tea.MouseWheelDown})
	got = derefTestModel(t, updated)
	if got.detail[len(got.detail)-1].vp.YOffset() != before {
		t.Fatalf("expected wheel outside detail viewport to keep offset at %d, got %d", before, got.detail[len(got.detail)-1].vp.YOffset())
	}
}

func TestAgentDetailShowsAndOpensRunBackgroundProcess(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	run := a.Runs().New("worker")
	run.Procs = tool.NewProcessRegistry()
	proc := run.Procs.StartBackground("printf hello")
	t.Cleanup(func() { _, _ = run.Procs.Kill(proc.ID) })
	setRunTranscriptForTest(run, agent.Message{Role: "assistant", Content: "root"})
	time.Sleep(50 * time.Millisecond)

	m := model{ready: true, width: 100, height: 28, activeTab: tabChat, input: newTestTextarea(), styles: ApplyThemeColors("tokyonight"), agent: a}
	m.openAgentDetail(run.ID)
	top := m.detail[len(m.detail)-1]
	if len(top.procs) == 0 {
		t.Fatal("expected background process blocks in agent detail view")
	}
	row := top.procs[0].rowStart

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: 2, Y: m.detailViewportContentTopY() + row})
	got := derefTestModel(t, updated)
	if len(got.detail) < 2 {
		t.Fatal("expected clicking process row to open process log detail")
	}
	if got.detail[len(got.detail)-1].kind != detailProcessLog {
		t.Fatalf("expected process log detail, got %v", got.detail[len(got.detail)-1].kind)
	}
	if got.detail[len(got.detail)-1].procID != proc.ID {
		t.Fatalf("expected process log for %q, got %q", proc.ID, got.detail[len(got.detail)-1].procID)
	}
}

func TestAgentDetailSuppressesHiddenInputEditing(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	run := a.Runs().New("worker")
	setRunTranscriptForTest(run, agent.Message{Role: "assistant", Content: "root"})

	m := model{ready: true, width: 100, height: 28, activeTab: tabChat, input: newTestTextarea(), styles: ApplyThemeColors("tokyonight"), agent: a}
	m.openAgentDetail(run.ID)

	updated, _ := m.Update(tea.KeyPressMsg{Text: "x"})
	got := derefTestModel(t, updated)
	if got.input.Value() != "" {
		t.Fatalf("expected hidden chat input to stay unchanged while detail view is open, got %q", got.input.Value())
	}
}

func TestTranscriptScrollbarTrackClickJumpsWithoutStartingDrag(t *testing.T) {
	m := model{
		width:     80,
		height:    24,
		activeTab: tabChat,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(40), viewport.WithHeight(10)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.viewport.SetContent(strings.Repeat("message line\n", 200))
	m.viewport.SetYOffset(60)
	before := m.viewport.YOffset()

	thumbTop, thumbSize, ok := scrollbarThumbMetrics(m.viewport.Height(), m.viewport.TotalLineCount(), m.viewport.VisibleLineCount(), before)
	if !ok {
		t.Fatal("expected scrollable transcript")
	}
	trackRow := 0
	if trackRow >= thumbTop && trackRow < thumbTop+thumbSize {
		trackRow = thumbTop + thumbSize
	}
	if trackRow >= m.viewport.Height() {
		trackRow = 0
	}
	trackTop := appHeaderHeight + 1

	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainScrollbarX(), Y: trackTop + trackRow})
	got := derefTestModel(t, updated)
	if got.viewport.YOffset() == before {
		t.Fatalf("expected scrollbar track click to jump transcript, offset stayed at %d", before)
	}
	if got.scrollbarDrag != scrollbarDragNone {
		t.Fatalf("expected scrollbar track click not to start drag, got %v", got.scrollbarDrag)
	}

	afterClick := got.viewport.YOffset()
	updated, _ = got.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.mainScrollbarX(), Y: trackTop + min(trackRow+2, m.viewport.Height()-1)})
	got = derefTestModel(t, updated)
	if got.viewport.YOffset() != afterClick {
		t.Fatalf("expected motion after track click not to keep dragging transcript, before=%d after=%d", afterClick, got.viewport.YOffset())
	}
}

func TestTranscriptScrollbarThumbClickDoesNotJumpScroll(t *testing.T) {
	m := model{
		width:     80,
		height:    24,
		activeTab: tabChat,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(40), viewport.WithHeight(10)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.viewport.SetContent(strings.Repeat("message line\n", 200))
	m.viewport.SetYOffset(60)
	before := m.viewport.YOffset()

	thumbTop, _, ok := scrollbarThumbMetrics(m.viewport.Height(), m.viewport.TotalLineCount(), m.viewport.VisibleLineCount(), before)
	if !ok {
		t.Fatal("expected scrollable transcript")
	}
	trackTop := appHeaderHeight + 1

	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainScrollbarX(), Y: trackTop + thumbTop})
	got := derefTestModel(t, updated)
	if got.viewport.YOffset() != before {
		t.Fatalf("expected scrollbar thumb click not to jump transcript, before=%d after=%d", before, got.viewport.YOffset())
	}
	if got.scrollbarDrag != scrollbarDragTranscript {
		t.Fatalf("expected transcript scrollbar drag to start, got %v", got.scrollbarDrag)
	}
}

func TestGitDiffMouseDragSelectsDiffText(t *testing.T) {
	m := model{
		width:     100,
		height:    30,
		activeTab: tabGit,
		styles:    ApplyThemeColors("tokyonight"),
		git: gitModel{
			diff: viewport.New(viewport.WithWidth(45), viewport.WithHeight(10)),
		},
	}
	m.git.setDiffContent("line one\nline two\nline three")

	panelW := m.panelWidth()
	diffLeft := panelW*20/100 + panelW*30/100 + 1
	gitBodyTop := appHeaderHeight + 1
	updated, _, ok := m.handleMouseAction(tea.Mouse{Button: tea.MouseLeft, X: diffLeft, Y: gitBodyTop + 1}, true)
	if !ok {
		t.Fatal("expected mouse press in git diff panel to be handled")
	}
	got := updated.(model)
	updated, _, ok = got.handleMouseMotion(tea.Mouse{Button: tea.MouseLeft, X: diffLeft + 4, Y: gitBodyTop + 1})
	if !ok {
		t.Fatal("expected git diff drag motion to be handled")
	}
	got = updated.(model)

	if !got.gitSel.active || !got.gitSel.dragging {
		t.Fatalf("expected active dragging selection, got %#v", got.gitSel)
	}
	if got.gitSel.startLine != 0 || got.gitSel.endLine != 0 || got.gitSel.startCol != 0 || got.gitSel.endCol != 4 {
		t.Fatalf("unexpected selection state: %#v", got.gitSel)
	}
	if !strings.Contains(got.git.diff.View(), "\x1b[7m") {
		t.Fatalf("expected highlighted diff content, got %q", got.git.diff.View())
	}
}

func TestGitRightClickDeselectsActiveFile(t *testing.T) {
	m := model{
		width:     100,
		height:    30,
		activeTab: tabGit,
		styles:    ApplyThemeColors("tokyonight"),
		git: gitModel{
			section:       gitSectionChanges,
			panel:         gitPanelFiles,
			unstagedFiles: []gitFile{{status: "M", path: "main.go"}},
			filesCursor:   0,
			diff:          viewport.New(viewport.WithWidth(45), viewport.WithHeight(10)),
		},
	}
	m.git.setDiffContent("diff content")

	panelW := m.panelWidth()
	gitHeaderH := appHeaderHeight
	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: panelW * 20 / 100, Y: gitHeaderH + 2})
	got := derefTestModel(t, updated)

	if got.git.filesCursor != -1 {
		t.Fatalf("expected git files cursor to be cleared, got %d", got.git.filesCursor)
	}
	if strings.TrimSpace(got.git.diff.View()) != "" {
		t.Fatalf("expected git diff to be cleared, got %q", got.git.diff.View())
	}
}

func TestFilesRightClickDeselectsActiveFile(t *testing.T) {
	m := model{
		width:     100,
		height:    30,
		activeTab: tabFiles,
		styles:    ApplyThemeColors("tokyonight"),
		files: filesModel{
			width:   100,
			nodes:   []fileNode{{path: "/tmp/main.go", name: "main.go"}},
			cursor:  0,
			preview: viewport.New(viewport.WithWidth(45), viewport.WithHeight(10)),
		},
	}
	m.files.previewPath = "/tmp/main.go"
	m.files.preview.SetContent("package main")
	m.files.previewRawLines = []string{"package main"}
	m.files.previewLines = []string{"package main"}

	filesHeaderH := appHeaderHeight
	updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: 1, Y: filesHeaderH + 1})
	got := derefTestModel(t, updated)

	if got.files.cursor != -1 {
		t.Fatalf("expected files cursor to be cleared, got %d", got.files.cursor)
	}
	if got.files.previewPath != "" {
		t.Fatalf("expected files preview path to be cleared, got %q", got.files.previewPath)
	}
	if strings.TrimSpace(got.files.preview.View()) != "" {
		t.Fatalf("expected files preview to be cleared, got %q", got.files.preview.View())
	}
}

func TestUpKeyUsesInputHistoryWithoutScrollingTranscript(t *testing.T) {
	m := model{
		input:             newTestTextarea(),
		viewport:          viewport.New(viewport.WithWidth(40), viewport.WithHeight(3)),
		inputHistory:      []string{"first", "second"},
		inputHistoryIndex: -1,
		scrollSpeed:       3,
	}
	m.viewport.SetContent(strings.Repeat("message line\n", 20))
	m.viewport.ScrollDown(6)
	before := m.viewport.YOffset()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	got := derefTestModel(t, updated)
	if got.input.Value() != "second" {
		t.Fatalf("expected up key to recall latest input history entry, got %q", got.input.Value())
	}
	if got.viewport.YOffset() != before {
		t.Fatalf("expected up key not to scroll transcript, offset changed from %d to %d", before, got.viewport.YOffset())
	}
}

func TestSlashCommandAddedToInputHistory(t *testing.T) {
	m := model{
		ready:             true,
		width:             80,
		height:            24,
		input:             newTestTextarea(),
		viewport:          viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:            ApplyThemeColors("tokyonight"),
		inputHistoryIndex: -1,
	}
	m.input.SetValue("/help")
	m.layout()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)

	if len(got.inputHistory) != 1 {
		t.Fatalf("expected slash command in history, got %d entries: %v", len(got.inputHistory), got.inputHistory)
	}
	if got.inputHistory[0] != "/help" {
		t.Fatalf("expected /help in history, got %q", got.inputHistory[0])
	}
}

func TestShellCommandNotAddedToInputHistory(t *testing.T) {
	m := model{
		ready:             true,
		width:             80,
		height:            24,
		input:             newTestTextarea(),
		viewport:          viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:            ApplyThemeColors("tokyonight"),
		inputHistoryIndex: -1,
	}
	m.input.SetValue("!echo hello")
	m.layout()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)

	if len(got.inputHistory) != 0 {
		t.Fatalf("expected shell command not in history, got %d entries: %v", len(got.inputHistory), got.inputHistory)
	}
}

func TestEnterWhileStreamingQueuesUserInput(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		streaming: true,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.input.SetValue("follow up after this")
	m.layout()

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)

	if cmd != nil {
		t.Fatalf("expected queued input to avoid starting a command, got %T", cmd)
	}
	if len(got.queuedInputs) != 1 || got.queuedInputs[0] != "follow up after this" {
		t.Fatalf("expected input to be queued, got %#v", got.queuedInputs)
	}
	if got.input.Value() != "" {
		t.Fatalf("expected input to reset after queueing, got %q", got.input.Value())
	}
	if len(got.messages) != 0 {
		t.Fatalf("expected queued input not to enter transcript yet, got %#v", got.messages)
	}
	if content := got.renderContent(); !strings.Contains(content, "Queued (1)") || !strings.Contains(content, "follow up after this") {
		t.Fatalf("expected queued input to render, got %q", content)
	}
}

func TestStreamDoneStartsNextQueuedInput(t *testing.T) {
	m := model{
		ready:        true,
		width:        80,
		height:       24,
		streaming:    true,
		agent:        agent.NewAgent(nil, nil, nil),
		input:        newTestTextarea(),
		viewport:     viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:       ApplyThemeColors("tokyonight"),
		queuedInputs: []string{"next request"},
	}
	m.layout()

	updated, cmd := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)

	if got.streaming {
		t.Fatal("expected streaming to stop")
	}
	if len(got.queuedInputs) != 0 {
		t.Fatalf("expected queued input to be consumed, got %#v", got.queuedInputs)
	}
	if cmd == nil {
		t.Fatal("expected next queued input command to start")
	}
}

func TestStreamDoneInterruptedDoesNotStartNextQueuedInput(t *testing.T) {
	m := model{
		ready:        true,
		width:        80,
		height:       24,
		streaming:    true,
		agent:        agent.NewAgent(nil, nil, nil),
		input:        newTestTextarea(),
		viewport:     viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:       ApplyThemeColors("tokyonight"),
		queuedInputs: []string{"next request"},
	}
	m.layout()

	updated, cmd := m.Update(streamDoneMsg{err: context.Canceled})
	got := derefTestModel(t, updated)

	if got.streaming {
		t.Fatal("expected streaming to stop")
	}
	if len(got.queuedInputs) != 1 {
		t.Fatalf("expected queued input to remain after interruption, got %#v", got.queuedInputs)
	}
	if cmd != nil {
		t.Fatalf("expected interrupted stream not to start next queued input, got %T", cmd)
	}
	if !got.streamWasInterrupted {
		t.Fatal("expected interrupted stream to be marked interrupted")
	}
}

func TestStreamDonePreservesActivityRowAndShowsIdleStatus(t *testing.T) {
	m := model{
		ready:               true,
		width:               80,
		height:              24,
		streaming:           true,
		activityRowReserved: true,
		lastActivity:        agent.ActivitySnapshot{LLMRunning: true},
		input:               newTestTextarea(),
		viewport:            viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:              ApplyThemeColors("tokyonight"),
	}
	m.layout()

	updated, _ := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)

	if got.streaming {
		t.Fatal("expected streaming to stop")
	}
	if !got.activityRowReserved {
		t.Fatal("expected activity row reservation to remain after stream done")
	}
	status := got.renderStatus()
	if !strings.Contains(status, "LLM: ○ idle") {
		t.Fatalf("expected idle LLM status, got %q", status)
	}
}

func TestRetryableLLMErrorDetection(t *testing.T) {
	if !isRetryableLLMError(context.DeadlineExceeded) {
		t.Fatal("expected context deadline to be retryable")
	}
	if isRetryableLLMError(os.ErrPermission) {
		t.Fatal("expected permission error to not be retryable")
	}
}

func TestPickerMouseRowUsesVisibleWindow(t *testing.T) {
	m := model{
		showPicker:   true,
		pickerKind:   "model",
		pickerIndex:  16,
		pickerItems:  []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16"},
		pickerValues: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16"},
	}

	idx, ok := m.pickerRowForY(3)
	if !ok {
		t.Fatal("expected picker row hit")
	}
	if idx != 2 {
		t.Fatalf("expected first visible picker row to map to index 2, got %d", idx)
	}

}

func TestPickerMouseReleaseSelectsModel(t *testing.T) {
	m := model{
		showPicker:   true,
		pickerKind:   "model",
		pickerItems:  []string{"test-model"},
		pickerValues: []string{"test-model"},
		input:        newTestTextarea(),
	}

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: 3, Y: 3})
	got := derefTestModel(t, updated)

	if got.showPicker {
		t.Fatal("expected picker to close after model row release")
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "test-model") {
		t.Fatalf("expected selected model message, got %#v", got.messages)
	}
}

func TestTabMouseReleaseUsesRightAlignedHeaderPosition(t *testing.T) {
	m := model{
		ready:     true,
		width:     100,
		height:    24,
		activeTab: tabFiles,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(96), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	barWidth := lipgloss.Width(renderTabBar(m.activeTab, m.chatUnread))
	barStart := m.tabBarStartXs(barWidth)[0]
	chatWidth := lipgloss.Width(hintStyle.Padding(0, 1).Render("1:chat"))
	filesWidth := lipgloss.Width(hintStyle.Padding(0, 1).Render("2:files"))

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: barStart + chatWidth + filesWidth + 1, Y: 1})
	got := derefTestModel(t, updated)

	if got.activeTab != tabGit {
		t.Fatalf("expected git tab after release, got %d", got.activeTab)
	}
}

func TestTabMouseMotionSwitchesWhenTerminalReportsDrag(t *testing.T) {
	m := model{
		ready:     true,
		width:     100,
		height:    24,
		activeTab: tabChat,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(96), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	barWidth := lipgloss.Width(renderTabBar(m.activeTab, m.chatUnread))
	barStart := m.tabBarStartXs(barWidth)[0]
	chatWidth := lipgloss.Width(hintStyle.Padding(0, 1).Render("1:chat"))

	updated, _ := m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: barStart + chatWidth + 1, Y: 1})
	got := derefTestModel(t, updated)

	if got.activeTab != tabFiles {
		t.Fatalf("expected files tab after left-button motion, got %d", got.activeTab)
	}
}

func TestMouseModeDefaultsOnWithoutConfig(t *testing.T) {
	m := model{ready: true, input: newTestTextarea()}

	// AllMotion (not CellMotion) so hover events arrive without a button held,
	// which the sidebar file hover-underline depends on.
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Fatalf("expected default mouse mode on without config, got %v", got)
	}
}

func TestMouseModeCanBeDisabledByConfig(t *testing.T) {
	disabled := false
	m := model{ready: true, input: newTestTextarea(), config: &config.Config{Ocode: config.OcodeConfig{}}}
	m.config.Ocode.TUI.Mouse = &disabled

	if got := m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("expected mouse mode off when configured false, got %v", got)
	}
}

func TestTuiRoleForAgentMessageOnlyMapsUserToUser(t *testing.T) {
	tests := []struct {
		role string
		want role
	}{
		{role: "user", want: roleUser},
		{role: "assistant", want: roleAssistant},
		{role: "tool", want: roleAssistant},
		{role: "system", want: roleAssistant},
	}

	for _, tt := range tests {
		if got := tuiRoleForAgentMessage(agent.Message{Role: tt.role}); got != tt.want {
			t.Fatalf("expected raw role %q to map to %v, got %v", tt.role, tt.want, got)
		}
	}
}

func TestDisplayTextForAgentMessageStripsCompactionMarker(t *testing.T) {
	got := displayTextForAgentMessage(agent.Message{
		Role:    "system",
		Content: "[ocode:compaction-summary]\nCompacted anchored summary (updated)\n\n## Goal\n- keep context",
	})
	if !strings.HasPrefix(got, "📦 Compacted anchored summary (updated)") {
		t.Fatalf("expected compaction banner, got %q", got)
	}
	if strings.Contains(got, "[ocode:compaction-summary]") {
		t.Fatalf("internal marker leaked into display: %q", got)
	}
}

func TestConnectMouseSelectsProviderAndMethodRows(t *testing.T) {
	if len(auth.Providers) < 2 {
		t.Fatal("expected at least two auth providers")
	}

	m := model{
		showConnect: true,
		connect: &connectDialog{
			stage: connectStageProvider,
		},
	}

	idx, ok := m.connectRowForY(4)
	if !ok {
		t.Fatal("expected provider row hit")
	}
	if idx != 1 {
		t.Fatalf("expected second provider row, got %d", idx)
	}

	updated, cmd := m.selectConnectRow(idx)
	if cmd != nil {
		t.Fatal("expected provider selection to stay in dialog without command")
	}
	got := derefTestModel(t, updated)
	if got.connect.stage != connectStageMethod || got.connect.providerIdx != 1 {
		t.Fatalf("expected provider click to open method stage, got %#v", got.connect)
	}

	idx, ok = got.connectRowForY(3)
	if !ok {
		t.Fatal("expected method row hit")
	}
	if idx != 0 {
		t.Fatalf("expected first method row, got %d", idx)
	}
}

func TestHandleModelCmdPreservesMCPProvenance(t *testing.T) {
	m := model{config: &config.Config{Model: "gpt-4o"}}
	a := agent.NewAgent(nil, nil, nil)
	a.RestoreMCPToolNames([]string{"demo_tool"})
	m.agent = a

	m.handleModelCmd([]string{"gpt-4o-mini"})

	if m.agent == nil {
		t.Fatal("expected agent to remain initialized")
	}
	if got := m.agent.MCPToolCount(); got != 1 {
		t.Fatalf("expected MCP provenance to survive model switch, got %d", got)
	}
}

func TestResetSessionAgentCreatesStubAgentWhenClientMissing(t *testing.T) {
	oldDebugAppend := agent.DebugAppend
	agent.DebugAppend = func(string, string) {}
	t.Cleanup(func() { agent.DebugAppend = oldDebugAppend })

	cfg := &config.Config{
		Model: "custom/demo",
		Provider: map[string]interface{}{
			"custom": map[string]interface{}{
				"options": map[string]interface{}{
					"baseURL": "https://example.invalid",
				},
			},
		},
	}

	m := model{config: cfg, workDir: t.TempDir()}
	cmd := m.resetSessionAgent()
	if m.agent == nil {
		t.Fatal("expected resetSessionAgent to install a stub agent when the model has no credentials")
	}
	if m.agent.Client() != nil {
		t.Fatalf("expected stub agent to keep a nil client, got %#v", m.agent.Client())
	}
	msgs, err := m.agent.Step(nil)
	if err != nil {
		t.Fatalf("stub agent Step() error = %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "(no llm client configured)" {
		t.Fatalf("stub agent Step() = %#v, want the no-LLM sentinel message", msgs)
	}
	if cmd == nil {
		t.Fatal("expected resetSessionAgent to return a job-listener command")
	}
}

func TestHandleNewCmdClearsTelemetry(t *testing.T) {
	spend := 0.5
	cleanupCalls := 0
	oldAgent := agent.NewAgent(nil, nil, nil)
	m := model{
		sessionTelemetry: sidebarTelemetry{
			inputTokens:  10,
			outputTokens: 20,
			totalTokens:  30,
			spend:        &spend,
		},
		agent: oldAgent,
		cleanupState: &modelCleanupState{
			onCleanup: func() { cleanupCalls++ },
		},
	}

	m.handleNewCmd(nil)

	if cleanupCalls != 1 {
		t.Fatalf("expected /new to use shared cleanup once, got %d", cleanupCalls)
	}
	select {
	case <-oldAgent.Done():
	default:
		t.Fatal("expected /new to shut down previous agent")
	}
	if m.sessionTelemetry.usedTokens() != 0 || m.sessionTelemetry.spend != nil {
		t.Fatalf("expected telemetry to clear on new session, got %#v", m.sessionTelemetry)
	}
}

func TestHandleNewCmdReplacesSupervisor(t *testing.T) {
	oldAgent := agent.NewAgent(nil, nil, nil)
	oldSupervisor := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: time.Millisecond})
	oldAgent.SetSupervisor(oldSupervisor)
	m := model{
		agent:      oldAgent,
		supervisor: oldSupervisor,
	}

	m.handleNewCmd(nil)

	if m.supervisor == nil {
		t.Fatal("expected /new to install a fresh supervisor")
	}
	if m.supervisor == oldSupervisor {
		t.Fatal("expected /new to replace the previous supervisor")
	}
	if m.agent == nil || m.agent.Supervisor() != m.supervisor {
		t.Fatal("expected new agent to use the fresh supervisor")
	}
	if _, err := oldSupervisor.Register(tool.ProcessRegistration{ID: "late-proc"}); err != tool.ErrProcessSupervisorClosed {
		t.Fatalf("old supervisor Register() error = %v, want %v", err, tool.ErrProcessSupervisorClosed)
	}
}

func TestActivityUpdateFromPreviousAgentIgnored(t *testing.T) {
	oldAgent := agent.NewAgent(nil, nil, nil)
	newAgent := agent.NewAgent(nil, nil, nil)
	m := model{agent: newAgent}

	updated, _ := m.Update(activityUpdateMsg{
		tracker: oldAgent.Activity(),
		snap:    agent.ActivitySnapshot{LLMRunning: true},
	})
	got := updated.(model)

	if got.lastActivity.LLMRunning {
		t.Fatal("expected stale activity update to be ignored")
	}
}

func TestJobCompletionFromPreviousAgentIgnored(t *testing.T) {
	oldAgent := agent.NewAgent(nil, nil, nil)
	newAgent := agent.NewAgent(nil, nil, nil)
	m := model{agent: newAgent}

	updated, _ := m.Update(jobCompletedMsg{
		agent: oldAgent,
		ev:    agent.JobEvent{Kind: "agent", ID: "old", Name: "old", Status: "done", Result: "old result"},
	})
	got := updated.(model)

	if len(got.messages) != 0 {
		t.Fatalf("expected stale job completion to be ignored, got %#v", got.messages)
	}
}

func TestHandleNewCmdResetsSessionScopedState(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	snapshot.Reset()
	tool.SetTodoSession("session-1")
	tool.ResetTodoState()
	if err := os.WriteFile("changed.go", []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Backup("changed.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := (tool.TodoWriteTool{}).Execute(mustJSON(t, map[string]string{"todoText": "- [ ] ship task 4"})); err != nil {
		t.Fatal(err)
	}

	m := model{}
	m.handleNewCmd(nil)

	if got := snapshot.ChangedFiles(); len(got) != 0 {
		t.Fatalf("expected snapshot state to reset, got %v", got)
	}
	if got := tool.TodoState(); got != "" {
		t.Fatalf("expected todo state to reset, got %q", got)
	}
}

func TestHandleNewCmdFirstRequestTitlesSession(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(".ocode/sessions", 0755); err != nil {
		t.Fatal(err)
	}

	m := model{}
	m.handleNewCmd(nil)
	m.messages = append(m.messages, message{role: roleUser, text: "first real request"})
	m.saveSession()

	sess, err := session.Load(m.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Title != "first real request" {
		t.Fatalf("expected first request title, got %q", sess.Title)
	}
	if len(sess.Messages) != 1 || sess.Messages[0].Content != "first real request" {
		t.Fatalf("expected transient new-session notice to stay out of history, got %#v", sess.Messages)
	}
}

func TestCountLoadedMCPToolsIgnoresCustomTools(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	a.AddTools([]tool.Tool{&tool.CustomTool{ToolName: "demo_tool"}})
	if got := a.MCPToolCount(); got != 0 {
		t.Fatalf("expected custom tools not to count as MCP tools, got %d", got)
	}
}

func TestSidebarTelemetryAggregationSumsUsageAndSpend(t *testing.T) {
	spendA := 0.1
	spendB := 0.2
	telemetry := aggregateSidebarTelemetry([]message{
		{raw: &agent.Message{Usage: &agent.TokenUsage{PromptTokens: int64Ptr(10), CompletionTokens: int64Ptr(20), CacheReadTokens: int64Ptr(4)}, Spend: &spendA}},
		{raw: &agent.Message{Usage: &agent.TokenUsage{PromptTokens: int64Ptr(5), CompletionTokens: int64Ptr(15), CacheReadTokens: int64Ptr(3)}, Spend: &spendB}},
	})

	if telemetry.inputTokens != 15 || telemetry.outputTokens != 35 || telemetry.totalTokens != 50 || telemetry.cachedTokens != 7 {
		t.Fatalf("expected summed usage totals, got %#v", telemetry)
	}
	if telemetry.spend == nil || math.Abs(*telemetry.spend-0.3) > 1e-9 {
		t.Fatalf("expected summed spend 0.3, got %#v", telemetry.spend)
	}
}

func TestSaveSessionPersistsSidebarTelemetryForReload(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	spend := 0.035
	m := model{
		sessionID: "session-usage",
		messages:  []message{{role: roleAssistant, text: "hello"}},
		sessionTelemetry: sidebarTelemetry{
			inputTokens:  12,
			outputTokens: 34,
			totalTokens:  46,
			cachedTokens: 9,
			spend:        &spend,
		},
	}

	m.saveSession()

	sess, err := session.Load("session-usage")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	telemetry := telemetryFromSessionMetadata(sess.Metadata)
	if telemetry.inputTokens != 12 || telemetry.outputTokens != 34 || telemetry.totalTokens != 46 || telemetry.cachedTokens != 9 {
		t.Fatalf("expected sidebar telemetry to round-trip, got %#v", telemetry)
	}
	if telemetry.spend == nil || math.Abs(*telemetry.spend-0.035) > 1e-9 {
		t.Fatalf("expected spend to round-trip, got %#v", telemetry.spend)
	}
}

func TestHandleSessionLoadRestoresSidebarUsageAndTodoState(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	snapshot.Reset()
	defer snapshot.Reset()
	tool.ResetTodoState()
	defer tool.ResetTodoState()

	meta := map[string]any{
		"input_tokens":  int64(12),
		"output_tokens": int64(34),
		"billed_tokens": int64(46),
		"cached_tokens": int64(9),
		"spend":         0.035,
		"todo_text":     "- [ ] restore the sidebar usage block",
	}
	if err := session.Save("session-usage", "Saved Usage", []agent.Message{{Role: "assistant", Content: "hello"}}, meta); err != nil {
		t.Fatalf("save session: %v", err)
	}

	currentSpend := 0.01
	m := model{
		sessionID:    "current-session",
		sessionTitle: "Current Session",
		width:        140,
		height:       40,
		showSidebar:  true,
		input:        textarea.New(),
		viewport:     viewport.New(viewport.WithWidth(100), viewport.WithHeight(20)),
		config:       &config.Config{Model: "gpt-4o"},
		messages:     []message{{role: roleAssistant, text: "current reply"}},
		sessionTelemetry: sidebarTelemetry{
			inputTokens:  1,
			outputTokens: 2,
			totalTokens:  3,
			spend:        &currentSpend,
		},
	}
	tool.SetTodoSession(m.sessionID)
	tool.SetTodoState("- [ ] current todo")

	m.handleSessionCmd([]string{"load", "session-usage"})

	if m.sessionID != "session-usage" {
		t.Fatalf("expected session id to switch, got %q", m.sessionID)
	}
	if m.sessionTitle != "Saved Usage" {
		t.Fatalf("expected session title to restore, got %q", m.sessionTitle)
	}
	if !m.titleRequested {
		t.Fatal("expected restored titled session to mark titleRequested")
	}
	if tool.TodoState() != "- [ ] restore the sidebar usage block" {
		t.Fatalf("expected todo state to restore, got %q", tool.TodoState())
	}
	if m.sessionTelemetry.inputTokens != 12 || m.sessionTelemetry.outputTokens != 34 || m.sessionTelemetry.totalTokens != 46 || m.sessionTelemetry.cachedTokens != 9 {
		t.Fatalf("expected restored sidebar telemetry, got %#v", m.sessionTelemetry)
	}
	if m.sessionTelemetry.spend == nil || math.Abs(*m.sessionTelemetry.spend-0.035) > 1e-9 {
		t.Fatalf("expected restored spend 0.035, got %#v", m.sessionTelemetry.spend)
	}
	if len(m.messages) < 2 {
		t.Fatalf("expected restored transcript and load confirmation, got %#v", m.messages)
	}
	if m.messages[0].text != "hello" {
		t.Fatalf("expected restored transcript message, got %q", m.messages[0].text)
	}
	if got := m.messages[len(m.messages)-1].text; got != "Loaded session session-usage" {
		t.Fatalf("expected load confirmation, got %q", got)
	}

	got := strings.Join(m.buildSidebarRenderData().bottomLines, "\n")
	for _, want := range []string{"In 12  Cache 9  Out 34", "$0.0350"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected restored sidebar usage block to include %q, got %q", want, got)
		}
	}
}

func TestSessionRestoreScrollsToBottom(t *testing.T) {
	m := model{
		restoredPendingScroll: true,
		messages: []message{
			{role: roleUser, text: "hello"},
			{role: roleAssistant, text: "world"},
		},
	}
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	m.input = textarea.New()
	m.files = newFilesModel(".")
	m.git = newGitModel(".")
	m.width = 100
	m.height = 30

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	result := updated.(model)

	if result.restoredPendingScroll {
		t.Error("restoredPendingScroll should be false after first WindowSizeMsg")
	}
	if !result.viewport.AtBottom() {
		t.Error("viewport should be at bottom after session restore")
	}
}

func TestRerenderTranscriptAutoScrollsNearBottom(t *testing.T) {
	m := model{
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(6)),
		styles:   ApplyThemeColors("tokyonight"),
		messages: []message{{role: roleAssistant, text: strings.Repeat("line\n", 60)}},
	}
	m.renderTranscript()

	maxOffset := m.viewport.TotalLineCount() - m.viewport.VisibleLineCount()
	m.viewport.SetYOffset((maxOffset*9 + 9) / 10)

	m.messages = append(m.messages, message{role: roleAssistant, text: "new line"})
	m.rerenderTranscriptAndMaybeScroll()

	if !m.viewport.AtBottom() {
		t.Fatalf("expected transcript to auto-scroll when already within 90%% of bottom; offset=%d max=%d", m.viewport.YOffset(), m.viewport.TotalLineCount()-m.viewport.VisibleLineCount())
	}
}

func TestRerenderTranscriptDoesNotAutoScrollAwayFromBottom(t *testing.T) {
	m := model{
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(6)),
		styles:   ApplyThemeColors("tokyonight"),
		messages: []message{{role: roleAssistant, text: strings.Repeat("line\n", 60)}},
	}
	m.renderTranscript()

	maxOffset := m.viewport.TotalLineCount() - m.viewport.VisibleLineCount()
	before := int(float64(maxOffset) * 0.89)
	m.viewport.SetYOffset(before)

	m.messages = append(m.messages, message{role: roleAssistant, text: "new line"})
	m.rerenderTranscriptAndMaybeScroll()

	if m.viewport.AtBottom() {
		t.Fatalf("expected transcript not to auto-scroll when above 90%% threshold; offset=%d max=%d", m.viewport.YOffset(), m.viewport.TotalLineCount()-m.viewport.VisibleLineCount())
	}
	if got := m.viewport.YOffset(); got != before {
		t.Fatalf("expected transcript offset to stay at %d when above threshold, got %d", before, got)
	}
}

func TestEscClearsFilesHighlightFirst(t *testing.T) {
	m := model{activeTab: tabFiles}
	m.filesSel = selectionState{active: true, startLine: 0, endLine: 1}
	m.files.selectedFiles = map[int]bool{0: true}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := derefTestModel(t, updated)

	if got.filesSel.active {
		t.Fatal("expected files selection highlight to clear first")
	}
	if len(got.files.selectedFiles) == 0 {
		t.Fatal("expected files multi-select to survive first esc")
	}
}

func TestEscClearsFilesSelectedFilesSecond(t *testing.T) {
	m := model{activeTab: tabFiles}
	m.files.selectedFiles = map[int]bool{0: true}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := derefTestModel(t, updated)

	if len(got.files.selectedFiles) != 0 {
		t.Fatalf("expected files multi-select to clear on esc, got %#v", got.files.selectedFiles)
	}
}

func TestEscClearsGitHighlightFirst(t *testing.T) {
	m := model{activeTab: tabGit}
	m.gitSel = selectionState{active: true, startLine: 0, endLine: 1}
	m.git.selectedFiles = map[int]bool{0: true}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := derefTestModel(t, updated)

	if got.gitSel.active {
		t.Fatal("expected git selection highlight to clear first")
	}
	if len(got.git.selectedFiles) == 0 {
		t.Fatal("expected git multi-select to survive first esc")
	}
}

func TestEscClearsGitSelectedFilesSecond(t *testing.T) {
	m := model{activeTab: tabGit}
	m.git.selectedFiles = map[int]bool{0: true}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := derefTestModel(t, updated)

	if len(got.git.selectedFiles) != 0 {
		t.Fatalf("expected git multi-select to clear on esc, got %#v", got.git.selectedFiles)
	}
}

func TestBuildSelectionContextEmpty(t *testing.T) {
	if got := (model{}).buildSelectionContext(); got != "" {
		t.Fatalf("expected empty context, got %q", got)
	}
}

func TestBuildSelectionContextFilesOnly(t *testing.T) {
	m := model{workDir: "/proj"}
	m.files.nodes = []fileNode{{path: "/proj/main.go", name: "main.go"}, {path: "/proj/foo.go", name: "foo.go"}}
	m.files.selectedFiles = map[int]bool{0: true, 1: true}

	got := m.buildSelectionContext()
	for _, want := range []string{"[Selected context]", "## Files", "main.go", "foo.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in context, got:\n%s", want, got)
		}
	}
}

func TestBuildSelectionContextFilesHighlight(t *testing.T) {
	m := model{workDir: "/proj"}
	m.files.previewPath = "/proj/main.go"
	m.files.previewRawLines = []string{"package main", "func main() {}", "}"}
	m.filesSel = selectionState{active: true, startLine: 0, startCol: 0, endLine: 1, endCol: 99}

	got := m.buildSelectionContext()
	for _, want := range []string{"main.go", "1:", "2:", "package main", "func main() {}"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in context, got:\n%s", want, got)
		}
	}
}

func TestBuildSelectionContextGitFiles(t *testing.T) {
	m := model{workDir: "/proj"}
	m.git.section = gitSectionChanges
	m.git.stagedFiles = []gitFile{{path: "internal/foo.go", status: "M", staged: true}}
	m.git.unstagedFiles = []gitFile{{path: "internal/bar.go", status: "M"}}
	m.git.filesCursor = 0

	// Only index 1 (bar.go) selected — foo.go must not appear.
	m.git.selectedFiles = map[int]bool{1: true}
	got := m.buildSelectionContext()
	if !strings.Contains(got, "## Git diff") {
		t.Fatalf("expected ## Git diff section, got:\n%s", got)
	}
	if !strings.Contains(got, "internal/bar.go") {
		t.Fatalf("expected internal/bar.go in context, got:\n%s", got)
	}
	if strings.Contains(got, "internal/foo.go") {
		t.Fatalf("expected internal/foo.go to be excluded (not selected), got:\n%s", got)
	}

	// No selection and not on git tab — git section must not appear.
	m.git.selectedFiles = nil
	m.activeTab = tabFiles
	got = m.buildSelectionContext()
	if strings.Contains(got, "## Git diff") {
		t.Fatalf("expected no git section when nothing selected and not on git tab, got:\n%s", got)
	}
}

func TestBuildSelectionSidebarBodyFilesAndLineSelection(t *testing.T) {
	m := model{workDir: "/proj"}
	m.files.nodes = []fileNode{{path: "/proj/main.go", name: "main.go"}, {path: "/proj/internal/foo.go", name: "foo.go"}}
	m.files.selectedFiles = map[int]bool{0: true, 1: true}
	m.files.previewPath = "/proj/internal/foo.go"
	m.files.previewRawLines = []string{"one", "two", "three"}
	m.filesSel = selectionState{active: true, startLine: 0, startCol: 0, endLine: 1, endCol: 9}

	got := strings.Join(m.buildSelectionSidebarBody(32), "\n")
	for _, want := range []string{"• main.go", "foo.go", "↳", ":1-2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in sidebar body, got:\n%s", want, got)
		}
	}
}

func TestBuildSidebarRenderDataIncludesSelectionSection(t *testing.T) {
	m := model{workDir: "/proj"}
	m.files.nodes = []fileNode{{path: "/proj/main.go", name: "main.go"}}
	m.files.selectedFiles = map[int]bool{0: true}

	got := strings.Join(m.buildSidebarRenderData().scrollLines, "\n")
	for _, want := range []string{"Selection", "main.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in sidebar render data, got:\n%s", want, got)
		}
	}
}

func TestPrepareAgentMessagesSkipsWhenNoAgent(t *testing.T) {
	m := model{}
	msgs := []agent.Message{{Role: "user", Content: "hello"}}
	got := m.prepareAgentMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected no new messages, got %d", len(got))
	}
}

func TestPrepareAgentMessagesIncludesSelectionContext(t *testing.T) {
	m := model{workDir: "/proj", agent: agent.NewAgent(retryTestClient{}, nil, nil)}
	m.files.nodes = []fileNode{{path: "/proj/main.go", name: "main.go"}}
	m.files.selectedFiles = map[int]bool{0: true}
	msgs := []agent.Message{{Role: "user", Content: "hello"}}

	got := m.prepareAgentMessages(msgs)
	var selection string
	for _, msg := range got {
		if strings.Contains(msg.Content, "[ocode:selection]") {
			selection = msg.Content
			break
		}
	}
	if !strings.Contains(selection, "## Files") || !strings.Contains(selection, "main.go") {
		t.Fatalf("expected selection context in prepared messages, got:\n%v", got)
	}
}

func int64Ptr(v int64) *int64 { return &v }

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestThinkingLevelIndexForBudget(t *testing.T) {
	cases := []struct {
		budget int
		want   int
	}{
		{budget: 0, want: 0},
		{budget: 1024, want: 1},
		{budget: 8000, want: 2},
		{budget: 16000, want: 3},
		{budget: 999, want: 0},
	}
	for _, tc := range cases {
		if got := thinkingLevelIndexForBudget(tc.budget); got != tc.want {
			t.Fatalf("budget %d: want %d, got %d", tc.budget, tc.want, got)
		}
	}
}

func TestTruncateTitle(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "no truncation needed",
			input:  "short",
			maxLen: 80,
			want:   "short",
		},
		{
			name:   "exact length",
			input:  "exactly eighty characters long" + strings.Repeat("x", 50),
			maxLen: 80,
			want:   "exactly eighty characters long" + strings.Repeat("x", 50),
		},
		{
			name:   "truncate with ellipsis",
			input:  "this is a very long title that needs to be truncated because it exceeds the maximum length",
			maxLen: 40,
			want:   "this is a very long title that needs ...",
		},
		{
			name:   "multibyte UTF-8 emoji safe",
			input:  "Hello 🎉🎊🎈 " + strings.Repeat("world ", 10),
			maxLen: 30,
			want:   "Hello 🎉🎊🎈 world world world...",
		},
		{
			name:   "multibyte UTF-8 CJK safe",
			input:  "你好世界你好世界你好世界你好世界你好世界你好世界你好世界",
			maxLen: 10,
			want:   "你好世界你好世...",
		},
		{
			name:   "accented characters safe",
			input:  "café résumé naïve façade " + strings.Repeat("x", 50),
			maxLen: 30,
			want:   "café résumé naïve façade xx...",
		},
		{
			name:   "maxLen 4 edge case",
			input:  "toolong",
			maxLen: 4,
			want:   "t...",
		},
	}

	for _, tc := range cases {
		got := truncateTitle(tc.input, tc.maxLen)
		if got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHandleTitleCmdExplicit(t *testing.T) {
	m := model{
		messages: []message{},
	}

	_ = m.handleTitleCmd([]string{"My", "Session", "Title"})

	if m.sessionTitle != "My Session Title" {
		t.Errorf("expected sessionTitle=%q, got %q", "My Session Title", m.sessionTitle)
	}
	if len(m.messages) == 0 {
		t.Fatal("expected confirmation message")
	}
	if !strings.Contains(m.messages[0].text, "Session title set to") {
		t.Errorf("unexpected message: %q", m.messages[0].text)
	}
}

func TestHandleTitleCmdExplicitTruncated(t *testing.T) {
	m := model{
		messages: []message{},
	}

	longTitle := strings.Repeat("x", 100)
	_ = m.handleTitleCmd([]string{longTitle})

	if len(m.sessionTitle) > maxExplicitTitleLen {
		t.Errorf("title exceeds max length: %d > %d", len(m.sessionTitle), maxExplicitTitleLen)
	}
	if !strings.HasSuffix(m.sessionTitle, "...") {
		t.Errorf("expected truncated title to end with ellipsis, got %q", m.sessionTitle)
	}
}

func TestHandleTitleCmdWhitespaceOnly(t *testing.T) {
	m := model{
		messages: []message{},
	}

	_ = m.handleTitleCmd([]string{"   ", "\t", "\n"})

	if len(m.messages) == 0 {
		t.Fatal("expected usage message")
	}
	if !strings.Contains(m.messages[0].text, "Usage:") {
		t.Errorf("expected usage message, got %q", m.messages[0].text)
	}
}

func TestHandleTitleCmdNoArgClearsTitle(t *testing.T) {
	m := model{
		messages:     []message{},
		sessionTitle: "old title",
	}

	_ = m.handleTitleCmd([]string{})

	if m.sessionTitle != "" {
		t.Errorf("expected sessionTitle to be cleared, got %q", m.sessionTitle)
	}
	if len(m.messages) == 0 {
		t.Fatal("expected auto-generate message")
	}
	if !strings.Contains(m.messages[0].text, "auto-generate") {
		t.Errorf("unexpected message: %q", m.messages[0].text)
	}
}

func TestHandleTitleCmdUTF8Safe(t *testing.T) {
	m := model{
		messages: []message{},
	}

	titleWithEmoji := "Test 🎉 Session 🚀 Title"
	_ = m.handleTitleCmd([]string{titleWithEmoji})

	if m.sessionTitle != titleWithEmoji {
		t.Errorf("expected %q, got %q", titleWithEmoji, m.sessionTitle)
	}
}

func TestCurrentContextEstimateExcludesNextInput(t *testing.T) {
	m := model{
		messages: []message{
			{role: roleUser, text: "hi", raw: &agent.Message{Role: "user", Content: "hi"}},
			{role: roleAssistant, text: "hello", raw: &agent.Message{
				Role:    "assistant",
				Content: "hello",
				Usage:   &agent.TokenUsage{PromptTokens: int64Ptr(10), CompletionTokens: int64Ptr(5), TotalTokens: int64Ptr(15)},
			}},
		},
	}
	tokens, source := m.currentContextEstimate()
	if tokens != 15 {
		t.Errorf("expected 15, got %d", tokens)
	}
	if source != "actual" {
		t.Errorf("expected source actual, got %s", source)
	}
}

func TestSidebarContextUsesCurrentEstimateNotCumulativeTotal(t *testing.T) {
	spend := 0.1
	m := model{
		ready:       true,
		width:       140,
		height:      40,
		showSidebar: true,
		sessionID:   "session-ctx",
		input:       textarea.New(),
		viewport:    viewport.New(viewport.WithWidth(100), viewport.WithHeight(20)),
		config:      &config.Config{Model: "gpt-4o"},
		messages: []message{
			{
				role: roleAssistant,
				text: "first",
				raw: &agent.Message{
					Role:  "assistant",
					Usage: &agent.TokenUsage{PromptTokens: int64Ptr(1000), CompletionTokens: int64Ptr(100)},
					Spend: &spend,
				},
			},
			{
				role: roleAssistant,
				text: "second",
				raw: &agent.Message{
					Role:  "assistant",
					Usage: &agent.TokenUsage{PromptTokens: int64Ptr(2000), CompletionTokens: int64Ptr(200)},
				},
			},
		},
	}
	view := m.View().Content
	// The context line should show ~2.2k, not the cumulative 3.3k
	if strings.Contains(view, "3.3k") || strings.Contains(view, "3300") {
		t.Fatalf("sidebar context should not show cumulative total 3.3k, got view:\n%s", view)
	}
	if !strings.Contains(view, "2.2k") && !strings.Contains(view, "2200") {
		t.Fatalf("expected sidebar to show ~2.2k context, got view:\n%s", view)
	}
}

func TestHandleAdvisorCmdRequiresProviderPrefix(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	m := model{config: &config.Config{}}
	m.config.Ocode.Advisor = config.DefaultAdvisorConfig()
	m.handleAdvisorCmd([]string{"claude-sonnet-4-6"})

	if len(m.messages) == 0 {
		t.Fatal("expected a validation message")
	}
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "provider/model") {
		t.Fatalf("expected provider/model validation message, got %q", last)
	}
	if m.config.Ocode.Advisor.Provider != config.DefaultAdvisorProvider() || m.config.Ocode.Advisor.Model != config.DefaultAdvisorModelName() {
		t.Fatalf("advisor config should remain unchanged on invalid input, got %#v", m.config.Ocode.Advisor)
	}
}

func TestRunPermissionsCmdModelOpensPermissionPicker(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := config.Config{}
	m := model{
		config: &cfg,
		agent:  agent.NewAgent(retryTestClient{}, nil, &cfg),
		input:  textarea.New(),
	}

	runPermissionsCmd(&m, []string{"model"})

	if !m.showPicker {
		t.Fatal("expected permission model picker to open")
	}
	if m.pickerKind != "permission-model" {
		t.Fatalf("expected permission-model picker kind, got %q", m.pickerKind)
	}
	if len(m.pickerItems) == 0 || m.pickerItems[0] != "(not set)" {
		t.Fatalf("expected a clear option at the top of the picker, got %#v", m.pickerItems[:minInt(len(m.pickerItems), 3)])
	}
	if len(m.pickerValues) == 0 || m.pickerValues[0] != "auto" {
		t.Fatalf("expected clear option to map to auto, got %#v", m.pickerValues[:minInt(len(m.pickerValues), 3)])
	}
}

func TestRunPermissionsCmdModelAutoClearsOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "anthropic/claude-sonnet-4-6"}
	m := model{
		config: &cfg,
		agent:  agent.NewAgent(retryTestClient{}, nil, &cfg),
		input:  textarea.New(),
	}

	runPermissionsCmd(&m, []string{"model", "auto"})

	if m.config.Ocode.Permissions.Auto == nil {
		t.Fatal("expected auto permission config to remain present")
	}
	if m.config.Ocode.Permissions.Auto.Model != "" {
		t.Fatalf("expected permission model override to clear, got %q", m.config.Ocode.Permissions.Auto.Model)
	}
	if len(m.messages) == 0 || !strings.Contains(m.messages[len(m.messages)-1].text, "Permission model cleared") {
		t.Fatalf("expected clear confirmation message, got %#v", m.messages)
	}
}
func TestRunPermissionsCmdModelRequiresProviderSlashModel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: false}
	m := model{
		config: &cfg,
		agent:  agent.NewAgent(retryTestClient{}, nil, &cfg),
		input:  textarea.New(),
	}

	// Bare model name without provider prefix should be rejected.
	runPermissionsCmd(&m, []string{"model", "claude-sonnet-4-6"})

	if len(m.messages) == 0 {
		t.Fatal("expected a validation message")
	}
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "provider/model") {
		t.Fatalf("expected provider/model validation message, got %q", last)
	}
	// Config should remain unchanged.
	if m.config.Ocode.Permissions.Auto != nil && m.config.Ocode.Permissions.Auto.Model != "" {
		t.Fatalf("permission model should remain unchanged on invalid input, got %q", m.config.Ocode.Permissions.Auto.Model)
	}
}

func TestRunPermissionsCmdModelRejectsProviderOnly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: false}
	m := model{
		config: &cfg,
		agent:  agent.NewAgent(retryTestClient{}, nil, &cfg),
		input:  textarea.New(),
	}

	// Provider with trailing slash but no model name should be rejected.
	runPermissionsCmd(&m, []string{"model", "anthropic/"})

	if len(m.messages) == 0 {
		t.Fatal("expected a validation message")
	}
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "provider/model") {
		t.Fatalf("expected provider/model validation message, got %q", last)
	}
}

func TestRunPermissionsCmdModelRejectsSlashOnly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: false}
	m := model{
		config: &cfg,
		agent:  agent.NewAgent(retryTestClient{}, nil, &cfg),
		input:  textarea.New(),
	}

	// Just a slash should be rejected.
	runPermissionsCmd(&m, []string{"model", "/"})

	if len(m.messages) == 0 {
		t.Fatal("expected a validation message")
	}
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "provider/model") {
		t.Fatalf("expected provider/model validation message, got %q", last)
	}
}

func TestRecapFinishedMsgIgnoresStaleGeneration(t *testing.T) {
	m := model{recapGen: 2, recapText: "current recap"}

	updated, _ := m.Update(recapFinishedMsg{gen: 1, text: "stale recap"})
	got := derefTestModel(t, updated)
	if got.recapText != "current recap" {
		t.Fatalf("stale recap should be ignored, got %q", got.recapText)
	}

	updated, _ = got.Update(recapFinishedMsg{gen: 2, text: "fresh recap"})
	got = derefTestModel(t, updated)
	if got.recapText != "fresh recap" {
		t.Fatalf("matching recap generation should update text, got %q", got.recapText)
	}
}
