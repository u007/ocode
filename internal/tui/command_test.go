package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/redact"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tui/fastviewport"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/config"
)

func TestLookupCommandResolvesAliases(t *testing.T) {
	if got := lookupCommand("/model"); got == nil || got.name != "/models" {
		t.Fatalf("expected /model to resolve to /models, got %#v", got)
	}

	if got := lookupCommand("/clear"); got == nil || got.name != "/new" {
		t.Fatalf("expected /clear to resolve to /new, got %#v", got)
	}

	if got := lookupCommand("/q"); got == nil || got.name != "/exit" {
		t.Fatalf("expected /q to resolve to /exit, got %#v", got)
	}

	if got := lookupCommand("/quit"); got == nil || got.name != "/exit" {
		t.Fatalf("expected /quit to resolve to /exit, got %#v", got)
	}

	if got := lookupCommand("/resume"); got == nil || got.name != "/session" {
		t.Fatalf("expected /resume to resolve to /session, got %#v", got)
	}

	if got := lookupCommand("/sessions"); got == nil || got.name != "/session" {
		t.Fatalf("expected /sessions to resolve to /session, got %#v", got)
	}

	if got := lookupCommand("/theme"); got == nil || got.name != "/themes" {
		t.Fatalf("expected /theme to resolve to /themes, got %#v", got)
	}

	if got := lookupCommand("/export-claude"); got == nil || got.name != "/export-claude" {
		t.Fatalf("expected /export-claude to resolve to itself, got %#v", got)
	}
}

func TestRenderFileSearchUsesWorkspaceFiles(t *testing.T) {
	m := model{width: 80, fileSearchResults: []fileSearchResult{
		{path: "main.go", dirName: ".", fileName: "main.go"},
		{path: "internal/tui/model.go", dirName: "tui", fileName: "model.go"},
	}}
	m.fileSearchIndex = 0
	got := m.renderFileSearch()

	for _, want := range []string{"main.go", "model.go", "Search files"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected file search to include %s, got %q", want, got)
		}
	}
}

// Enter opens the file in the configured editor; Ctrl+E opens it with the
// cross-platform system opener. Both return a command and close the search.
func TestEnterAndCtrlEInFileSearchOpenFile(t *testing.T) {
	for _, tc := range []struct {
		name           string
		msg            tea.KeyPressMsg
		wantInputValue string
		wantCmd        bool
	}{
		{name: "ctrl+e", msg: tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl, Text: ""}, wantCmd: true},
		{name: "enter", msg: tea.KeyPressMsg{Code: tea.KeyEnter}, wantCmd: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := model{width: 80, input: newTestTextarea(), showFileSearch: true, fileSearchResults: []fileSearchResult{{path: "main.go", fileName: "main.go"}}}
			m.fileSearchIndex = 0

			updated, cmd := m.Update(tc.msg)
			got := updated.(model)
			if got.showFileSearch {
				t.Fatal("expected file search to close after " + tc.name)
			}
			if tc.wantInputValue != "" && got.input.Value() != tc.wantInputValue {
				t.Fatalf("expected input %q after %s, got %q", tc.wantInputValue, tc.name, got.input.Value())
			}
			if tc.wantInputValue == "" && got.input.Value() != "" {
				t.Fatalf("expected input to stay empty after %s, got %q", tc.name, got.input.Value())
			}
			if (cmd != nil) != tc.wantCmd {
				t.Fatalf("expected cmd presence=%v after %s, got %v", tc.wantCmd, tc.name, cmd != nil)
			}
		})
	}
}

func TestSlashAutocompleteResolvesCommand(t *testing.T) {
	m := model{}

	if got := autocompleteSlashInput(&m, "/m"); len(got) == 0 || got[0] != "/models" {
		t.Fatalf("expected /m to resolve to /models, got %#v", got)
	}

	got := autocompleteSlashInput(&m, "/models ")
	if len(got) == 0 {
		t.Fatalf("expected /models autocomplete to return at least one model, got empty")
	}
}

