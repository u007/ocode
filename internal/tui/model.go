package tui

import (
	"fmt"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/config"
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
}

type model struct {
	viewport viewport.Model
	input    textarea.Model
	messages []message
	agent    *agent.Agent
	config   *config.Config
	width    int
	height   int
	ready    bool
	err      error
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

func newModel() model {
	cfg, _ := config.Load()

	// Initialize tools
	tools := []tool.Tool{
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
	}

	var a *agent.Agent
	if cfg != nil && cfg.Model != "" {
		client := agent.NewClient("", cfg.Model)
		a = agent.NewAgent(client, tools)
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

	return model{
		viewport: vp,
		input:    ta,
		messages: []message{},
		config:   cfg,
		agent:    a,
	}
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
			m.messages = append(m.messages,
				message{role: roleUser, text: text},
			)
			m.input.Reset()
			m.renderTranscript()
			m.viewport.GotoBottom()

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
			if am.Role == "assistant" {
				if len(am.ToolCalls) > 0 {
					for _, tc := range am.ToolCalls {
						m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("🔧 calling %s(%s)...", tc.Function.Name, tc.Function.Arguments)})
					}
				}
				if am.Content != "" {
					m.messages = append(m.messages, message{role: roleAssistant, text: am.Content})
				}
			} else if am.Role == "tool" {
				m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("✅ tool result: %s", am.Content)})
			}
		}
		m.renderTranscript()
		m.viewport.GotoBottom()
	case errorMsg:
		m.err = msg
	}

	return m, tea.Batch(tiCmd, vpCmd)
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
			role := "user"
			if msg.role == roleAssistant {
				role = "assistant"
			}
			agentMsgs = append(agentMsgs, agent.Message{Role: role, Content: msg.text})
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
