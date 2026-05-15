package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
	"github.com/jamesmercstudio/ocode/internal/tool"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type authFinishedMsg struct {
	token string
	err   error
}

type fileSearchFinishedMsg struct {
	processedText string
	messages      []message
	err           error
}

type leaderTimeoutMsg struct {
	seq int
}

type model struct {
	viewport         viewport.Model
	input            textarea.Model
	messages         []message
	agent            *agent.Agent
	config           *config.Config
	sessionID        string
	showThinking     bool
	showDetails      bool
	leaderActive     bool
	leaderSeq        int
	showPalette      bool
	showSidebar      bool
	sessionTelemetry sidebarTelemetry
	activeModel      string
	paletteInput     string
	width            int
	height           int
	ready            bool
	err              error
	scrollSpeed      int
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
)

const (
	sidebarMinWidth    = 120
	sidebarColumnWidth = 38
)

func (m *model) applyTheme() {
	if m.config == nil || m.config.TUI.Theme == "" {
		return
	}

	switch m.config.TUI.Theme {
	case "tokyonight":
		userStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
		assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#bb9af7")).Bold(true)
		borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3b4261")).
			Padding(0, 1)
		hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89")).Italic(true)
	case "opencode":
		userStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00")).Bold(true)
		assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ffff")).Bold(true)
		borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444444")).
			Padding(0, 1)
		hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Italic(true)
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
		&tool.WriteTool{},
		&tool.DeleteTool{},
		&tool.GlobTool{},
		&tool.GrepTool{},
		&tool.BashTool{},
		&tool.EditTool{},
		&tool.MultiEditTool{},
		&tool.PatchTool{},
		&tool.TodoWriteTool{},
		&tool.SkillTool{},
		&tool.QuestionTool{},
		&tool.WebFetchTool{},
		&tool.WebSearchTool{},
		&tool.ListTool{},
		&tool.LSPTool{},
	}
}