func TestTabAutocompleteRunsInUpdatePath(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}
	m.input.SetValue("/m")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if got.input.Value() != "/models" {
		t.Fatalf("expected tab to complete /m to /models, got %q", got.input.Value())
	}

	if got.showFileSearch {
		t.Fatal("expected tab autocomplete to operate on the main input, not file search")
	}
}

func TestTabOnModelOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}
	m.input.SetValue("/models ")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if !got.showPicker {
		t.Fatal("expected tab on /models to open picker")
	}
}

func TestThemeCommandOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}

	updated, cmd := m.handleCommand("/theme")
	if cmd != nil {
		t.Fatalf("expected /theme to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if len(got.messages) != 1 {
		t.Fatalf("expected /theme to be recorded in transcript, got %d messages", len(got.messages))
	}
	if got.messages[0].role != roleUser || got.messages[0].text != "/theme" {
		t.Fatalf("expected first transcript message to be /theme, got %#v", got.messages[0])
	}
	if !got.showPicker || got.pickerKind != "theme" {
		t.Fatalf("expected /theme to open theme picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
	if len(got.pickerItems) == 0 {
		t.Fatal("expected theme picker to include themes")
	}
}

func TestTabOnThemeOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}
	m.input.SetValue("/theme ")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if !got.showPicker || got.pickerKind != "theme" {
		t.Fatalf("expected tab on /theme to open theme picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
}

func TestThemePickerSelectionSwitchesTheme(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	themes := AvailableThemes()
	if len(themes) == 0 {
		t.Fatal("expected built-in themes")
	}
	m := model{
		input:        textarea.New(),
		viewport:     fastviewport.New(80, 20),
		config:       &config.Config{Ocode: config.OcodeConfig{}},
		showPicker:   true,
		pickerKind:   "theme",
		pickerItems:  themes,
		pickerValues: themes,
	}

	updated, cmd := m.selectPickerIndex(0)
	if cmd != nil {
		t.Fatalf("expected theme picker selection to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if got.showPicker {
		t.Fatal("expected theme picker to close after selection")
	}
	if got.config.Ocode.TUI.Theme != themes[0] {
		t.Fatalf("expected selected theme %q, got %q", themes[0], got.config.Ocode.TUI.Theme)
	}
}

func TestSidebarCommandRecordsTranscriptMessage(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}

	updated, cmd := m.handleCommand("/sidebar")
	if cmd != nil {
		t.Fatalf("expected /sidebar to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if !got.showSidebar {
		t.Fatal("expected /sidebar to toggle sidebar state on")
	}

	if len(got.messages) != 1 {
		t.Fatalf("expected /sidebar to be recorded in transcript, got %d messages", len(got.messages))
	}
	if got.messages[0].role != roleUser || got.messages[0].text != "/sidebar" {
		t.Fatalf("expected transcript to include /sidebar, got %#v", got.messages[0])
	}

	updated, cmd = got.handleCommand("/sidebar")
	if cmd != nil {
		t.Fatalf("expected /sidebar toggle off to return no command, got %T", cmd)
	}

	got = updated.(*model)
	if got.showSidebar {
		t.Fatal("expected /sidebar to toggle sidebar state off")
	}
	if len(got.messages) != 2 {
		t.Fatalf("expected second /sidebar run to add another transcript entry, got %d messages", len(got.messages))
	}
	if got.messages[1].role != roleUser || got.messages[1].text != "/sidebar" {
		t.Fatalf("expected second transcript entry to include /sidebar, got %#v", got.messages[1])
	}
}

func TestDrainQueuedCommandsProcessesAllSynchronousCommands(t *testing.T) {
	m := model{
		width:          80,
		height:         20,
		input:          textarea.New(),
		viewport:       fastviewport.New(80, 20),
		queuedCommands: []string{"/theme", "/sidebar"},
	}

	cmd, drained := m.drainQueuedCommands()
	if cmd != nil {
		t.Fatalf("expected synchronous queued commands to return no command, got %T", cmd)
	}
	if !drained {
		t.Fatal("expected queued commands to be drained")
	}
	if len(m.queuedCommands) != 0 {
		t.Fatalf("expected queue to be empty after drain, got %#v", m.queuedCommands)
	}
	if !m.showPicker || m.pickerKind != "theme" {
		t.Fatalf("expected /theme to open picker, got showPicker=%v kind=%q", m.showPicker, m.pickerKind)
	}
	if !m.showSidebar {
		t.Fatal("expected /sidebar to toggle sidebar after /theme")
	}
	if len(m.messages) != 2 {
		t.Fatalf("expected both commands to be recorded, got %#v", m.messages)
	}
	if m.messages[0].text != "/theme" || m.messages[1].text != "/sidebar" {
		t.Fatalf("expected queued commands in order, got %#v", m.messages)
	}
}

func TestLoginCommandBypassesBusyQueue(t *testing.T) {
	m := model{
		width:     80,
		height:    20,
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		streaming: true,
	}

	updated, cmd := m.handleCommand("/login")
	if cmd == nil {
		t.Fatal("expected /login to run immediately while busy")
	}

	got := updated.(*model)
	if len(got.queuedCommands) != 0 {
		t.Fatalf("expected /login not to be queued, got %#v", got.queuedCommands)
	}
	if len(got.messages) == 0 || got.messages[0].role != roleUser || got.messages[0].text != "/login" {
		t.Fatalf("expected /login to be recorded immediately, got %#v", got.messages)
	}
}

func TestNewCommandBypassesBusyQueue(t *testing.T) {
	m := model{
		width:          80,
		height:         20,
		input:          textarea.New(),
		viewport:       fastviewport.New(80, 20),
		streaming:      true,
		sessionID:      "old-session",
		messages:       []message{{role: roleUser, text: "keep me busy"}},
		queuedInputs:   []string{"pending user input"},
		queuedCommands: []string{"/sidebar"},
	}

	updated, _ := m.handleCommand("/new")
	got := updated.(*model)
	if len(got.queuedCommands) != 0 {
		t.Fatalf("expected /new not to be queued, got %#v", got.queuedCommands)
	}
	if len(got.queuedInputs) != 0 {
		t.Fatalf("expected /new to clear queued inputs, got %#v", got.queuedInputs)
	}
	if got.sessionID == "old-session" {
		t.Fatal("expected /new to start a fresh session immediately")
	}
	if len(got.messages) != 1 || got.messages[0].role != roleAssistant || got.messages[0].text != "Started new session." {
		t.Fatalf("expected /new to reset the transcript immediately, got %#v", got.messages)
	}
}

func TestCompactFinishedResumesAfterQueuedLocalCommands(t *testing.T) {
	m := model{
		width:                80,
		height:               20,
		input:                textarea.New(),
		viewport:             fastviewport.New(80, 20),
		agent:                agent.NewAgent(nil, nil, &config.Config{}, nil),
		messages:             []message{{role: roleUser, text: "hello"}},
		pendingCompactResume: true,
		queuedCommands:       []string{"/sidebar"},
	}

	updated, cmd := m.Update(compactFinishedMsg{result: agent.CompactResult{OK: false}})
	if cmd == nil {
		t.Fatal("expected compact finish to resume the agent after draining queued commands")
	}

	got := derefTestModel(t, updated)
	if len(got.queuedCommands) != 0 {
		t.Fatalf("expected queued commands to be drained, got %#v", got.queuedCommands)
	}
	if !got.showSidebar {
		t.Fatal("expected queued /sidebar command to run before resuming")
	}
}

func TestCommandHelpTextShowsAliasesAndArgs(t *testing.T) {
	help := commandHelpText()
	for _, want := range []string{"/models [name], /model", "/mask [on|off|status|mode [lenient|full]|model [name]|list]", "/session [list|load <id>], /sessions, /resume", "/new, /clear", "/exit, /quit, /q"} {
		if !strings.Contains(help, want) {
			t.Fatalf("expected help text to include %q, got %q", want, help)
		}
	}
}
func TestMaskCommandShowsStatusAndHint(t *testing.T) {
	m := model{
		input:             newTestTextarea(),
		viewport:          fastviewport.New(80, 20),
		activeModel:       "gpt-4o",
		redactionEnabled:  true,
		redactionModel:    "lmstudio/local-scan",
		redactionRegistry: redact.NewRegistry(redact.NewNonce()),
		redactMode:        "lenient",
		showSidebar:       true,
		showThinking:      true,
	}

	updated, cmd := m.handleCommand("/mask")
	if cmd != nil {
		t.Fatalf("expected /mask to return no command, got %T", cmd)
	}

	got := derefTestModel(t, updated)
	if !got.redactionEnabled {
		t.Fatal("expected /mask with no args to leave redaction enabled")
	}
	if len(got.messages) == 0 {
		t.Fatal("expected /mask to append a status message")
	}
	msg := got.messages[len(got.messages)-1].text
	for _, want := range []string{"Secret redaction: enabled", "Scan mode: lenient", "Tier-2 scanner: inactive (model=lmstudio/local-scan, base_url not configured)", "Try: /mask [on|off|status|mode [lenient|full]|model [name]|list]"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected /mask output to include %q, got %q", want, msg)
		}
	}
}

func TestMaskModeShowsCurrentAndSetsNew(t *testing.T) {
	m := model{
		input:             newTestTextarea(),
		viewport:          fastviewport.New(80, 20),
		redactionRegistry: redact.NewRegistry(redact.NewNonce()),
		redactMode:        "lenient",
	}

	// Test: /mask mode (no arg) shows current mode
	updated, cmd := m.handleCommand("/mask mode")
	if cmd != nil {
		t.Fatalf("expected /mask mode to return no command, got %T", cmd)
	}
	got := derefTestModel(t, updated)
	msg := got.messages[len(got.messages)-1].text
	if !strings.Contains(msg, "Current mode: lenient") {
		t.Fatalf("expected mode display, got %q", msg)
	}
	if !strings.Contains(msg, "lenient") || !strings.Contains(msg, "full") {
		t.Fatalf("expected mode descriptions, got %q", msg)
	}
}

func TestMaskModeSetFull(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := model{
		input:             newTestTextarea(),
		viewport:          fastviewport.New(80, 20),
		redactionRegistry: redact.NewRegistry(redact.NewNonce()),
		redactMode:        "lenient",
	}

	// Test: /mask mode full
	updated, cmd := m.handleCommand("/mask mode full")
	if cmd != nil {
		t.Fatalf("expected /mask mode full to return no command, got %T", cmd)
	}
	got := derefTestModel(t, updated)
	if got.redactMode != "full" {
		t.Fatalf("expected redactMode=full, got %q", got.redactMode)
	}
	msg := got.messages[len(got.messages)-1].text
	if !strings.Contains(msg, "Scan mode: full") {
		t.Fatalf("expected scan mode confirmation, got %q", msg)
	}
}

func TestMaskModeInvalid(t *testing.T) {
	m := model{
		input:             newTestTextarea(),
		viewport:          fastviewport.New(80, 20),
		redactionRegistry: redact.NewRegistry(redact.NewNonce()),
		redactMode:        "lenient",
	}

	updated, cmd := m.handleCommand("/mask mode invalid")
	if cmd != nil {
		t.Fatalf("expected /mask mode invalid to return no command, got %T", cmd)
	}
	got := derefTestModel(t, updated)
	msg := got.messages[len(got.messages)-1].text
	if !strings.Contains(msg, "Invalid mode") {
		t.Fatalf("expected invalid mode error, got %q", msg)
	}
}

func TestMaskRuntimeReconfigUpdatesAgentAndScanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{}
	cfg.Ocode.Security.Redaction.Enabled = true
	cfg.Ocode.Security.Redaction.Model = "scan-old"
	cfg.Ocode.Security.Redaction.BaseURL = "http://localhost:11434"

	m := model{
		input:             newTestTextarea(),
		viewport:          fastviewport.New(80, 20),
		config:            cfg,
		agent:             agent.NewAgent(&agent.GenericClient{Provider: "openai", Model: "gpt-4o"}, nil, cfg, nil),
		redactionEnabled:  true,
		redactionModel:    "scan-old",
		redactionRegistry: redact.NewRegistry(redact.NewNonce()),
		llmScanner:        buildLLMScanner("http://localhost:11434", "scan-old", false),
		redactMode:        "lenient",
	}
	m.syncRedactionRuntime()

	updated, cmd := m.handleCommand("/mask off")
	if cmd != nil {
		t.Fatalf("expected /mask off to return no command, got %T", cmd)
	}
	got := derefTestModel(t, updated)
	gc, ok := got.agent.Client().(*agent.GenericClient)
	if !ok {
		t.Fatalf("expected *agent.GenericClient, got %T", got.agent.Client())
	}
	if gc.Redaction != nil {
		t.Fatal("expected redaction hook to be detached after /mask off")
	}

	updated, cmd = got.handleCommand("/mask on")
	if cmd != nil {
		t.Fatalf("expected /mask on to return no command, got %T", cmd)
	}
	got = derefTestModel(t, updated)
	gc, ok = got.agent.Client().(*agent.GenericClient)
	if !ok {
		t.Fatalf("expected *agent.GenericClient, got %T", got.agent.Client())
	}
	if gc.Redaction == nil || !gc.Redaction.Enabled {
		t.Fatal("expected redaction hook to be reattached after /mask on")
	}

	updated, cmd = got.handleCommand("/mask model scan-new")
	if cmd != nil {
		t.Fatalf("expected /mask model to return no command, got %T", cmd)
	}
	got = derefTestModel(t, updated)
	if got.llmScanner == nil {
		t.Fatal("expected llm scanner to be rebuilt after /mask model")
	}
	if got.llmScanner.Model != "scan-new" {
		t.Fatalf("expected llm scanner model scan-new, got %q", got.llmScanner.Model)
	}
	gc, ok = got.agent.Client().(*agent.GenericClient)
	if !ok {
		t.Fatalf("expected *agent.GenericClient, got %T", got.agent.Client())
	}
	if gc.Redaction == nil {
		t.Fatal("expected redaction hook to remain attached after /mask model")
	}
}

func TestMaskModelAutoSetsBaseURLAndRebuildsScanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{}
	cfg.Ocode.Security.Redaction.Enabled = true

	m := model{
		input:             newTestTextarea(),
		viewport:          fastviewport.New(80, 20),
		config:            cfg,
		agent:             agent.NewAgent(&agent.GenericClient{Provider: "openai", Model: "gpt-4o"}, nil, cfg, nil),
		redactionEnabled:  true,
		redactionRegistry: redact.NewRegistry(redact.NewNonce()),
		redactMode:        "lenient",
	}

	updated, cmd := m.handleCommand("/mask model lmstudio/scan-new")
	if cmd != nil {
		t.Fatalf("expected /mask model to return no command, got %T", cmd)
	}
	got := derefTestModel(t, updated)
	if got.config.Ocode.Security.Redaction.BaseURL != "http://localhost:1234/v1" {
		t.Fatalf("expected auto base_url, got %q", got.config.Ocode.Security.Redaction.BaseURL)
	}
	if got.config.Ocode.Security.Redaction.Model != "lmstudio/scan-new" {
		t.Fatalf("expected normalized model in config, got %q", got.config.Ocode.Security.Redaction.Model)
	}
	if got.llmScanner == nil {
		t.Fatal("expected llm scanner to be rebuilt after auto base_url set")
	}
	if got.llmScanner.BaseURL != "http://localhost:1234/v1" {
		t.Fatalf("expected llm scanner base_url http://localhost:1234/v1, got %q", got.llmScanner.BaseURL)
	}
	if got.llmScanner.Model != "scan-new" {
		t.Fatalf("expected llm scanner model scan-new, got %q", got.llmScanner.Model)
	}
}

