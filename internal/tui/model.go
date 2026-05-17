package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/skill"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
	"github.com/jamesmercstudio/ocode/internal/tool"
	"github.com/jamesmercstudio/ocode/internal/version"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type role int

const (
	roleUser role = iota
	roleAssistant
)

type message struct {
	role role
	text string
	raw  *agent.Message
}

type editorFinishedMsg struct {
	content string
	err     error
}

type permissionAskMsg struct {
	toolName   string
	toolArgs   json.RawMessage
	toolCallID string
}

type authFinishedMsg struct {
	token string
	err   error
}

type shellFinishedMsg struct {
	command string
	err     error
}

type connectOAuthFinishedMsg struct {
	provider string
	cred     auth.Credential
	err      error
}

type connectTestFinishedMsg struct {
	provider string
	err      error
}

type fileSearchFinishedMsg struct {
	processedText string
	messages      []message
	err           error
}

type leaderTimeoutMsg struct {
	seq int
}

type statusMsg struct {
	text string
}

type streamMsgEvent struct {
	msg    agent.Message
	ch     chan agent.Message
	errCh  chan error
	cancel chan struct{}
}

type ctrlCResetMsg struct{}
type streamStartedMsg struct{ cancel chan struct{} }

type streamDoneMsg struct {
	err error
}

type activityUpdateMsg struct {
	snap agent.ActivitySnapshot
}

type model struct {
	viewport            viewport.Model
	input               textarea.Model
	messages            []message
	agent               *agent.Agent
	config              *config.Config
	sessionID           string
	showThinking        bool
	showDetails         bool
	leaderActive        bool
	leaderSeq           int
	showPalette         bool
	showPicker          bool
	pickerKind          string
	pickerItems         []string
	pickerValues        []string
	pickerIndex         int
	pickerFilter        string
	showSlashPopup      bool
	slashPopupIndex     int
	slashPopupItems     []slashSuggestion
	showConnect         bool
	connect             *connectDialog
	showSidebar         bool
	sessionTelemetry    sidebarTelemetry
	activeModel         string
	paletteInput        string
	width               int
	height              int
	ready               bool
	err                 error
	scrollSpeed         int
	workDir             string
	currentAgentIdx     int
	showPermDialog      bool
	pendingToolName     string
	pendingToolArgs     json.RawMessage
	pendingToolCallID   string
	pendingPermission   agent.PermissionRequest
	styles              Styles
	streaming           bool
	ctrlCPressed        bool
	cancelStream        chan struct{}
	lastActivity        agent.ActivitySnapshot
	activityRowReserved bool
	escPressed          bool
	escPressTime        time.Time
	lastRetryableLLMErr string
	inputHistory        []string
	inputHistoryIndex   int
	queuedInputs        []string
	showFullToolOutput  bool
	fullToolOutputTitle string
	fullToolOutput      viewport.Model
	toolOutputLineMap   map[int]int
}

type agentResponseMsg string
type errorMsg error

var (
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Bold(true)
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#BB9AF7")).Bold(true)
	borderStyle    = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3B4261")).
			Padding(0, 1)
	hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Italic(true)

	todoDoneStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Strikethrough(true)
	todoInProgressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0AF68")).Bold(true)
	todoPendingStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A9B1D6"))
)

// styleTodoLine renders a markdown todo line with strikethrough/dim for done
// items and a warning color for in-progress (`- [~]` or `- [-]`). Non-todo
// lines are returned unchanged.
func styleTodoLine(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]
	prefix, body, ok := splitTodoMarker(trimmed)
	if !ok {
		return line
	}
	switch prefix {
	case "x", "X":
		return indent + todoDoneStyle.Render("- [✓] "+body)
	case "~", "-":
		return indent + todoInProgressStyle.Render("- [⟳] "+body)
	case " ":
		return indent + todoPendingStyle.Render("- [○] "+body)
	default:
		return line
	}
}

func splitTodoMarker(s string) (marker, body string, ok bool) {
	if len(s) < 6 || s[0] != '-' || s[1] != ' ' || s[2] != '[' || s[4] != ']' {
		return "", "", false
	}
	rest := s[5:]
	if len(rest) > 0 && rest[0] == ' ' {
		rest = rest[1:]
	}
	return string(s[3]), rest, true
}

const (
	sidebarMinWidth    = 120
	sidebarColumnWidth = 38
)

func (m *model) applyTheme() {
	if m.config != nil && m.config.TUI.Theme != "" {
		m.styles = ApplyThemeColors(m.config.TUI.Theme)
	} else {
		m.styles = ApplyThemeColors("tokyonight")
	}
}

func (m *model) toggleSidebar() {
	m.showSidebar = !m.showSidebar
	m.layout()
}

func (m model) sidebarEnabled() bool {
	return m.showSidebar && m.width >= sidebarMinWidth
}

func (m model) panelWidth() int {
	if m.sidebarEnabled() {
		return m.width - sidebarColumnWidth
	}
	return m.width
}

func (m model) currentModelName() string {
	if m.activeModel != "" {
		return m.activeModel
	}
	if m.config != nil && m.config.Model != "" {
		return m.config.Model
	}

	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].raw != nil && m.messages[i].raw.Model != "" {
			return m.messages[i].raw.Model
		}
	}

	return "no model"
}

func (m *model) getInitialTools() []tool.Tool {
	return []tool.Tool{
		&tool.ReadTool{},
		&tool.WriteTool{Config: m.config},
		&tool.DeleteTool{},
		&tool.GlobTool{},
		&tool.GrepTool{},
		&tool.BashTool{},
		&tool.EditTool{Config: m.config},
		&tool.MultiEditTool{},
		&tool.PatchTool{},
		&tool.TodoWriteTool{},
		&tool.SkillTool{},
		&tool.QuestionTool{},
		&tool.WebFetchTool{},
		&tool.WebSearchTool{},
		&tool.ListTool{},
		&tool.LSPTool{},
		&tool.FormatTool{Config: m.config},
	}
}

func (m *model) switchAgent(name string) {
	spec := agent.FindAgentSpec(name)
	if spec == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown agent: %s", name)})
		return
	}
	for i := range agent.DefaultAgents {
		if agent.DefaultAgents[i].Name == name {
			m.currentAgentIdx = i
			break
		}
	}
	if m.agent != nil {
		m.agent.SetSpec(spec)
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Agent switched to: %s (%s)", spec.Name, spec.Description)})
}

