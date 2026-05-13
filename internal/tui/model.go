package tui

import (
	"strings"

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
	width    int
	height   int
	ready    bool
}

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
				message{role: roleAssistant, text: hintStyle.Render("(no llm wired yet)")},
			)
			m.input.Reset()
			m.renderTranscript()
			m.viewport.GotoBottom()
		}
	}

	return m, tea.Batch(tiCmd, vpCmd)
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