func TestMaskOnRebuildsScannerWhenInitiallyDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{}
	cfg.Ocode.Security.Redaction.Enabled = false
	cfg.Ocode.Security.Redaction.Model = "scan-old"
	cfg.Ocode.Security.Redaction.BaseURL = "http://localhost:11434"

	m := model{
		input:            newTestTextarea(),
		viewport:         fastviewport.New(80, 20),
		config:           cfg,
		agent:            agent.NewAgent(&agent.GenericClient{Provider: "openai", Model: "gpt-4o"}, nil, cfg, nil),
		redactionEnabled: false,
		redactionModel:   "scan-old",
		redactMode:       "lenient",
	}

	updated, cmd := m.handleCommand("/mask on")
	if cmd != nil {
		t.Fatalf("expected /mask on to return no command, got %T", cmd)
	}
	got := derefTestModel(t, updated)
	if got.llmScanner == nil {
		t.Fatal("expected llm scanner to be rebuilt when enabling redaction")
	}
	if got.llmScanner.Model != "scan-old" {
		t.Fatalf("expected rebuilt scanner to use configured model, got %q", got.llmScanner.Model)
	}
	gc, ok := got.agent.Client().(*agent.GenericClient)
	if !ok {
		t.Fatalf("expected *agent.GenericClient, got %T", got.agent.Client())
	}
	if gc.Redaction == nil || !gc.Redaction.Enabled {
		t.Fatal("expected redaction hook to be attached after /mask on")
	}
}