func newModel(sid string, cont bool, yolo bool) model {
	cfg, _ := config.Load()
	_ = auth.HydrateEnv()

	if cont {
		sessions, _ := session.List()
		if len(sessions) > 0 {
			sid = sessions[0].ID
		}
	}

	tmp := model{}
	tools := tmp.getInitialTools()

	var a *agent.Agent
	if cfg != nil && cfg.Model != "" {
		client := agent.NewClient(cfg, cfg.Model)
		a = agent.NewAgent(client, tools, cfg)
		if yolo && a.Permissions() != nil {
			a.Permissions().SetMode(agent.PermissionModeYOLO)
		}
		a.LoadExternalTools(cfg)
	}

	ta := textarea.New()
	ta.Placeholder = "Ask anything…  (enter to send, shift+enter for newline, ctrl+c twice to quit)"
	ta.Focus()
	ta.Prompt = "▍ "
	ta.CharLimit = 8000
	ta.SetHeight(3)
	ta.MaxWidth = 80
	ta.ShowLineNumbers = false
	styles := ta.Styles()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	ta.SetStyles(styles)
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"), key.WithHelp("shift+enter", "insert newline"))

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent(hintStyle.Render("  ocode v"+version.Version+" — opencode clone · type a message to begin\n"))

	if sid == "" {
		sid = time.Now().Format("2006-01-02-150405")
	}
	tool.SetTodoSession(sid)
	snapshot.Reset()
	tool.ResetTodoState()

	m := model{
		viewport:     vp,
		input:        ta,
		messages:     []message{},
		config:       cfg,
		agent:        a,
		sessionID:    sid,
		showThinking: true,
		showSidebar:  true,
		activeModel: func() string {
			if cfg != nil {
				return cfg.Model
			}
			return ""
		}(),
		scrollSpeed: 3,
		workDir: func() string {
			d, _ := os.Getwd()
			return d
		}(),
	}

	if cfg != nil && cfg.TUI.Scroll != 0 {
		m.scrollSpeed = int(cfg.TUI.Scroll)
	}

	m.applyTheme()

	if sid != "" {
		sess, err := session.Load(sid)
		if err == nil {
			m.sessionTelemetry = telemetryFromSessionMetadata(sess.Metadata)
			restoreTodoState(sess.Metadata)
			for _, am := range sess.Messages {
				role := tuiRoleForAgentMessage(am)
				copyMsg := am
				m.messages = append(m.messages, message{role: role, text: am.Content, raw: &copyMsg})
			}
		}
	}

	return m
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if m.showFullToolOutput {
			m.width = msg.Width
			m.height = msg.Height
			m.layoutFullToolOutput()
			m.ready = true
			return m, nil
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if idx, ok := m.toolOutputForClick(msg); ok {
				m.openFullToolOutput(idx)
				return m, nil
			}
			if m.showPicker {
				mouse := msg.Mouse()
				if idx, ok := m.pickerRowForY(mouse.Y); ok {
					m.pickerIndex = idx
					return m.selectPickerIndex(idx)
				}
				return m, nil
			}
			if m.showConnect {
				mouse := msg.Mouse()
				if idx, ok := m.connectRowForY(mouse.Y); ok {
					return m.selectConnectRow(idx)
				}
				return m, nil
			}
			if m.showSlashPopup {
				mouse := msg.Mouse()
				if idx, ok := m.slashPopupRowForY(mouse.Y); ok {
					selected := m.slashPopupItems[idx]
					m.closeSlashPopup()
					m.input.SetValue(selected.name + " ")
					if selected.name == "/models" {
						m.openModelPicker()
					} else if selected.name == "/session" {
						m.openSessionPicker()
					} else if selected.name == "/themes" {
						m.openThemePicker()
					}
					return m, nil
				}
			}
			if path, ok := m.sidebarFileForClick(msg); ok {
				return m, openSidebarFileInEditor(path)
			}
		}
	case tea.MouseWheelMsg:
		if m.showFullToolOutput {
			if msg.Button == tea.MouseWheelUp {
				m.fullToolOutput.ScrollUp(m.scrollSpeed)
				return m, nil
			}
			if msg.Button == tea.MouseWheelDown {
				m.fullToolOutput.ScrollDown(m.scrollSpeed)
				return m, nil
			}
		}
		if msg.Button == tea.MouseWheelUp {
			m.viewport.ScrollUp(m.scrollSpeed)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.viewport.ScrollDown(m.scrollSpeed)
			return m, nil
		}
	case tea.KeyPressMsg:
		if m.showFullToolOutput {
			switch msg.String() {
			case "esc", "b", "backspace":
				m.showFullToolOutput = false
				m.renderTranscript()
				return m, nil
			}
			var cmd tea.Cmd
			m.fullToolOutput, cmd = m.fullToolOutput.Update(msg)
			return m, cmd
		}
		if m.showSlashPopup {
			switch msg.String() {
			case "esc":
				m.closeSlashPopup()
				return m, nil
			case "up":
				if m.slashPopupIndex > 0 {
					m.slashPopupIndex--
					return m, nil
				}
				// fall through to textarea when already at top
			case "down", "tab":
				if len(m.slashPopupItems) == 0 {
					break // fall through to outer handler when nothing to navigate
				}
				if m.slashPopupIndex < len(m.slashPopupItems)-1 {
					m.slashPopupIndex++
				}
				return m, nil
			case "enter":
				if len(m.slashPopupItems) > 0 && m.slashPopupIndex < len(m.slashPopupItems) && !m.inputIsExactSlashCommand() {
					selected := m.slashPopupItems[m.slashPopupIndex]
					m.closeSlashPopup()
					m.input.SetValue(selected.name + " ")
					if selected.name == "/models" {
						m.openModelPicker()
					} else if selected.name == "/session" {
						m.openSessionPicker()
					} else if selected.name == "/themes" {
						m.openThemePicker()
					}
					return m, nil
				}
			}
		}
	}

	m.input, tiCmd = m.input.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	m = m.updateSlashPopupState()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.ready = true
	case tea.KeyPressMsg:
		keyStr := msg.String()
		if m.showPicker {
			switch keyStr {
			case "esc":
				m.closePicker()
				return m, nil
			case "up":
				if m.pickerIndex > 0 {
					m.pickerIndex--
				}
				return m, nil
			case "down":
				items, _ := m.pickerVisibleItems()
				if m.pickerIndex < len(items)-1 {
					m.pickerIndex++
				}
				return m, nil
			case "enter":
				return m.selectPickerIndex(m.pickerIndex)
			case "backspace":
				if len(m.pickerFilter) > 0 {
					m.pickerFilter = m.pickerFilter[:len(m.pickerFilter)-1]
					m.pickerIndex = 0
				}
				return m, nil
			}
			if len(msg.Text) > 0 {
				m.pickerFilter += msg.Text
				m.pickerIndex = 0
				return m, nil
			}
			return m, nil
		}

		if m.showConnect {
			return m.updateConnectDialog(msg)
		}

		if m.showPalette {
			if keyStr == "esc" || keyStr == "ctrl+p" {
				m.showPalette = false
				return m, nil
			}
			if keyStr == "enter" {
				m.showPalette = false
				return m.handleCommand(m.paletteInput)
			}
			if keyStr == "backspace" {
				if len(m.paletteInput) > 0 {
					m.paletteInput = m.paletteInput[:len(m.paletteInput)-1]
				}
				return m, nil
			}
			if len(msg.Text) > 0 {
				m.paletteInput += msg.Text
			}
			return m, nil
		}

		if m.leaderActive {
			m.leaderActive = false

			key := keyStr
			if m.config != nil {
				if cmd, ok := m.config.TUI.Keybinds[key]; ok {
					return m.handleCommand(cmd)
				}
			}

			switch key {
			case "u":
				return m.handleCommand("/undo")
			case "r":
				return m.handleCommand("/redo")
			case "n":
				return m.handleCommand("/new")
			case "l":
				return m.handleCommand("/session")
			case "c":
				return m.handleCommand("/compact")
			case "q":
				m.saveSession()
				return m, tea.Quit
			}
			return m, nil
		}

		if m.escPressed && keyStr != "esc" {
			m.escPressed = false
		}

		switch keyStr {
		case "ctrl+p":
			m.showPalette = !m.showPalette
			m.paletteInput = ""
			return m, nil
		case "up":
			// Navigate input history backwards
			if len(m.inputHistory) == 0 {
				break // fall through to textarea
			}
			if m.inputHistoryIndex == -1 {
				// First up: go to most recent entry
				m.inputHistoryIndex = len(m.inputHistory) - 1
			} else if m.inputHistoryIndex > 0 {
				m.inputHistoryIndex--
			}
			m.input.SetValue(m.inputHistory[m.inputHistoryIndex])
			return m, nil
		case "down":
			// Navigate input history forwards
			if len(m.inputHistory) == 0 || m.inputHistoryIndex == -1 {
				break // fall through to textarea
			}
			if m.inputHistoryIndex < len(m.inputHistory)-1 {
				m.inputHistoryIndex++
				m.input.SetValue(m.inputHistory[m.inputHistoryIndex])
			} else {
				// Past the most recent: clear input
				m.inputHistoryIndex = -1
				m.input.SetValue("")
			}
			return m, nil
		case "ctrl+b":
			m.toggleSidebar()
			return m, nil
		case "ctrl+o":
			return m.handleCommand("/yolo")
		case "ctrl+y":
			return m.retryLastLLMError()
		case "ctrl+x":
			m.leaderActive = true
			m.leaderSeq++
			timeout := 2000
			if m.config != nil && m.config.TUI.LeaderTimeout != 0 {
				timeout = m.config.TUI.LeaderTimeout
			}
			seq := m.leaderSeq
			return m, tea.Tick(time.Duration(timeout)*time.Millisecond, func(time.Time) tea.Msg {
				return leaderTimeoutMsg{seq: seq}
			})
		case "esc":
			if m.streaming && m.cancelStream != nil {
				select {
				case <-m.cancelStream:
				default:
					close(m.cancelStream)
				}
				return m, nil
			}
			if !m.escPressed {
				m.escPressed = true
				m.escPressTime = time.Now()
				return m, nil
			}
			if time.Since(m.escPressTime) < 500*time.Millisecond {
				m.escPressed = false
				m.openMessagePicker()
				return m, nil
			}
			m.escPressed = false
			return m, nil
		case "ctrl+c":
			if m.ctrlCPressed {
				m.saveSession()
				return m, tea.Quit
			}
			m.ctrlCPressed = true
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return ctrlCResetMsg{} })
		case "shift+tab":
			m.cycleAgentMode()
			return m, nil
		case "tab":
			current := m.input.Value()
			if strings.HasPrefix(current, "/") {
				trimmed := strings.TrimSpace(current)
				if trimmed == "/models" || strings.HasPrefix(trimmed, "/models ") || trimmed == "/model" || strings.HasPrefix(trimmed, "/model ") {
					m.openModelPicker()
					return m, nil
				}
				if trimmed == "/themes" || strings.HasPrefix(trimmed, "/themes ") || trimmed == "/theme" || strings.HasPrefix(trimmed, "/theme ") {
					m.openThemePicker()
					return m, nil
				}

				suggestions := autocompleteSlashInput(&m, current)
				if len(suggestions) == 0 {
					return m, nil
				}

				if strings.HasSuffix(current, " ") {
					m.input.SetValue(strings.TrimSpace(current) + " " + suggestions[0])
					return m, nil
				}

				m.input.SetValue(suggestions[0])
				return m, nil
			}
			m.cycleAgentMode()
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, tea.Batch(tiCmd, vpCmd)
			}

			// Save non-command inputs to history
			if !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "!") {
				// Deduplicate: don't add if same as last entry
				if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != text {
					m.inputHistory = append(m.inputHistory, text)
				}
			}
			m.inputHistoryIndex = -1

			if strings.HasPrefix(text, "/") {
				m.closeSlashPopup()
				return m.handleCommand(text)
			}

			if strings.HasPrefix(text, "!") {
				m.input.Reset()
				cmdText := strings.TrimPrefix(text, "!")
				m.messages = append(m.messages, message{role: roleUser, text: text})
				m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("🔧 running shell: %s", cmdText)})
				m.renderTranscript()
				m.viewport.GotoBottom()
				return m, runInteractiveShell(cmdText, m.workDir)
			}

			if m.streaming {
				m.queuedInputs = append(m.queuedInputs, text)
				m.input.Reset()
				m.layout()
				m.viewport.GotoBottom()
				return m, nil
			}

			var pendingToolCallID string
			if len(m.messages) > 0 {
				last := m.messages[len(m.messages)-1]
				if last.raw != nil && len(last.raw.ToolCalls) > 0 {
					for _, tc := range last.raw.ToolCalls {
						if tc.Function.Name == "question" {
							pendingToolCallID = tc.ID
							break
						}
					}
				}
			}

			if pendingToolCallID != "" {
				m.messages = append(m.messages, message{
					role: roleAssistant,
					text: fmt.Sprintf("✅ tool result: %s", text),
					raw: &agent.Message{
						Role:    "tool",
						Content: text,
						ToolID:  pendingToolCallID,
					},
				})
				m.input.Reset()
				m.renderTranscript()
				m.viewport.GotoBottom()
				m.saveSession()
				return m, m.askAgent()
			}

			if m.showPermDialog {
				choice := strings.ToLower(strings.TrimSpace(text))
				m.showPermDialog = false
				cmd := m.handlePermissionChoice(choice)
				m.input.Reset()
				m.renderTranscript()
				m.viewport.GotoBottom()
				m.saveSession()
				return m, cmd
			}

			m.input.Reset()
			return m, m.processFileReferences(text)
		}
	case leaderTimeoutMsg:
		if m.leaderActive && msg.seq == m.leaderSeq {
			m.leaderActive = false
		}
		return m, nil
	case fileSearchFinishedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error processing files: %v", msg.err)})
		} else {
			m.messages = append(m.messages, msg.messages...)
			m.messages = append(m.messages, message{role: roleUser, text: msg.processedText})
		}
		m.renderTranscript()
		m.viewport.GotoBottom()
		m.saveSession()
		if m.agent != nil {
			return m, m.askAgent()
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: hintStyle.Render("(no llm configured, check opencode.json)")})
		m.renderTranscript()
		m.viewport.GotoBottom()
	case authFinishedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Login failed: %v", msg.err)})
		} else {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Google Login successful! Token received."})
			os.Setenv("GOOGLE_OAUTH_ACCESS_TOKEN", msg.token)
			if m.config != nil && m.config.Model != "" {
				client := agent.NewClient(m.config, m.config.Model)
				m.agent = agent.NewAgent(client, m.getInitialTools(), m.config)
			}
		}
		m.renderTranscript()
	case statusMsg:
		m.messages = append(m.messages, message{role: roleAssistant, text: msg.text})
		m.renderTranscript()
		m.viewport.GotoBottom()
	case connectOAuthFinishedMsg:
		if m.connect == nil {
			return m, nil
		}
		if msg.err != nil {
			m.connect.message = fmt.Sprintf("OAuth failed: %v", msg.err)
			m.connect.messageOK = false
			m.connect.stage = connectStageMessage
			m.viewport.GotoBottom()
			return m, nil
		}
		if err := auth.Set(msg.provider, msg.cred); err != nil {
			m.connect.message = fmt.Sprintf("OAuth succeeded but failed to save: %v", err)
			m.connect.messageOK = false
			m.connect.stage = connectStageMessage
			m.viewport.GotoBottom()
			return m, nil
		}
		m.rebuildAgentClient()
		label := msg.provider
		if msg.cred.Account != "" {
			label = msg.provider + " (" + msg.cred.Account + ")"
		}
		m.connect.message = fmt.Sprintf("%s OAuth complete. Testing connection…", label)
		m.connect.messageOK = true
		m.connect.stage = connectStageMessage
		m.viewport.GotoBottom()
		return m, m.testConnection(msg.provider)
	case connectTestFinishedMsg:
		if m.connect == nil {
			return m, nil
		}
		if msg.err != nil {
			m.connect.message = fmt.Sprintf("%s\n\n⚠ Connection test failed: %v", m.connect.message, msg.err)
			m.connect.messageOK = false
		} else {
			m.connect.message = fmt.Sprintf("%s\n\n✓ Connection verified.", m.connect.message)
		}
	case shellFinishedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Shell command failed: %v", msg.err)})
		} else {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Shell command finished: %s", msg.command)})
		}
		m.renderTranscript()
		m.viewport.GotoBottom()
		m.saveSession()
	case []agent.Message:
		for _, am := range msg {
			m.appendAgentMessage(am)
		}
		m.renderTranscript()
		m.viewport.GotoBottom()
		m.saveSession()
		if len(msg) > 0 && (msg[len(msg)-1].Role == "tool" || (msg[len(msg)-1].Role == "assistant" && len(msg[len(msg)-1].ToolCalls) > 0)) {
			stop := false
			last := msg[len(msg)-1]
			if last.Role == "assistant" {
				for _, tc := range last.ToolCalls {
					if tc.Function.Name == "question" {
						stop = true
						break
					}
				}
			}
			if last.Role == "tool" && strings.HasPrefix(last.Content, "PERMISSION_ASK:") {
				stop = true
			}
			if !stop {
				return m, m.askAgent()
			}
		}
	case ctrlCResetMsg:
		m.ctrlCPressed = false
	case streamStartedMsg:
		m.streaming = true
		m.cancelStream = msg.cancel
		m.lastActivity = agent.ActivitySnapshot{LLMRunning: true}
		if !m.activityRowReserved {
			m.activityRowReserved = true
			m.layout()
		}
		if m.agent != nil {
			return m, listenActivity(m.agent.Activity())
		}
	case activityUpdateMsg:
		m.lastActivity = msg.snap
		if !m.activityRowReserved {
			m.activityRowReserved = true
			m.layout()
		}
		if m.agent != nil {
			return m, listenActivity(m.agent.Activity())
		}
	case streamMsgEvent:
		m.appendAgentMessage(msg.msg)
		m.renderTranscript()
		m.viewport.GotoBottom()
		return m, waitStreamEvent(msg.ch, msg.errCh, msg.cancel)
	case streamDoneMsg:
		m.streaming = false
		m.lastActivity = agent.ActivitySnapshot{}
		m.layout()
		m.saveSession()
		if msg.err != nil {
			errorText := fmt.Sprintf("Error: %v", msg.err)
			if isRetryableLLMError(msg.err) {
				m.lastRetryableLLMErr = errorText
			} else {
				m.lastRetryableLLMErr = ""
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: errorText})
			m.renderTranscript()
			m.viewport.GotoBottom()
		} else {
			m.lastRetryableLLMErr = ""
			if len(m.queuedInputs) > 0 && m.agent != nil {
				text := m.queuedInputs[0]
				m.queuedInputs = m.queuedInputs[1:]
				m.layout()
				m.viewport.GotoBottom()
				return m, m.processFileReferences(text)
			}
		}
	case editorFinishedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Editor error: %v", msg.err)})
		} else {
			m.input.SetValue(msg.content)
		}
	case errorMsg:
		if msg != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error: %v", error(msg))})
			m.renderTranscript()
			m.viewport.GotoBottom()
		}
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m *model) handleCommand(text string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return m, nil
	}
	cmd := parts[0]
	args := parts[1:]

	if cmd != "/editor" {
		m.input.Reset()
	}

	var cmdResult tea.Cmd
	spec := lookupCommand(cmd)
	if spec != nil {
		cmdResult = spec.handler(m, args)
	} else if customCmd, ok := customCommandLookup[cmd]; ok {
		prompt := customCmd.Prompt
		userArgs := strings.Join(args, " ")
		if userArgs != "" {
			prompt = strings.ReplaceAll(prompt, "{{args}}", userArgs)
			prompt = strings.ReplaceAll(prompt, "{args}", userArgs)
			if !strings.HasSuffix(prompt, "\n") && !strings.HasSuffix(prompt, " ") {
				prompt += " " + userArgs
			}
		}
		m.messages = append(m.messages, message{role: roleUser, text: cmd + " " + userArgs})
		m.renderTranscript()
		m.viewport.GotoBottom()
		return m, m.sendCustomCommandPrompt(prompt)
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown command: %s", cmd)})
	}

	m.renderTranscript()
	m.viewport.GotoBottom()
	return m, cmdResult
}