func newModel(sid string, cont bool) model {
	cfg, _ := config.Load()

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
		a.LoadExternalTools(cfg)
	}

	ta := textarea.New()
	ta.Placeholder = "Ask anything…  (enter to send, ctrl+c to quit)"
	ta.Focus()
	ta.Prompt = "▍ "
	ta.CharLimit = 8000
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()

	vp := viewport.New(80, 20)
	vp.SetContent(hintStyle.Render("  ocode — opencode clone · type a message to begin\n"))

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
		activeModel:  func() string { if cfg != nil { return cfg.Model }; return "" }(),
		scrollSpeed:  3,
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
				role := roleUser
				if am.Role == "assistant" || am.Role == "tool" {
					role = roleAssistant
				}
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
	case tea.MouseMsg:
		if msg.Type == tea.MouseLeft {
			if path, ok := m.sidebarFileForClick(msg); ok {
				return m, openSidebarFileInEditor(path)
			}
		}
		if msg.Type == tea.MouseWheelUp {
			m.viewport.LineUp(m.scrollSpeed)
			return m, nil
		}
		if msg.Type == tea.MouseWheelDown {
			m.viewport.LineDown(m.scrollSpeed)
			return m, nil
		}
	}

	m.input, tiCmd = m.input.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.ready = true
	case tea.KeyMsg:
		if m.showPalette {
			if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlP {
				m.showPalette = false
				return m, nil
			}
			if msg.Type == tea.KeyEnter {
				m.showPalette = false
				return m.handleCommand(m.paletteInput)
			}
			if msg.Type == tea.KeyBackspace {
				if len(m.paletteInput) > 0 {
					m.paletteInput = m.paletteInput[:len(m.paletteInput)-1]
				}
				return m, nil
			}
			m.paletteInput += msg.String()
			return m, nil
		}

		if m.leaderActive {
			m.leaderActive = false

			key := msg.String()
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
				return m.handleCommand("/session list")
			case "c":
				return m.handleCommand("/compact")
			case "q":
				return m, tea.Quit
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyCtrlP:
			m.showPalette = !m.showPalette
			m.paletteInput = ""
			return m, nil
		case tea.KeyCtrlB:
			m.toggleSidebar()
			return m, nil
		case tea.KeyCtrlX:
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
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyTab:
			current := m.input.Value()
			if !strings.HasPrefix(current, "/") {
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
		case tea.KeyEnter:
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, tea.Batch(tiCmd, vpCmd)
			}

			if strings.HasPrefix(text, "/") {
				return m.handleCommand(text)
			}

			if strings.HasPrefix(text, "!") {
				m.input.Reset()
				cmdText := strings.TrimPrefix(text, "!")
				m.messages = append(m.messages, message{role: roleUser, text: text})
				m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("🔧 calling bash(%s)...", cmdText)})
				m.renderTranscript()
				m.viewport.GotoBottom()
				return m, func() tea.Msg {
					bash := tool.BashTool{}
					args, _ := json.Marshal(map[string]string{"command": cmdText})
					res, err := bash.Execute(args)
					if err != nil {
						res = fmt.Sprintf("Error: %v", err)
					}
					return []agent.Message{{Role: "tool", Content: res}}
				}
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
		m.viewport.GotoBottom()
	case []agent.Message:
		for _, am := range msg {
			copyMsg := am
			if am.Role == "assistant" {
				if len(am.ToolCalls) > 0 {
					m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("🔧 calling %d tools...", len(am.ToolCalls)), raw: &copyMsg})
				} else if am.Content != "" {
					m.messages = append(m.messages, message{role: roleAssistant, text: am.Content, raw: &copyMsg})
				}
			} else if am.Role == "tool" {
				m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("✅ tool result: %s", am.Content), raw: &copyMsg})
			}
			if am.Usage != nil || am.Spend != nil {
				m.sessionTelemetry.addMessage(am)
			}
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
			if !stop {
				return m, m.askAgent()
			}
		}
	case editorFinishedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Editor error: %v", msg.err)})
		} else {
			m.input.SetValue(msg.content)
		}
	case errorMsg:
		m.err = msg
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
	if spec == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown command: %s", cmd)})
	} else {
		cmdResult = spec.handler(m, args)
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
		var b strings.Builder
		b.WriteString("Provider status:\n")
		providers := []string{"openai", "anthropic", "google", "zai", "zai-coding", "openrouter", "moonshot", "minimax", "alibaba", "alibaba-coding", "chutes"}
		for _, p := range providers {
			status := "❌ disconnected"
			envVar := ""
			switch p {
			case "openai":
				envVar = "OPENAI_API_KEY"
			case "anthropic":
				envVar = "ANTHROPIC_API_KEY"
			case "openrouter":
				envVar = "OPENROUTER_API_KEY"
			}
			if envVar != "" && os.Getenv(envVar) != "" {
				status = "✅ connected"
			}
			b.WriteString(fmt.Sprintf("- %s: %s\n", p, status))
		}
		b.WriteString("\nUsage: /connect <provider> <apikey>")
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
	} else if len(args) == 2 {
		provider := args[0]
		key := args[1]

		envVar := ""
		switch provider {
		case "openai":
			envVar = "OPENAI_API_KEY"
		case "anthropic":
			envVar = "ANTHROPIC_API_KEY"
		case "openrouter":
			envVar = "OPENROUTER_API_KEY"
		case "google":
			envVar = "GOOGLE_API_KEY"
		}

		if envVar != "" {
			os.Setenv(envVar, key)
			// Show only a masked version of the key to avoid it lingering in scroll-back.
			masked := maskKey(key)
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf(
				"API key for %s set (%s). Note: the key is held in memory only and will not persist after restart.", provider, masked,
			)})

			if m.config != nil && m.config.Model != "" {
				client := agent.NewClient(m.config, m.config.Model)
				m.agent = agent.NewAgent(client, m.getInitialTools(), m.config)
			}
		} else {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Provider %s not natively supported for direct key setting yet.", provider)})
		}
	}
}

func (m *model) handleSessionCmd(args []string) {
	if len(args) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Active session: %s\nUse '/session list' to see all sessions.", m.sessionID)})
	} else if args[0] == "list" {
		sessions, _ := session.List()
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for _, s := range sessions {
			title := s.Title
			if title == "" {
				title = "(no title)"
			}
			b.WriteString(fmt.Sprintf("- %s: %s\n", s.ID, title))
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
	} else if args[0] == "load" && len(args) > 1 {
		sess, err := session.Load(args[1])
		if err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error loading session: %v", err)})
		} else {
			m.sessionID = args[1]
			tool.SetTodoSession(m.sessionID)
			snapshot.Reset()
			tool.ResetTodoState()
			m.sessionTelemetry = telemetryFromSessionMetadata(sess.Metadata)
			restoreTodoState(sess.Metadata)
			m.messages = []message{}
			for _, am := range sess.Messages {
				role := roleUser
				if am.Role == "assistant" || am.Role == "tool" {
					role = roleAssistant
				}
				copyMsg := am
				m.messages = append(m.messages, message{role: role, text: am.Content, raw: &copyMsg})
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Loaded session %s", m.sessionID)})
		}
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /session [list|load <id>]"})
	}
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
	m.messages = []message{}
	m.sessionID = time.Now().Format("2006-01-02-150405")
	tool.SetTodoSession(m.sessionID)
	snapshot.Reset()
	tool.ResetTodoState()
	m.sessionTelemetry = sidebarTelemetry{}
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
		m.messages = append(m.messages, message{role: roleAssistant, text: "Available themes:\n- opencode\n- tokyonight\nUse '/themes <name>' to switch."})
		return
	}

	m.config.TUI.Theme = args[0]
	m.applyTheme()
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Theme switched to %s", args[0])})
}