func TestCdCommandChangesProcessAndSessionWorkspace(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace-a")
	target := filepath.Join(root, "workspace-b")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	m := model{
		input:    newTestTextarea(),
		workDir:  workspace,
		agent:    agent.NewAgent(&agent.GenericClient{Provider: "openai", Model: "gpt-4o"}, nil, &config.Config{}, nil),
		viewport: fastviewport.New(80, 20),
	}

	if cmd := runCdCmd(&m, []string{target}); cmd != nil {
		t.Fatalf("expected /cd to return no command, got %T", cmd)
	}
	if got := m.workDir; got != target {
		t.Fatalf("expected model workDir %q, got %q", target, got)
	}
	wantCwd := target
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		wantCwd = resolved
	}
	if got, err := os.Getwd(); err != nil || got != wantCwd {
		t.Fatalf("expected process cwd %q, got %q (err=%v)", wantCwd, got, err)
	}
	if got := session.ProjectSlug(); got != session.ProjectSlugForPath(wantCwd) {
		t.Fatalf("expected session slug to follow cwd change, got %q want %q", got, session.ProjectSlugForPath(wantCwd))
	}
}

func TestMaskListUsesIndexKindPreviewAndSource(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	reg.GetOrAssign("secret1234", "api_key", "session")
	reg.GetOrAssign("token5678", "token", "tool")

	m := model{
		input:             newTestTextarea(),
		viewport:          fastviewport.New(80, 20),
		redactionRegistry: reg,
	}

	updated, cmd := m.handleCommand("/mask list")
	if cmd != nil {
		t.Fatalf("expected /mask list to return no command, got %T", cmd)
	}

	got := derefTestModel(t, updated)
	if len(got.messages) == 0 {
		t.Fatal("expected /mask list to append a message")
	}
	msg := got.messages[len(got.messages)-1].text
	if !strings.Contains(msg, "Registered secrets (2):") {
		t.Fatalf("expected list header, got %q", msg)
	}
	if !strings.Contains(msg, "1. [api_key] "+redact.MaskedPreview("secret1234")+" (source=session)") {
		t.Fatalf("expected first row to include index, kind, preview, and source, got %q", msg)
	}
	if !strings.Contains(msg, "2. [token] "+redact.MaskedPreview("token5678")+" (source=tool)") {
		t.Fatalf("expected second row to include index, kind, preview, and source, got %q", msg)
	}
}

func TestEditorCommandOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20), files: newFilesModel(".")}

	updated, cmd := m.handleCommand("/editor")
	if cmd != nil {
		t.Fatalf("expected /editor to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if !got.showPicker || got.pickerKind != "editor" {
		t.Fatalf("expected /editor to open editor picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
	if len(got.pickerItems) == 0 {
		t.Fatal("expected editor picker to include editors")
	}
	if got.pickerItems[0] != "nvim" {
		t.Fatalf("expected first picker item to be nvim, got %q", got.pickerItems[0])
	}
}

func TestEditorCommandSetsEditor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := model{
		input:    newTestTextarea(),
		viewport: fastviewport.New(80, 20),
		files:    newFilesModel("."),
		config:   &config.Config{Ocode: config.OcodeConfig{}},
	}

	updated, cmd := m.handleCommand("/editor cat")
	if cmd != nil {
		t.Fatalf("expected /editor cat to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if got.files.editor != "cat" {
		t.Fatalf("expected editor to be set to cat, got %q", got.files.editor)
	}
}

func TestEditorModeCommandOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}

	updated, cmd := m.handleCommand("/editor-mode")
	if cmd != nil {
		t.Fatalf("expected /editor-mode to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if !got.showPicker || got.pickerKind != "editor-mode" {
		t.Fatalf("expected /editor-mode to open editor-mode picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
	if len(got.pickerItems) != 3 {
		t.Fatalf("expected 3 editor mode options, got %d", len(got.pickerItems))
	}
	if got.pickerItems[0] != config.EditorModeExternal {
		t.Fatalf("expected first option to be external, got %q", got.pickerItems[0])
	}
}

func TestEditorModeCommandValidMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := model{
		input:    textarea.New(),
		viewport: fastviewport.New(80, 20),
		config:   &config.Config{Ocode: config.OcodeConfig{}},
		files:    newFilesModel("."),
	}

	updated, cmd := m.handleCommand("/editor-mode external")
	if cmd != nil {
		t.Fatalf("expected /editor-mode external to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if got.config.Ocode.EditorMode != config.EditorModeExternal {
		t.Fatalf("expected editor mode to be set to external, got %q", got.config.Ocode.EditorMode)
	}
}

func TestEditorModeCommandInvalidMode(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}

	updated, cmd := m.handleCommand("/editor-mode bogus")
	if cmd != nil {
		t.Fatalf("expected /editor-mode bogus to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if len(got.messages) != 2 {
		t.Fatalf("expected transcript command plus error message for invalid mode, got %d messages", len(got.messages))
	}
	if got.messages[0].role != roleUser || got.messages[0].text != "/editor-mode bogus" {
		t.Fatalf("expected command to be recorded first, got %#v", got.messages[0])
	}
	if !strings.Contains(got.messages[1].text, "Invalid editor mode") {
		t.Fatalf("expected error to mention invalid mode, got %q", got.messages[1].text)
	}
}

func TestEstimateTok(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcde", 1},
		{"abcdefgh", 2},
	}
	for _, c := range cases {
		if got := estimateTok(c.input); got != c.want {
			t.Errorf("estimateTok(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestContextCommandIsRegistered(t *testing.T) {
	spec := lookupCommand("/context")
	if spec == nil {
		t.Fatal("expected /context to be registered")
	}
	if spec.help == "" {
		t.Fatal("expected /context to have help text")
	}
}

func TestContextCommandNilAgentGuard(t *testing.T) {
	m := model{width: 80}
	// must not panic
	m.handleContextCmd(nil)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	if !strings.Contains(m.messages[0].text, "No agent") {
		t.Fatalf("expected no-agent message, got %q", m.messages[0].text)
	}
}

func TestContextMCPGrouping(t *testing.T) {
	serverNames := []string{"claude_ai_Gmail", "context7"}
	toolNames := map[string]struct{}{
		"claude_ai_Gmail_search": {},
		"claude_ai_Gmail_send":   {},
		"context7_query":         {},
		"bash":                   {},
	}
	defs := []map[string]interface{}{
		{"name": "claude_ai_Gmail_search", "description": "search"},
		{"name": "claude_ai_Gmail_send", "description": "send"},
		{"name": "context7_query", "description": "query"},
		{"name": "bash", "description": "run bash"},
	}

	grouped, builtin := groupMCPToolDefs(defs, toolNames, serverNames)

	if len(grouped["claude_ai_Gmail"]) != 2 {
		t.Errorf("expected 2 tools for claude_ai_Gmail, got %d", len(grouped["claude_ai_Gmail"]))
	}
	if len(grouped["context7"]) != 1 {
		t.Errorf("expected 1 tool for context7, got %d", len(grouped["context7"]))
	}
	if len(builtin) != 1 || builtin[0]["name"] != "bash" {
		t.Errorf("expected bash in builtin, got %v", builtin)
	}
}

func TestContextCommandInHelp(t *testing.T) {
	help := commandHelpText()
	if !strings.Contains(help, "/context") {
		t.Fatalf("expected /context in help text, got:\n%s", help)
	}
}

func TestContextCommandAutocompletes(t *testing.T) {
	m := model{}
	results := autocompleteSlashInput(&m, "/con")
	found := false
	for _, r := range results {
		if r == "/context" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /context in autocomplete for '/con', got %v", results)
	}
}

func TestContextCommandOutputHasNoRaw(t *testing.T) {
	m := model{width: 80, agent: agent.NewAgent(nil, nil, nil, nil), config: &config.Config{}}
	m.handleContextCmd(nil)
	for _, msg := range m.messages {
		if msg.raw != nil {
			t.Fatal("context command output must not set raw field (would inject into LLM)")
		}
	}
}

func TestContextCommandOutputsSections(t *testing.T) {
	m := model{width: 80, agent: agent.NewAgent(nil, nil, nil, nil), config: &config.Config{}}
	m.handleContextCmd(nil)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	for _, want := range []string{"Context Budget", "Base Prompt", "Tools (injected every request)", "MCP: (none)", "Skill catalog (pre-injected)", "Skills (full contents available on demand, not pre-injected)", "Session Messages"} {
		if !strings.Contains(m.messages[0].text, want) {
			t.Fatalf("expected %q in context output, got %q", want, m.messages[0].text)
		}
	}
	if m.messages[0].raw != nil {
		t.Fatal("context command output must not set raw field")
	}
}

// oneShotStreamClient returns a single tool-free assistant message so a.Step
// completes in one turn without retries or hanging.
type oneShotStreamClient struct{}

func (oneShotStreamClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return &agent.Message{Role: "assistant", Content: "done"}, nil
}
func (oneShotStreamClient) GetProvider() string { return "test" }
func (oneShotStreamClient) GetModel() string    { return "test-model" }

// TestCustomCommandUsesStreamingPath guards the fix for custom slash commands
// (e.g. /review-changes) freezing the chat: they must stream live via
// streamStartedMsg, not run synchronously and only deliver output at the end.
func TestCustomCommandUsesStreamingPath(t *testing.T) {
	a := agent.NewAgent(oneShotStreamClient{}, nil, &config.Config{}, nil)
	m := &model{agent: a, config: &config.Config{}}

	cmd := m.sendCustomCommandPrompt("review please")
	if cmd == nil {
		t.Fatal("sendCustomCommandPrompt returned nil command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg from the streaming path, got %T", msg)
	}
	found := false
	for _, c := range batch {
		if c == nil {
			continue
		}
		if _, ok := c().(streamStartedMsg); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("custom command did not emit streamStartedMsg — it is not using the streaming path")
	}
}