func (m *model) handleLoginCmd(args []string) tea.Cmd {
	return func() tea.Msg {
		token, err := auth.LoginWithGoogle()
		return authFinishedMsg{token: token, err: err}
	}
}

func (m *model) handleMCPAuth(serverName string) error {
	if m.config == nil {
		return fmt.Errorf("config not loaded")
	}
	mcpCfg, ok := m.config.MCP[serverName]
	if !ok {
		return fmt.Errorf("mcp server %q not found in config", serverName)
	}
	if mcpCfg.OAuth == nil {
		return fmt.Errorf("mcp server %q has no oauth configuration", serverName)
	}
	oauth := mcpCfg.OAuth
	if oauth.AuthorizationURL == "" || oauth.TokenURL == "" || oauth.ClientID == "" {
		return fmt.Errorf("mcp server %q oauth config is incomplete", serverName)
	}
	return auth.MCPAuthFlow(serverName, oauth.AuthorizationURL, oauth.TokenURL, oauth.ClientID, oauth.Scopes)
}

func (m *model) processFileReferences(text string) tea.Cmd {
	return func() tea.Msg {
		re := regexp.MustCompile(`@([^\s]+)`)
		matches := re.FindAllStringSubmatch(text, -1)
		processedText := text
		var msgs []message

		for _, match := range matches {
			path := match[1]
			foundPath := ""
			filepath.Walk(".", func(p string, info os.FileInfo, err error) error { //nolint:errcheck
				if err != nil {
					return nil
				}
				if foundPath != "" {
					// Already found — skip remaining directories to avoid full-tree scan.
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if info.IsDir() {
					return nil
				}
				if strings.Contains(strings.ToLower(p), strings.ToLower(path)) {
					foundPath = p
				}
				return nil
			})

			if foundPath != "" {
				path = foundPath
			}

			content, err := os.ReadFile(path)
			if err == nil {
				fileCtx := fmt.Sprintf("\n--- File: %s ---\n%s\n", path, string(content))
				msgs = append(msgs, message{
					role: roleAssistant,
					text: fmt.Sprintf("📎 Added context from %s", path),
					raw: &agent.Message{
						Role:    "system",
						Content: fileCtx,
					},
				})
			}
		}
		return fileSearchFinishedMsg{processedText: processedText, messages: msgs}
	}
}

func (m *model) handleModelCmd(args []string) {
	if len(args) == 0 {
		m.openModelPicker()
		return
	}
	if len(args) > 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Switching to model %s", args[0])})
		var mcpNames []string
		if m.agent != nil {
			mcpNames = m.agent.MCPToolNames()
		}
		client := agent.NewClient(m.config, args[0])
		if client != nil {
			var tools []tool.Tool
			if m.agent != nil {
				tools = m.agent.GetTools()
			} else {
				tools = m.getInitialTools()
			}
			m.agent = agent.NewAgent(client, tools, m.config)
			m.agent.RestoreMCPToolNames(mcpNames)
			m.activeModel = args[0]
			if m.config != nil {
				m.config.Model = args[0]
			}
			// SaveLastModel persists any model name to ocodeconfig.json (project-level)
			if err := config.SaveLastModel(args[0]); err != nil {
				log.Printf("save last model: %v", err)
			}
			// SaveRecentModel requires "provider/model" format and goes to the global state file
			if strings.Contains(args[0], "/") {
				if err := config.SaveRecentModel(args[0]); err != nil {
					log.Printf("save recent model: %v", err)
				}
			}
		}
	}
}

