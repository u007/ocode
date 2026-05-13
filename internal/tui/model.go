package tui

import (
	"fmt"
	"os"
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
	viewport  viewport.Model
	input     textarea.Model
	messages  []message
	agent     *agent.Agent
	config    *config.Config
	sessionID string
	width     int
	height    int
	ready     bool
	err       error
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

func (m *model) getInitialTools() []tool.Tool {
	return []tool.Tool{
		tool.ReadTool{},
		tool.WriteTool{},
		tool.GlobTool{},
		tool.GrepTool{},
		tool.BashTool{},
		tool.EditTool{},
		tool.ApplyPatchTool{},
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
		ids, _ := session.List()
		if len(ids) > 0 {
			sid = ids[len(ids)-1] // latest
		}
	}

	tmp := model{}
	tools := tmp.getInitialTools()

	var a *agent.Agent
	if cfg != nil && cfg.Model != "" {
		client := agent.NewClient(cfg, cfg.Model)
		a = agent.NewAgent(client, tools)
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
		viewport:  vp,
		input:     ta,
		messages:  []message{},
		config:    cfg,
		agent:     a,
		sessionID: sid,
	}

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
		switch msg.Type {
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
				m.messages = append(m.messages,
					message{role: roleUser, text: text},
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
	case "/connect":
		m.handleConnectCmd(args)
	case "/session":
		m.handleSessionCmd(args)
	case "/compact":
		m.handleCompactCmd(args)
	case "/undo":
		m.handleUndoCmd(args)
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
			m.agent = agent.NewAgent(client, tools)
		}
	}
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
		ids, _ := session.List()
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for _, id := range ids {
			b.WriteString(fmt.Sprintf("- %s\n", id))
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
		session.Save(m.sessionID, agentMsgs)
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
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress Ctrl+C to quit", m.err)
	}
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7DCFFF")).
		Bold(true).
		Render("◆ ocode") + hintStyle.Render("  ·  opencode clone")

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		borderStyle.Width(m.width-2).Render(m.viewport.View()),
		borderStyle.Width(m.width-2).Render(m.input.View()),
	)
}
