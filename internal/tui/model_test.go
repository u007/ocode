package tui

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

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

func TestSidebarToggleWithCtrlB(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(80, 20)}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	got := updated.(model)
	if !got.showSidebar {
		t.Fatal("expected Ctrl+B to toggle sidebar on")
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	got = updated.(model)
	if got.showSidebar {
		t.Fatal("expected Ctrl+B to toggle sidebar off")
	}
}

func TestSidebarViewUsesSplitLayoutWhenWide(t *testing.T) {
	spend := 0.1234
	m := model{
		ready:       true,
		width:       140,
		height:      40,
		showSidebar: true,
		sessionID:   "session-123",
		input:       textarea.New(),
		viewport:    viewport.New(100, 20),
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

	view := m.View()
	for _, want := range []string{"Session", "session-123", "Model", "gpt-4o", "Context", "Spend", "MCP", "LSP", "TODO", "Ctrl+B"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected wide view to include %q, got %q", want, view)
		}
	}
}

func TestSidebarViewHidesOnNarrowTerminals(t *testing.T) {
	m := model{
		ready:       true,
		width:       80,
		height:      30,
		showSidebar: true,
		input:       textarea.New(),
		viewport:    viewport.New(76, 20),
	}

	view := m.View()
	if strings.Contains(view, "No live session todo state yet.") || strings.Contains(view, "Ctrl+B toggle sidebar") {
		t.Fatalf("expected narrow view to hide sidebar, got %q", view)
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
		ready:       true,
		width:       140,
		height:      40,
		showSidebar: true,
		input:       textarea.New(),
		viewport:    viewport.New(100, 20),
	}

	view := m.View()
	for _, want := range []string{"Files", "changed.go", "TODO", "- [ ] ship task 4"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected sidebar to include %q, got %q", want, view)
		}
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

	m := model{ready: true, width: 140, height: 40, showSidebar: true, input: textarea.New(), viewport: viewport.New(100, 20)}
	updated, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 120, Y: 20})
	_ = updated
	if cmd == nil {
		t.Fatal("expected sidebar file click to return editor command")
	}
	cmd()

	if gotPath != "changed.go" {
		t.Fatalf("expected clicked file to open, got %q", gotPath)
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
	m := model{config: &config.Config{Model: "gpt-4o"}}
	m.handleModelCmd([]string{"gpt-4o-mini"})

	if got := m.currentModelName(); got != "gpt-4o-mini" {
		t.Fatalf("expected active model to update, got %q", got)
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

func TestHandleNewCmdClearsTelemetry(t *testing.T) {
	spend := 0.5
	m := model{
		sessionTelemetry: sidebarTelemetry{
			promptTokens:     10,
			completionTokens: 20,
			totalTokens:      30,
			spend:            &spend,
		},
	}

	m.handleNewCmd(nil)

	if m.sessionTelemetry.usedTokens() != 0 || m.sessionTelemetry.spend != nil {
		t.Fatalf("expected telemetry to clear on new session, got %#v", m.sessionTelemetry)
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
		{raw: &agent.Message{Usage: &agent.TokenUsage{PromptTokens: int64Ptr(10), CompletionTokens: int64Ptr(20)}, Spend: &spendA}},
		{raw: &agent.Message{Usage: &agent.TokenUsage{PromptTokens: int64Ptr(5), CompletionTokens: int64Ptr(15)}, Spend: &spendB}},
	})

	if telemetry.promptTokens != 15 || telemetry.completionTokens != 35 || telemetry.totalTokens != 50 {
		t.Fatalf("expected summed usage totals, got %#v", telemetry)
	}
	if telemetry.spend == nil || math.Abs(*telemetry.spend-0.3) > 1e-9 {
		t.Fatalf("expected summed spend 0.3, got %#v", telemetry.spend)
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