func (m *model) handleThinkingCmd(args []string) {
	m.showThinking = !m.showThinking
	status := "hidden"
	if m.showThinking {
		status = "visible"
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Thinking blocks are now %s.", status)})
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

func (m *model) handleConnectCmd(args []string) {
	if len(args) == 0 {
		m.openConnectDialog()
		return
	}
	if len(args) == 2 {
		provider := args[0]
		key := args[1]
		p := auth.FindProvider(provider)
		if p == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown provider: %s", provider)})
			return
		}
		if err := auth.Set(p.ID, auth.Credential{Kind: auth.KindAPIKey, Key: key}); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to save key: %v", err)})
			return
		}
		if p.EnvVar != "" {
			os.Setenv(p.EnvVar, key)
		}
		m.rebuildAgentClient()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf(
			"API key for %s set (%s). Saved to ~/.config/ocode/auth.json.", p.Label, maskKey(key),
		)})
		return
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /connect [provider apikey] — or run /connect with no args for the dialog."})
}

func (m *model) handleSessionCmd(args []string) {
	if len(args) == 0 {
		m.openSessionPicker()
	} else if args[0] == "list" {
		sessions, _ := session.ListAll()
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for _, s := range sessions {
			title := s.Title
			if title == "" {
				title = "(no title)"
			}
			marker := "ocode"
			if s.Source == session.SourceClaude {
				marker = "claude"
			}
			b.WriteString(fmt.Sprintf("- [%s] %s: %s\n", marker, s.ID, title))
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
	} else if args[0] == "load" && len(args) > 1 {
		sess, err := session.LoadAny(args[1])
		if err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error loading session: %v", err)})
		} else {
			m.saveSession()
			m.sessionID = sess.ID
			tool.SetTodoSession(m.sessionID)
			snapshot.Reset()
			tool.ResetTodoState()
			m.sessionTelemetry = telemetryFromSessionMetadata(sess.Metadata)
			restoreTodoState(sess.Metadata)
			m.messages = []message{}
			for _, am := range sess.Messages {
				role := tuiRoleForAgentMessage(am)
				copyMsg := am
				m.messages = append(m.messages, message{role: role, text: am.Content, raw: &copyMsg})
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Loaded session %s", m.sessionID)})
			m.input.Focus()
			m.layout()
			m.viewport.GotoBottom()
		}
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /session [list|load <id>]"})
	}
}

func tuiRoleForAgentMessage(msg agent.Message) role {
	if msg.Role == "user" {
		return roleUser
	}
	return roleAssistant
}

func (m *model) handleCompactCmd(args []string) {
	newMsgs := []message{}
	for _, msg := range m.messages {
		if msg.role == roleUser || (msg.role == roleAssistant && msg.raw == nil) {
			newMsgs = append(newMsgs, msg)
		}
	}
	m.messages = newMsgs
	m.messages = append(m.messages, message{role: roleAssistant, text: "Conversation compacted (removed tool history from view)."})
}

func (m *model) handleRedoCmd(args []string) {
	path, err := snapshot.Redo()
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error redoing: %v", err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Successfully restored changes to %s", path)})
	}
}

func (m *model) handleExportCmd(args []string) {
	filename := fmt.Sprintf("ocode_export_%d.md", time.Now().Unix())
	var b strings.Builder
	for _, msg := range m.messages {
		role := "User"
		if msg.role == roleAssistant {
			role = "Assistant"
		}
		b.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", role, msg.text))
	}
	err := os.WriteFile(filename, []byte(b.String()), 0644)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error exporting: %v", err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Exported conversation to %s", filename)})
	}
}

func (m *model) handleNewCmd(args []string) {
	m.saveSession()
	m.messages = []message{}
	m.sessionID = time.Now().Format("2006-01-02-150405")
	tool.SetTodoSession(m.sessionID)
	snapshot.Reset()
	tool.ResetTodoState()
	m.sessionTelemetry = sidebarTelemetry{}
	m.inputHistory = nil
	m.inputHistoryIndex = -1
	m.messages = append(m.messages, message{role: roleAssistant, text: "Started new session."})
}

func (m *model) handleEditorCmd(args []string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	editor, ok := resolveEditor(editor)
	if !ok {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Error: EDITOR not set and no common editor found."})
		return nil
	}

	tmpFile, err := os.CreateTemp("", "ocode-msg-*.txt")
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error creating temp file: %v", err)})
		return nil
	}

	content := m.input.Value()
	tmpFile.Write([]byte(content))
	tmpFile.Close()

	cmdParts := strings.Fields(editor)
	cmdParts = append(cmdParts, tmpFile.Name())
	c := exec.Command(cmdParts[0], cmdParts[1:]...)

	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return editorFinishedMsg{err: err}
		}
		newContent, err := os.ReadFile(tmpFile.Name())
		os.Remove(tmpFile.Name())
		if err != nil {
			return editorFinishedMsg{err: err}
		}
		return editorFinishedMsg{content: string(newContent)}
	})
}

func runInteractiveShell(command string, dir string) tea.Cmd {
	c := shellExecCommand(command)
	if dir != "" {
		c.Dir = dir
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return shellFinishedMsg{command: command, err: err}
	})
}

func shellExecCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("bash", "-c", command)
}

var openSidebarFileInEditor = openPathInEditor

func openPathInEditor(path string) tea.Cmd {
	editor, ok := resolveEditor(os.Getenv("EDITOR"))
	if !ok {
		return func() tea.Msg { return errorMsg(fmt.Errorf("EDITOR not set and no common editor found")) }
	}

	cmdParts := strings.Fields(editor)
	cmdParts = append(cmdParts, path)
	c := exec.Command(cmdParts[0], cmdParts[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return errorMsg(err)
		}
		return nil
	})
}

