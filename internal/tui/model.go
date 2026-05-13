package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/tool"
	"github.com/jamesmercstudio/ocode/internal/snapshot"

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

type model struct {
	viewport      viewport.Model
	input         textarea.Model
	messages      []message
	agent         *agent.Agent
	config        *config.Config
	sessionID     string
	showThinking  bool
	showDetails   bool
	leaderActive  bool
	leaderTimer   *time.Timer
	showPalette   bool
	paletteInput  string
	width         int
	height        int
	ready         bool
	err           error
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

func (m *model) getInitialTools() []tool.Tool {
	return []tool.Tool{
		tool.ReadTool{},
		tool.WriteTool{},
		tool.DeleteTool{},
		tool.GlobTool{},
		tool.GrepTool{},
		tool.BashTool{},
		tool.EditTool{},
		tool.MultiEditTool{},
		tool.PatchTool{},
		tool.TodoWriteTool{},
		tool.SkillTool{},
		tool.QuestionTool{},
		tool.WebFetchTool{},
		tool.WebSearchTool{},
		tool.LSPTool{},
	}
}

func newModel(sid string, cont bool) model {
	cfg, _ := config.Load()

	if cont {
		sessions, _ := session.List()
		if len(sessions) > 0 {
			sid = sessions[len(sessions)-1].ID // latest
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

	m := model{
		viewport:      vp,
		input:         ta,
		messages:      []message{},
		config:        cfg,
		agent:         a,
		sessionID:     sid,
		showThinking:  true,
	}

	m.applyTheme()

	if sid != "" {
		msgs, err := session.Load(sid)
		if err == nil {
			for _, am := range msgs {
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
			if m.leaderTimer != nil { m.leaderTimer.Stop() }

			key := msg.String()
			// Check custom keybinds first
			if m.config != nil {
				if cmd, ok := m.config.TUI.Keybinds[key]; ok {
					return m.handleCommand(cmd)
				}
			}

			switch key {
			case "u": return m.handleCommand("/undo")
			case "r": return m.handleCommand("/redo")
			case "n": return m.handleCommand("/new")
			case "l": return m.handleCommand("/session list")
			case "c": return m.handleCommand("/compact")
			case "q": return m, tea.Quit
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyCtrlP:
			m.showPalette = !m.showPalette
			m.paletteInput = ""
			return m, nil
		case tea.KeyCtrlX:
			m.leaderActive = true
			timeout := 2000
			if m.config != nil && m.config.TUI.LeaderTimeout != 0 { timeout = m.config.TUI.LeaderTimeout }
			m.leaderTimer = time.AfterFunc(time.Duration(timeout)*time.Millisecond, func() {
				// We can't easily trigger a tea.Msg from here without a handle
				// But for now, simple timeout is fine.
			})
			return m, nil
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
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

			// Check if we are answering a question
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
			} else {
				// Handle file references (@path) with fuzzy matching
				re := regexp.MustCompile(`@([^\s]+)`)
				matches := re.FindAllStringSubmatch(text, -1)
				processedText := text
				for _, match := range matches {
					path := match[1]

					// Simple fuzzy matching by walking
					foundPath := ""
					filepath.Walk(".", func(p string, info os.FileInfo, err error) error {
						if foundPath != "" || info.IsDir() { return nil }
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
						m.messages = append(m.messages, message{
							role: roleAssistant,
							text: fmt.Sprintf("📎 Added context from %s", path),
							raw: &agent.Message{
								Role:    "system",
								Content: fileCtx,
							},
						})
					}
				}

				m.messages = append(m.messages,
					message{role: roleUser, text: processedText},
				)
			}
			m.input.Reset()
			m.renderTranscript()
			m.viewport.GotoBottom()
			m.saveSession()

			if m.agent != nil {
				return m, m.askAgent()
			} else {
				m.messages = append(m.messages, message{role: roleAssistant, text: hintStyle.Render("(no llm configured, check opencode.json)")})
				m.renderTranscript()
				m.viewport.GotoBottom()
			}
		}
	case []agent.Message:
		for _, am := range msg {
			copyMsg := am // copy
			if am.Role == "assistant" {
				if len(am.ToolCalls) > 0 {
					m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("🔧 calling %d tools...", len(am.ToolCalls)), raw: &copyMsg})
				} else if am.Content != "" {
					m.messages = append(m.messages, message{role: roleAssistant, text: am.Content, raw: &copyMsg})
				}
			} else if am.Role == "tool" {
				m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("✅ tool result: %s", am.Content), raw: &copyMsg})
			}
		}
		m.renderTranscript()
		m.viewport.GotoBottom()
		m.saveSession()
	case errorMsg:
		m.err = msg
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m *model) handleCommand(text string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(text)
	cmd := parts[0]
	args := parts[1:]
	m.input.Reset()

	switch cmd {
	case "/model":
		m.handleModelCmd(args)
	case "/thinking":
		m.handleThinkingCmd(args)
	case "/connect":
		m.handleConnectCmd(args)
	case "/session":
		m.handleSessionCmd(args)
	case "/compact":
		m.handleCompactCmd(args)
	case "/undo":
		m.handleUndoCmd(args)
	case "/help":
		m.handleHelpCmd(args)
	case "/redo":
		m.handleRedoCmd(args)
	case "/export":
		m.handleExportCmd(args)
	case "/new", "/clear":
		m.handleNewCmd(args)
	case "/editor":
		m.handleEditorCmd(args)
	case "/exit", "/quit", "/q":
		return m, tea.Quit
	case "/themes":
		m.handleThemesCmd(args)
	case "/share":
		m.handleShareCmd(args)
	case "/models":
		m.handleModelsCmd(args)
	case "/details":
		m.handleDetailsCmd(args)
	case "/init":
		m.handleInitCmd(args)
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown command: %s", cmd)})
	}

	m.renderTranscript()
	m.viewport.GotoBottom()
	return m, nil
}

func (m *model) handleModelCmd(args []string) {
	if len(args) > 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Switching to model %s", args[0])})
		client := agent.NewClient(m.config, args[0])
		if client != nil {
			var tools []tool.Tool
			if m.agent != nil {
				tools = m.agent.GetTools()
			} else {
				tools = m.getInitialTools()
			}
			m.agent = agent.NewAgent(client, tools, m.config)
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

func (m *model) handleConnectCmd(args []string) {
	var b strings.Builder
	b.WriteString("Provider status:\n")
	providers := []string{"openai", "anthropic", "google", "zai", "zai-coding", "openrouter", "moonshot", "minimax", "alibaba", "alibaba-coding", "chutes"}
	for _, p := range providers {
		status := "❌ disconnected"
		envVar := ""
		switch p {
		case "openai": envVar = "OPENAI_API_KEY"
		case "anthropic": envVar = "ANTHROPIC_API_KEY"
		case "openrouter": envVar = "OPENROUTER_API_KEY"
		}
		if envVar != "" && os.Getenv(envVar) != "" {
			status = "✅ connected"
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", p, status))
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
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
			if title == "" { title = "(no title)" }
			b.WriteString(fmt.Sprintf("- %s: %s\n", s.ID, title))
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
	} else if args[0] == "load" && len(args) > 1 {
		msgs, err := session.Load(args[1])
		if err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error loading session: %v", err)})
		} else {
			m.sessionID = args[1]
			m.messages = []message{}
			for _, am := range msgs {
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
		if msg.role == roleAssistant { role = "Assistant" }
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
	m.messages = append(m.messages, message{role: roleAssistant, text: "Started new session."})
}

func (m *model) handleEditorCmd(args []string) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Error: EDITOR environment variable not set."})
		return
	}

	tmpFile, err := os.CreateTemp("", "ocode-msg-*.txt")
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error creating temp file: %v", err)})
		return
	}
	defer os.Remove(tmpFile.Name())

	content := m.input.Value()
	tmpFile.Write([]byte(content))
	tmpFile.Close()

	cmdParts := strings.Fields(editor)
	cmdParts = append(cmdParts, tmpFile.Name())
	cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// This will suspend Bubble Tea
	// But in this environment we can't easily wait for a subprocess that needs terminal access
	// Let's return a message for now
	m.messages = append(m.messages, message{role: roleAssistant, text: "Editor launched (simulated). In a real terminal, this would open your editor."})
}

func (m *model) handleShareCmd(args []string) {
	filename := fmt.Sprintf("ocode_share_%s.md", m.sessionID)
	var b strings.Builder
	b.WriteString("# Shared OpenCode Session\n\n")
	for _, msg := range m.messages {
		role := "User"
		if msg.role == roleAssistant { role = "Assistant" }
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

	models := map[string][]string{
		"openai":    {"gpt-4o", "gpt-4o-mini", "o1-preview"},
		"anthropic": {"claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307"},
		"google":    {"gemini-1.5-pro", "gemini-1.5-flash"},
	}

	list := models[provider]
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
	help := `Available Commands:
/model <name>  : Switch LLM model
/connect       : Show provider connection status
/session <cmd> : Manage sessions (list, load <id>)
/compact       : Reduce context size by removing tool history
/undo          : Revert last file change
/redo          : Restore last undone change
/export        : Save chat as Markdown
/new           : Start a fresh session
/thinking      : Toggle visibility of agent thoughts
/models        : List recommended models for active provider
/details       : Toggle tool execution details
/init          : Create default AGENTS.md
/help          : Show this help

Shortcuts:
!command       : Run a shell command
@path          : Add file content to context
Ctrl+P         : Open command palette
Ctrl+X         : Leader key for quick actions (u:undo, r:redo, n:new, l:list, c:compact)
`
	m.messages = append(m.messages, message{role: roleAssistant, text: help})
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
		session.Save(m.sessionID, "", agentMsgs)
	}
}

func (m model) askAgent() tea.Cmd {
	return func() tea.Msg {
		var agentMsgs []agent.Message

		// Add context as system message
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
	inputHeight := 5
	headerHeight := 2
	m.input.SetWidth(m.width - 4)
	m.viewport.Width = m.width - 4
	m.viewport.Height = m.height - inputHeight - headerHeight - 2
	m.renderTranscript()
}

func (m *model) renderPalette() string {
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true).Render(" > ") + m.paletteInput
	commands := []string{"/model", "/connect", "/session", "/compact", "/undo", "/redo", "/export", "/new", "/thinking", "/models", "/details", "/init"}
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

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		borderStyle.Width(m.width-2).Render(m.viewport.View()),
		borderStyle.Width(m.width-2).Render(m.input.View()),
		status,
	)
}

func (m *model) renderStatus() string {
	modelName := "no model"
	if m.agent != nil {
		modelName = m.config.Model
	}

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#565F89")).
		Background(lipgloss.Color("#1A1B26")).
		Padding(0, 1)

	return statusStyle.Width(m.width).Render(
		fmt.Sprintf(" Model: %s | Session: %s | ctrl+p: palette | ctrl+x: leader", modelName, m.sessionID),
	)
}