func (m *model) handleModelsCmd(args []string) {
	provider := "openai"
	if m.agent != nil {
		provider = m.agent.GetProvider()
	}
	list := providerModels(provider)
	if len(list) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("No model list for provider %s", provider)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Available models for %s:\n- %s", provider, strings.Join(list, "\n- "))})
	}
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

func (m *model) handleUndoCmd(args []string) {
	path, err := snapshot.Undo()
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error undoing: %v", err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Successfully reverted changes to %s", path)})
	}
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

func (m model) askAgent() tea.Cmd {
	return func() tea.Msg {
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

	inputHeight := 5
	headerHeight := 2
	panelWidth := m.panelWidth()
	innerWidth := panelWidth - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	m.input.SetWidth(innerWidth)
	m.viewport.Width = innerWidth
	m.viewport.Height = m.height - inputHeight - headerHeight - 2
	if m.viewport.Height < 1 {
		m.viewport.Height = 1
	}
	m.renderTranscript()
}

func (m *model) renderPalette() string {
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true).Render(" > ") + m.paletteInput
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

func (m *model) renderTranscript() {
	if len(m.messages) == 0 {
		return
	}
	var b strings.Builder
	for i, msg := range m.messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		switch msg.role {
		case roleUser:
			b.WriteString(userStyle.Render("you") + "\n" + msg.text)
		case roleAssistant:
			b.WriteString(assistantStyle.Render("ocode") + "\n" + msg.text)
		}
	}
	m.viewport.SetContent(b.String())
}

func (m model) View() string {
	if !m.ready {
		return "initializing…"
	}

	if m.showPalette {
		return m.renderPalette()
	}

	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress Ctrl+C to quit", m.err)
	}
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7DCFFF")).
		Bold(true).
		Render("◆ ocode") + hintStyle.Render("  ·  opencode clone")

	status := m.renderStatus()
	panelWidth := m.panelWidth()
	transcript := borderStyle.Width(panelWidth - 2).Render(m.viewport.View())
	input := borderStyle.Width(panelWidth - 2).Render(m.input.View())
	left := lipgloss.JoinVertical(lipgloss.Left,
		header,
		transcript,
		input,
		status,
	)

	if m.sidebarEnabled() {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, m.renderSidebar())
	}

	return left
}

func (m *model) renderStatus() string {
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#565F89")).
		Background(lipgloss.Color("#1A1B26")).
		Padding(0, 1)

	return statusStyle.Width(m.panelWidth()).Render(
		fmt.Sprintf(" Model: %s | Session: %s | ctrl+p: palette | ctrl+x: leader", m.currentModelName(), m.sessionID),
	)
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
		appendSection("TODO", strings.Split(todo, "\n"), nil)
	}

	appendSection("Hints", []string{"Ctrl+B toggle sidebar", "/sidebar toggle sidebar", "Ctrl+P command palette", "Ctrl+X leader actions"}, nil)
	return data
}

func (m model) renderSidebar() string {
	data := m.buildSidebarRenderData()
	sections := strings.Join(data.lines, "\n")
	return borderStyle.Width(sidebarColumnWidth).Render(sections)
}

func (m model) sidebarFileForClick(msg tea.MouseMsg) (string, bool) {
	if !m.sidebarEnabled() || msg.Type != tea.MouseLeft {
		return "", false
	}
	if msg.X < m.panelWidth() {
		return "", false
	}

	data := m.buildSidebarRenderData()
	for line, path := range data.fileLines {
		if msg.Y == line {
			return path, true
		}
	}
	return "", false
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