func resolveEditor(editor string) (string, bool) {
	if editor != "" {
		return editor, true
	}
	for _, candidate := range []string{"vim", "nano", "notepad"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

func (m *model) handleShareCmd(args []string) {
	filename := fmt.Sprintf("ocode_share_%s.md", m.sessionID)
	var b strings.Builder
	b.WriteString("# Shared OpenCode Session\n\n")
	for _, msg := range m.messages {
		role := "User"
		if msg.role == roleAssistant {
			role = "Assistant"
		}
		b.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", role, msg.text))
	}
	os.WriteFile(filename, []byte(b.String()), 0644)
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Session shared to local file: %s", filename)})
}

func (m *model) handleThemesCmd(args []string) {
	if len(args) == 0 {
		m.openThemePicker()
		return
	}

	name := args[0]
	if _, ok := GetTheme(name); !ok {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown theme: %s", name)})
		return
	}

	m.config.TUI.Theme = name
	m.applyTheme()
	if err := config.SaveTUITheme(name); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Theme switched to %s (save failed: %v)", name, err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Theme switched to %s", name)})
	}
}

// handleModelsCmd is an alias for handleModelCmd; see commandSpecs for the /model ↔ /models aliasing.
func (m *model) handleModelsCmd(args []string) {
	m.handleModelCmd(args)
}

func (m *model) handleDetailsCmd(args []string) {
	m.showDetails = !m.showDetails
	status := "hidden"
	if m.showDetails {
		status = "visible"
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Tool execution details are now %s.", status)})
}

func (m *model) handleInitCmd(args []string) {
	if _, err := os.Stat("AGENTS.md"); err == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "AGENTS.md already exists."})
		return
	}

	content := "# Project Rules\n\n- Follow Go best practices.\n- Keep functions small and modular.\n"
	err := os.WriteFile("AGENTS.md", []byte(content), 0644)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error creating AGENTS.md: %v", err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Created AGENTS.md with default rules."})
	}
}

func (m *model) handleHelpCmd(args []string) {
	m.messages = append(m.messages, message{role: roleAssistant, text: commandHelpText()})
}

func (m *model) handleSkillsCmd(args []string) {
	skills := skill.LoadSkills()
	if len(skills) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No skills found."})
		return
	}
	var b strings.Builder
	b.WriteString("Available skills:\n")
	for _, s := range skills {
		b.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
}

func (m *model) sendCustomCommandPrompt(prompt string) tea.Cmd {
	return func() tea.Msg {
		if m.agent == nil {
			return errorMsg(fmt.Errorf("no agent configured"))
		}
		var agentMsgs []agent.Message
		ctx := agent.LoadContext()
		if ctx != "" {
			agentMsgs = append(agentMsgs, agent.Message{Role: "system", Content: "Context and rules:\n" + ctx})
		}
		agentMsgs = append(agentMsgs, agent.Message{Role: "user", Content: prompt})
		resp, err := m.agent.Step(agentMsgs)
		if err != nil {
			return errorMsg(err)
		}
		return resp
	}
}

func (m *model) handleUndoCmd(args []string) {
	path, err := snapshot.Undo()
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error undoing: %v", err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Successfully reverted changes to %s", path)})
	}
}

func (m *model) handleGitHubPR(owner, repo string, prNumber int) (string, error) {
	pr, err := ghGetPR(owner, repo, prNumber)
	if err != nil {
		return "", err
	}
	diff, err := ghGetPRDiff(owner, repo, prNumber)
	if err != nil {
		diff = "(diff unavailable)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("PR #%d: %s\n", pr.Number, pr.Title))
	b.WriteString(fmt.Sprintf("State: %s | Author: %s\n\n", pr.State, pr.User.Login))
	if pr.Body != "" {
		b.WriteString(pr.Body + "\n\n")
	}
	b.WriteString("--- DIFF ---\n")
	b.WriteString(diff)
	return b.String(), nil
}

func (m *model) handleGitHubIssueList(owner, repo, state string) (string, error) {
	issues, err := ghListIssues(owner, repo, state)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, issue := range issues {
		labels := strings.Join(issue.Labels, ", ")
		b.WriteString(fmt.Sprintf("#%d: %s [%s] by %s", issue.Number, issue.Title, issue.State, issue.Author))
		if labels != "" {
			b.WriteString(fmt.Sprintf(" | labels: %s", labels))
		}
		b.WriteString("\n")
	}
	if len(issues) == 0 {
		return "No issues found.", nil
	}
	return b.String(), nil
}

func (m *model) handleGitHubIssueGet(owner, repo string, number int) (string, error) {
	issue, err := ghGetIssue(owner, repo, number)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("#%d: %s\n", issue.Number, issue.Title))
	b.WriteString(fmt.Sprintf("State: %s | Author: %s\n", issue.State, issue.Author))
	if len(issue.Labels) > 0 {
		b.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(issue.Labels, ", ")))
	}
	b.WriteString("\n")
	b.WriteString(issue.Body)
	return b.String(), nil
}

func (m *model) handleGitHubWorkflow(name string) string {
	return ghGenerateWorkflow(name, nil)
}

func (m *model) saveSession() {
	if len(m.messages) > 0 {
		var agentMsgs []agent.Message
		for _, msg := range m.messages {
			if msg.raw != nil {
				agentMsgs = append(agentMsgs, *msg.raw)
			} else {
				role := "user"
				if msg.role == roleAssistant {
					role = "assistant"
				}
				agentMsgs = append(agentMsgs, agent.Message{Role: role, Content: msg.text})
			}
		}
		session.Save(m.sessionID, "", agentMsgs, m.sessionSidebarMetadata())
	}
}

func (m *model) appendAgentMessage(am agent.Message) {
	copyMsg := am
	if am.Role == "assistant" {
		if am.ReasoningContent != "" && m.showThinking {
			m.messages = append(m.messages, message{
				role: roleAssistant,
				text: m.styles.Thinking.Render("⟁ thinking\n") + renderThinkingContent(am.ReasoningContent, m.styles),
			})
		}
		if len(am.ToolCalls) > 0 {
			var b strings.Builder
			if am.Content != "" {
				b.WriteString(am.Content)
				b.WriteString("\n\n")
			}
			for i, tc := range am.ToolCalls {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(formatToolCallHint(tc))
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: b.String(), raw: &copyMsg})
		} else if am.Content != "" {
			m.messages = append(m.messages, message{role: roleAssistant, text: am.Content, raw: &copyMsg})
		}
	} else if am.Role == "tool" {
		if strings.HasPrefix(am.Content, "PERMISSION_ASK:") {
			if req, ok := parsePermissionRequest(am.Content); ok {
				m.showPermDialog = true
				m.pendingPermission = req
				m.pendingToolName = req.ToolName
				m.pendingToolArgs = req.Args
				m.pendingToolCallID = am.ToolID
				m.messages = append(m.messages, message{role: roleAssistant, text: renderPermissionPrompt(req), raw: &copyMsg})
			}
		} else {
			toolName := m.lookupToolName(am.ToolID)
			m.messages = append(m.messages, message{
				role: roleAssistant,
				text: renderToolResult(toolName, am.Content, m.styles),
				raw:  &copyMsg,
			})
		}
	}
	if am.Usage != nil || am.Spend != nil {
		m.sessionTelemetry.addMessage(am)
	}
}

func parsePermissionRequest(content string) (agent.PermissionRequest, bool) {
	var req agent.PermissionRequest
	payload := strings.TrimPrefix(content, "PERMISSION_ASK:")
	if payload == content || payload == "" {
		return req, false
	}
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return req, false
	}
	if req.ToolName == "" {
		return req, false
	}
	return req, true
}

func renderPermissionPrompt(req agent.PermissionRequest) string {
	var b strings.Builder
	b.WriteString("Permission required\n\n")
	b.WriteString(fmt.Sprintf("Tool: %s\n", req.ToolName))
	if req.Command != "" {
		b.WriteString(fmt.Sprintf("Command: %s\n", req.Command))
	}
	if req.Prefix != "" {
		b.WriteString(fmt.Sprintf("Prefix: %s\n", req.Prefix))
	}
	if req.Rule != "" {
		b.WriteString(fmt.Sprintf("Matched rule: %s\n", req.Rule))
	}
	b.WriteString("\nChoose: [y] allow once  [n] deny once  [a] always allow matched rule  [t] always allow tool")
	return b.String()
}

func (m *model) handlePermissionChoice(choice string) tea.Cmd {
	if m.agent == nil {
		return func() tea.Msg { return errorMsg(fmt.Errorf("no agent configured")) }
	}
	req := m.pendingPermission
	toolName := m.pendingToolName
	args := m.pendingToolArgs
	if len(args) == 0 {
		args = req.Args
	}

	switch choice {
	case "y", "yes", "allow", "once":
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Allowed %q once.", toolName)})
		return m.executeApprovedTool(toolName, args)
	case "a", "always", "always allow":
		m.setPermissionRule(req, agent.PermissionAllow)
		m.persistPermissions()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Always allowing %s.", permissionRuleLabel(req))})
		return m.executeToolWithRules(toolName, args)
	case "t":
		m.setToolPermission(toolName, agent.PermissionAllow)
		m.persistPermissions()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Always allowing tool %q.", toolName)})
		return m.executeToolWithRules(toolName, args)
	case "n", "no", "deny":
		return m.permissionDeniedToolResult(toolName)
	default:
		m.showPermDialog = true
		m.messages = append(m.messages, message{role: roleAssistant, text: "Invalid permission choice. Use y, n, a, or t."})
		return nil
	}
}

func permissionRuleLabel(req agent.PermissionRequest) string {
	if req.Scope == agent.PermissionScopeBashPrefix && req.Prefix != "" {
		return fmt.Sprintf("bash prefix %q", req.Prefix)
	}
	return fmt.Sprintf("tool %q", req.ToolName)
}

func (m *model) setPermissionRule(req agent.PermissionRequest, level agent.PermissionLevel) {
	if req.Scope == agent.PermissionScopeBashPrefix && req.Prefix != "" {
		if m.agent != nil && m.agent.Permissions() != nil {
			m.agent.Permissions().SetBashPrefixRule(req.Prefix, level)
		}
		return
	}
	m.setToolPermission(req.ToolName, level)
}

func (m *model) setToolPermission(toolName string, level agent.PermissionLevel) {
	if m.agent != nil && m.agent.Permissions() != nil {
		m.agent.Permissions().SetRule(toolName, level)
	}
}

func (m *model) persistPermissions() {
	if m.agent == nil || m.agent.Permissions() == nil {
		return
	}
	permissions := m.agent.Permissions().ExportConfig()
	if m.config != nil && m.config.Ocode != nil {
		m.config.Ocode.Permissions = permissions
	}
	if err := config.SaveOcodePermissions(permissions); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to save permissions: %v", err)})
	}
}

func (m model) executeApprovedTool(toolName string, args json.RawMessage) tea.Cmd {
	return func() tea.Msg {
		result, err := m.agent.HandleApprovedToolCall(toolName, args)
		if err != nil {
			result = fmt.Sprintf("Error: %v", err)
		}
		return []agent.Message{{Role: "tool", ToolID: m.pendingToolCallID, Content: result}}
	}
}

func (m model) executeToolWithRules(toolName string, args json.RawMessage) tea.Cmd {
	return func() tea.Msg {
		result, err := m.agent.HandleToolCall(toolName, args)
		if err != nil {
			result = fmt.Sprintf("Error: %v", err)
		}
		return []agent.Message{{Role: "tool", ToolID: m.pendingToolCallID, Content: result}}
	}
}

func (m model) permissionDeniedToolResult(toolName string) tea.Cmd {
	return func() tea.Msg {
		return []agent.Message{{Role: "tool", ToolID: m.pendingToolCallID, Content: fmt.Sprintf("denied: tool %q denied by user", toolName)}}
	}
}

func (m *model) lookupToolName(toolID string) string {
	if toolID == "" {
		return ""
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		raw := m.messages[i].raw
		if raw == nil {
			continue
		}
		for _, tc := range raw.ToolCalls {
			if tc.ID == toolID {
				return tc.Function.Name
			}
		}
	}
	return ""
}

func (m model) askAgent() tea.Cmd {
	var agentMsgs []agent.Message
	ctx := agent.LoadContext()
	if ctx != "" {
		agentMsgs = append(agentMsgs, agent.Message{Role: "system", Content: "Context and rules:\n" + ctx})
	}

	for _, msg := range m.messages {
		if msg.raw != nil {
			agentMsgs = append(agentMsgs, *msg.raw)
			continue
		}
		role := "user"
		if msg.role == roleAssistant {
			role = "assistant"
		}
		agentMsgs = append(agentMsgs, agent.Message{
			Role:    role,
			Content: msg.text,
		})
	}

	cancel := make(chan struct{})
	ch := make(chan agent.Message, 16)
	errCh := make(chan error, 1)
	a := m.agent
	go func() {
		a.OnMessage = func(am agent.Message) { ch <- am }
		_, err := a.Step(agentMsgs)
		a.OnMessage = nil
		close(ch)
		errCh <- err
	}()
	return tea.Batch(
		func() tea.Msg { return streamStartedMsg{cancel: cancel} },
		waitStreamEvent(ch, errCh, cancel),
	)
}

func (m model) retryLastLLMError() (tea.Model, tea.Cmd) {
	if m.streaming {
		return m, nil
	}
	if m.agent == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No LLM configured to retry."})
		m.renderTranscript()
		m.viewport.GotoBottom()
		return m, nil
	}
	if m.lastRetryableLLMErr == "" {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No retryable LLM timeout or I/O error."})
		m.renderTranscript()
		m.viewport.GotoBottom()
		return m, nil
	}
	if len(m.messages) > 0 {
		last := m.messages[len(m.messages)-1]
		if last.role == roleAssistant && last.text == m.lastRetryableLLMErr {
			m.messages = m.messages[:len(m.messages)-1]
		}
	}
	m.lastRetryableLLMErr = ""
	m.renderTranscript()
	m.viewport.GotoBottom()
	return m, m.askAgent()
}

func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "connection reset") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "eof")
}

func waitStreamEvent(ch chan agent.Message, errCh chan error, cancel chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-cancel:
			return streamDoneMsg{err: nil}
		case am, ok := <-ch:
			if !ok {
				return streamDoneMsg{err: <-errCh}
			}
			return streamMsgEvent{msg: am, ch: ch, errCh: errCh, cancel: cancel}
		}
	}
}

func (m model) reExecutePendingTool(toolName string) tea.Cmd {
	return func() tea.Msg {
		if m.agent == nil {
			return errorMsg(fmt.Errorf("no agent configured"))
		}
		var agentMsgs []agent.Message
		ctx := agent.LoadContext()
		if ctx != "" {
			agentMsgs = append(agentMsgs, agent.Message{Role: "system", Content: "Context and rules:\n" + ctx})
		}
		for _, msg := range m.messages {
			if msg.raw != nil {
				agentMsgs = append(agentMsgs, *msg.raw)
				continue
			}
			role := "user"
			if msg.role == roleAssistant {
				role = "assistant"
			}
			agentMsgs = append(agentMsgs, agent.Message{
				Role:    role,
				Content: msg.text,
			})
		}
		resp, err := m.agent.Step(agentMsgs)
		if err != nil {
			return errorMsg(err)
		}
		return resp
	}
}

func (m *model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	panelWidth := m.panelWidth()
	innerWidth := panelWidth - 6
	if innerWidth < 1 {
		innerWidth = 1
	}
	m.input.SetWidth(innerWidth)
	m.input.MaxWidth = innerWidth
	m.viewport.SetWidth(innerWidth)
	newHeight := m.height - m.bottomChromeHeight(panelWidth)
	if newHeight < 1 {
		newHeight = 1
	}
	m.viewport.SetHeight(newHeight)
	m.renderTranscript()
}

func (m model) bottomChromeHeight(panelWidth int) int {
	header := m.styles.Header.Render("◆ ocode") + hintStyle.Render("  ·  opencode clone v"+version.Version)
	input := borderStyle.Width(panelWidth - 2).Render(m.input.View())
	status := m.renderStatus()

	height := lipgloss.Height(header)
	height += 2 // transcript border
	height += lipgloss.Height(input)
	if m.showSlashPopup {
		height += lipgloss.Height(m.renderSlashPopup())
	}
	if row := m.renderQueueRow(); row != "" {
		height += lipgloss.Height(row)
	}
	if row := m.renderActivityRow(); row != "" {
		height += lipgloss.Height(row)
	}
	height += lipgloss.Height(status)
	return height
}

func (m *model) renderPalette() string {
	header := m.styles.Header.Render(" > ") + m.paletteInput
	commands := commandNames()
	var results []string
	for _, c := range commands {
		if strings.Contains(c, m.paletteInput) {
			results = append(results, c)
		}
	}

	body := strings.Join(results, "\n")
	return borderStyle.Width(m.width - 2).Render(header + "\n\n" + body)
}

func (m *model) openFullToolOutput(messageIndex int) {
	if messageIndex < 0 || messageIndex >= len(m.messages) {
		return
	}
	msg := m.messages[messageIndex]
	if msg.raw == nil {
		return
	}
	m.showFullToolOutput = true
	toolName := m.lookupToolName(msg.raw.ToolID)
	if toolName == "" {
		toolName = "tool"
	}
	m.fullToolOutputTitle = fmt.Sprintf("%s output", toolName)
	m.fullToolOutput = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.fullToolOutput.SetContent(msg.raw.Content)
	m.layoutFullToolOutput()
}

func (m *model) layoutFullToolOutput() {
	width := m.width - 4
	if width < 1 {
		width = 1
	}
	height := m.height - 4
	if height < 1 {
		height = 1
	}
	m.fullToolOutput.SetWidth(width)
	m.fullToolOutput.SetHeight(height)
}

func (m model) renderFullToolOutput() string {
	header := m.styles.Header.Render("◆ " + m.fullToolOutputTitle)
	help := hintStyle.Render("  esc/b/backspace: back  ·  arrows/mouse: scroll")
	body := borderStyle.Width(m.width - 2).Render(constrainView(m.fullToolOutput.View(), m.fullToolOutput.Width(), m.fullToolOutput.Height()))
	return lipgloss.JoinVertical(lipgloss.Left, header+help, body)
}

func (m model) toolOutputForClick(msg tea.MouseClickMsg) (int, bool) {
	if len(m.toolOutputLineMap) == 0 || m.showFullToolOutput {
		return 0, false
	}
	mouse := msg.Mouse()
	if m.sidebarEnabled() && mouse.X >= m.panelWidth() {
		return 0, false
	}
	innerY := mouse.Y - lipgloss.Height(m.styles.Header.Render("◆ ocode")) - 1
	if innerY < 0 || innerY >= m.viewport.Height() {
		return 0, false
	}
	idx, ok := m.toolOutputLineMap[m.viewport.YOffset()+innerY]
	return idx, ok
}

func (m *model) renderTranscript() {
	if len(m.messages) == 0 {
		return
	}
	var b strings.Builder
	expandableMessages := []int{}
	for i, msg := range m.messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		switch msg.role {
		case roleUser:
			b.WriteString(userStyle.Render("you") + "\n" + msg.text)
		case roleAssistant:
			b.WriteString(assistantStyle.Render("ocode") + "\n" + m.renderAssistantText(msg.text))
			if msg.raw != nil && strings.Contains(msg.text, fullToolOutputMarker) {
				expandableMessages = append(expandableMessages, i)
			}
		}
	}
	wrapped := wrapView(b.String(), m.viewport.Width())
	m.toolOutputLineMap = toolOutputLineMap(wrapped, expandableMessages)
	m.viewport.SetContent(wrapped)
}

func toolOutputLineMap(rendered string, expandableMessages []int) map[int]int {
	if len(expandableMessages) == 0 {
		return nil
	}
	lineMap := map[int]int{}
	next := 0
	for lineNo, line := range strings.Split(rendered, "\n") {
		if !strings.Contains(line, fullToolOutputMarker) || next >= len(expandableMessages) {
			continue
		}
		lineMap[lineNo] = expandableMessages[next]
		next++
	}
	return lineMap
}

func padViewHeight(view string, height int) string {
	if height <= 0 {
		return view
	}
	for lipgloss.Height(view) < height {
		view += "\n"
	}
	return view
}

func constrainView(view string, width int, height int) string {
	if width > 0 {
		view = wrapView(view, width)
	}
	if height > 0 {
		lines := strings.Split(view, "\n")
		if len(lines) > height {
			lines = lines[:height]
		}
		view = strings.Join(lines, "\n")
	}
	return padViewHeight(view, height)
}

func wrapView(view string, width int) string {
	if width <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(line, width, false), "\n")...)
	}
	return strings.Join(wrapped, "\n")
}

func listenActivity(tracker *agent.ActivityTracker) tea.Cmd {
	return func() tea.Msg {
		snap := <-tracker.Notify()
		return activityUpdateMsg{snap: snap}
	}
}

func (m model) renderActivityRow() string {
	if !m.activityRowReserved {
		return ""
	}
	snap := m.lastActivity
	if !snap.LLMRunning && len(snap.ActiveTools) == 0 && len(snap.ActiveAgents) == 0 {
		return m.styles.Status.Width(m.statusContentWidth()).Render("")
	}
	var parts []string
	if snap.LLMRunning {
		parts = append(parts, "⟳ LLM")
	}
	if len(snap.ActiveTools) > 0 {
		parts = append(parts, "⚙ "+strings.Join(snap.ActiveTools, ", "))
	}
	if len(snap.ActiveAgents) > 0 {
		parts = append(parts, "🤖 "+strings.Join(snap.ActiveAgents, ", "))
	}
	return m.styles.Status.Width(m.statusContentWidth()).Render(" " + strings.Join(parts, "  │  "))
}

func (m model) renderAssistantText(text string) string {
	var b strings.Builder
	for {
		start, tagLen := findThinkingStart(text)
		if start < 0 {
			b.WriteString(m.styles.Text.Render(text))
			break
		}
		if start > 0 {
			b.WriteString(m.styles.Text.Render(text[:start]))
		}
		remaining := text[start+tagLen:]
		end, endLen := findThinkingEnd(remaining)
		if end < 0 {
			if m.showThinking {
				b.WriteString(m.styles.Thinking.Render(remaining))
			}
			break
		}
		if m.showThinking {
			b.WriteString(m.styles.Thinking.Render(remaining[:end]))
		}
		text = remaining[end+endLen:]
	}
	return b.String()
}

func findThinkingStart(text string) (int, int) {
	think := strings.Index(text, "<think>")
	thinking := strings.Index(text, "<thinking>")
	if think < 0 {
		if thinking < 0 {
			return -1, 0
		}
		return thinking, len("<thinking>")
	}
	if thinking < 0 || think < thinking {
		return think, len("<think>")
	}
	return thinking, len("<thinking>")
}

func findThinkingEnd(text string) (int, int) {
	think := strings.Index(text, "</think>")
	thinking := strings.Index(text, "</thinking>")
	if think < 0 {
		if thinking < 0 {
			return -1, 0
		}
		return thinking, len("</thinking>")
	}
	if thinking < 0 || think < thinking {
		return think, len("</think>")
	}
	return thinking, len("</thinking>")
}

func (m model) View() tea.View {
	v := tea.NewView(m.renderContent())
	v.AltScreen = true
	if m.config != nil && m.config.TUI.Mouse != nil && *m.config.TUI.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m model) renderContent() string {
	if !m.ready {
		return "initializing…"
	}

	if m.showFullToolOutput {
		return m.renderFullToolOutput()
	}

	if m.showPicker {
		return m.renderPicker()
	}

	if m.showConnect {
		return m.renderConnect()
	}

	if m.showPalette {
		return m.renderPalette()
	}

	header := m.styles.Header.Render("◆ ocode") + hintStyle.Render("  ·  opencode clone v"+version.Version)

	status := m.renderStatus()
	panelWidth := m.panelWidth()
	transcript := borderStyle.Width(panelWidth - 2).Render(constrainView(m.viewport.View(), m.viewport.Width(), m.viewport.Height()))
	input := borderStyle.Width(panelWidth - 2).Render(m.input.View())
	leftParts := []string{header, transcript}
	if m.showSlashPopup {
		leftParts = append(leftParts, m.renderSlashPopup())
	}
	if row := m.renderQueueRow(); row != "" {
		leftParts = append(leftParts, row)
	}
	leftParts = append(leftParts, input)
	if row := m.renderActivityRow(); row != "" {
		leftParts = append(leftParts, row)
	}
	leftParts = append(leftParts, status)
	left := lipgloss.JoinVertical(lipgloss.Left, leftParts...)

	if m.sidebarEnabled() {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, m.renderSidebar())
	}

	return left
}

func (m *model) renderStatus() string {
	agentName := "build"
	specs := agent.DefaultAgents
	if m.currentAgentIdx >= 0 && m.currentAgentIdx < len(specs) {
		agentName = specs[m.currentAgentIdx].Name
	}

	suffix := " | tab: agent | ctrl+p: palette | ctrl+x: leader | ctrl+o: yolo | ctrl+y: retry"
	if m.ctrlCPressed {
		suffix = " | ctrl+c again to quit"
	} else if m.streaming {
		suffix = " | esc: stop"
	}
	llmState := "○ idle"
	if m.streaming || m.lastActivity.LLMRunning {
		llmState = "⟳ running"
	}
	permissionMode := ""
	if m.agent != nil && m.agent.Permissions() != nil && m.agent.Permissions().Mode() == agent.PermissionModeYOLO {
		permissionMode = " | YOLO permissions"
	}
	width := m.statusContentWidth()
	text := fmt.Sprintf(" LLM: %s | Agent: %s | Mode: %s | Model: %s | Session: %s%s%s", llmState, agentName, m.agentModeLabel(), m.currentModelName(), m.sessionID, permissionMode, suffix)
	return m.styles.Status.Width(width).Render(ansi.Truncate(text, width, "..."))
}

func (m model) renderQueueRow() string {
	if len(m.queuedInputs) == 0 {
		return ""
	}
	items := make([]string, 0, len(m.queuedInputs))
	for i, input := range m.queuedInputs {
		label := fmt.Sprintf("%d. %s", i+1, strings.TrimSpace(input))
		items = append(items, ansi.Truncate(label, 48, "..."))
	}
	text := fmt.Sprintf(" Queued (%d): %s", len(m.queuedInputs), strings.Join(items, " | "))
	return m.styles.Status.Width(m.statusContentWidth()).Render(text)
}

func (m model) statusContentWidth() int {
	width := m.panelWidth() - 2
	if width < 1 {
		return 1
	}
	return width
}

type sidebarTelemetry struct {
	promptTokens     int64
	completionTokens int64
	totalTokens      int64
	spend            *float64
}

type sidebarRenderData struct {
	lines     []string
	fileLines map[int]string
}

func (t sidebarTelemetry) usedTokens() int64 {
	if t.totalTokens > 0 {
		return t.totalTokens
	}
	return t.promptTokens + t.completionTokens
}

func (t *sidebarTelemetry) addMessage(msg agent.Message) {
	messageTotal := int64(0)
	if msg.Usage != nil {
		if msg.Usage.PromptTokens != nil {
			t.promptTokens += *msg.Usage.PromptTokens
			messageTotal += *msg.Usage.PromptTokens
		}
		if msg.Usage.CompletionTokens != nil {
			t.completionTokens += *msg.Usage.CompletionTokens
			messageTotal += *msg.Usage.CompletionTokens
		}
		if msg.Usage.TotalTokens != nil {
			messageTotal = *msg.Usage.TotalTokens
		}
		t.totalTokens += messageTotal
	}
	if msg.Spend != nil {
		if t.spend == nil {
			t.spend = new(float64)
		}
		*t.spend += *msg.Spend
	}
}

func (t sidebarTelemetry) metadata() map[string]any {
	if t.promptTokens == 0 && t.completionTokens == 0 && t.totalTokens == 0 && t.spend == nil {
		return nil
	}
	meta := map[string]any{
		"prompt_tokens":     t.promptTokens,
		"completion_tokens": t.completionTokens,
		"total_tokens":      t.totalTokens,
	}
	if t.spend != nil {
		meta["spend"] = *t.spend
	}
	return meta
}

func (m model) sessionSidebarMetadata() map[string]any {
	meta := m.sessionTelemetry.metadata()
	todo := tool.TodoState()
	if todo != "" {
		if meta == nil {
			meta = make(map[string]any)
		}
		meta["todo_text"] = todo
	}
	return meta
}

func restoreTodoState(meta map[string]any) {
	if len(meta) == 0 {
		return
	}
	if v, ok := meta["todo_text"]; ok {
		if s, ok := v.(string); ok {
			tool.SetTodoState(s)
		}
	}
}

func telemetryFromSessionMetadata(meta map[string]any) sidebarTelemetry {
	if len(meta) == 0 {
		return sidebarTelemetry{}
	}

	var telemetry sidebarTelemetry
	if v, ok := meta["prompt_tokens"]; ok {
		telemetry.promptTokens = int64FromAny(v)
	}
	if v, ok := meta["completion_tokens"]; ok {
		telemetry.completionTokens = int64FromAny(v)
	}
	if v, ok := meta["total_tokens"]; ok {
		telemetry.totalTokens = int64FromAny(v)
	}
	if v, ok := meta["spend"]; ok {
		if f, ok := float64FromAny(v); ok {
			telemetry.spend = &f
		}
	}
	return telemetry
}

func int64FromAny(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func float64FromAny(v any) (float64, bool) {
	switch n := v.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func aggregateSidebarTelemetry(messages []message) sidebarTelemetry {
	var telemetry sidebarTelemetry
	for _, msg := range messages {
		if msg.raw == nil {
			continue
		}
		telemetry.addMessage(*msg.raw)
	}
	return telemetry
}

func modelContextWindow(modelName string) (int64, bool) {
	switch modelName {
	case "gpt-4o", "gpt-4o-mini", "o1-preview":
		return 128000, true
	case "claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307":
		return 200000, true
	case "gemini-1.5-pro":
		return 1048576, true
	case "gemini-1.5-flash":
		return 1000000, true
	default:
		return 0, false
	}
}

func formatPercent(used, total int64) string {
	if total <= 0 {
		return "0%"
	}
	percent := float64(used) / float64(total) * 100
	return fmt.Sprintf("%.1f%%", percent)
}

func (m model) buildSidebarRenderData() sidebarRenderData {
	data := sidebarRenderData{fileLines: map[int]string{}}
	appendSection := func(title string, body []string, filePaths []string) {
		if len(data.lines) > 0 {
			data.lines = append(data.lines, "")
		}
		data.lines = append(data.lines, title)
		for i, line := range body {
			data.lines = append(data.lines, line)
			if i < len(filePaths) {
				data.fileLines[len(data.lines)] = filePaths[i]
			}
		}
	}

	telemetry := m.sessionTelemetry
	if telemetry.usedTokens() == 0 && telemetry.spend == nil {
		telemetry = aggregateSidebarTelemetry(m.messages)
	}
	modelName := m.currentModelName()

	contextLine := "n/a"
	if used := telemetry.usedTokens(); used > 0 {
		if window, ok := modelContextWindow(modelName); ok {
			contextLine = fmt.Sprintf("%s / %s (%s)", strconv.FormatInt(used, 10), strconv.FormatInt(window, 10), formatPercent(used, window))
		} else {
			contextLine = fmt.Sprintf("%s tokens", strconv.FormatInt(used, 10))
		}
	}

	spendLine := "n/a"
	if telemetry.spend != nil {
		spendLine = fmt.Sprintf("$%.4f", *telemetry.spend)
	}

	projectDir := shortenSidebarPath(shortenWorkingDir(m.workDir), sidebarColumnWidth-4)
	appendSection("Project", []string{projectDir}, nil)
	appendSection("Session", []string{m.sessionID}, nil)
	appendSection("Model", []string{modelName}, nil)
	appendSection("Context", []string{contextLine}, nil)
	appendSection("Spend", []string{spendLine}, nil)
	appendSection("MCP", []string{m.renderMCPStatus()}, nil)
	appendSection("LSP", []string{m.renderLSPStatus()}, nil)

	changed := snapshot.ChangedFiles()
	if len(changed) == 0 {
		appendSection("Files", []string{"No changed files yet."}, nil)
	} else {
		body := make([]string, 0, len(changed))
		for _, path := range changed {
			body = append(body, "- "+shortenSidebarPath(path, sidebarColumnWidth-4))
		}
		appendSection("Files", body, changed)
	}

	todo := tool.TodoState()
	if todo == "" {
		appendSection("TODO", []string{"No live session todo state yet."}, nil)
	} else {
		raw := strings.Split(todo, "\n")
		styled := make([]string, len(raw))
		for i, line := range raw {
			styled[i] = styleTodoLine(line)
		}
		appendSection("TODO", styled, nil)
	}

	appendSection("Hints", []string{"Ctrl+B toggle sidebar", "/sidebar toggle sidebar", "Ctrl+P command palette", "Ctrl+X leader actions", "Ctrl+O toggle YOLO", "Ctrl+Y retry LLM timeout/I/O"}, nil)
	return data
}

func (m model) renderSidebar() string {
	data := m.buildSidebarRenderData()
	maxLines := m.height - 2 // account for border
	if maxLines < 1 {
		maxLines = 1
	}
	lines := data.lines
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	sections := strings.Join(lines, "\n")
	return borderStyle.Width(sidebarColumnWidth).Render(sections)
}

func (m model) sidebarFileForClick(msg tea.MouseClickMsg) (string, bool) {
	if !m.sidebarEnabled() || msg.Button != tea.MouseLeft {
		return "", false
	}
	mouse := msg.Mouse()
	if mouse.X < m.panelWidth() {
		return "", false
	}

	data := m.buildSidebarRenderData()
	for line, path := range data.fileLines {
		if mouse.Y == line {
			return path, true
		}
	}
	return "", false
}

func shortenWorkingDir(dir string) string {
	if dir == "" {
		return "(no project)"
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(dir, home) {
		rel := "~" + dir[len(home):]
		if rel == "~" {
			return "~/"
		}
		return rel
	}
	return dir
}

func shortenSidebarPath(path string, max int) string {
	if len(path) <= max {
		return path
	}
	if max <= 3 {
		return path[:max]
	}
	return path[:max-3] + "..."
}

func (m model) renderMCPStatus() string {
	enabled := 0
	if m.config != nil {
		for _, cfg := range m.config.MCP {
			if cfg.Enabled {
				enabled++
			}
		}
	}

	if enabled == 0 {
		return "disabled"
	}

	loaded := 0
	if m.agent != nil {
		loaded = m.agent.MCPToolCount()
	}

	if m.agent != nil {
		errs := m.agent.MCPErrors()
		if len(errs) > 0 {
			return fmt.Sprintf("%d configured, %d loaded, %d errors", enabled, loaded, len(errs))
		}
	}

	if loaded > 0 {
		return fmt.Sprintf("%d configured, %d loaded", enabled, loaded)
	}
	return fmt.Sprintf("%d configured", enabled)
}

func (m model) renderLSPStatus() string {
	if m.agent == nil {
		return "unavailable"
	}

	for _, tool := range m.agent.GetTools() {
		if tool.Name() == "lsp" {
			return "available"
		}
	}

	return "unavailable"
}
