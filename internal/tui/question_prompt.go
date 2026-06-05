package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

const questionOtherLabel = "Something else"

func parseQuestionPrompt(content string) ([]tool.QuestionPrompt, bool) {
	_, payload, found := strings.Cut(content, tool.SentinelQuestionPrompt)
	if !found {
		return nil, false
	}
	payload = strings.TrimSpace(payload)
	if wait := strings.Index(payload, tool.SentinelWaitingForUser); wait >= 0 {
		payload = strings.TrimSpace(payload[:wait])
	}
	if payload == "" {
		return nil, false
	}
	var prompts []tool.QuestionPrompt
	if err := json.Unmarshal([]byte(payload), &prompts); err != nil || len(prompts) == 0 {
		return nil, false
	}
	return prompts, true
}

func renderQuestionTranscriptNotice(prompts []tool.QuestionPrompt) string {
	if len(prompts) == 1 {
		return "❔ " + prompts[0].Header + " — waiting for your answer"
	}
	return fmt.Sprintf("❔ %d questions — waiting for your answers", len(prompts))
}

func (m *model) startQuestionPrompt(toolCallID string, prompts []tool.QuestionPrompt) {
	m.input.Blur()
	m.showQuestionDialog = true
	m.activeTab = tabChat
	m.chatUnread = false
	m.questionToolCallID = toolCallID
	m.questionPrompts = prompts
	m.questionTab = 0
	m.questionCursor = make([]int, len(prompts))
	m.questionSelected = make([]map[int]bool, len(prompts))
	m.questionCustom = make([]string, len(prompts))
	m.questionTextActive = false
	for i := range prompts {
		m.questionSelected[i] = map[int]bool{}
	}
	m.questionInput.Reset()
}

func (m *model) renderQuestionDialog(width int) string {
	contentWidth := max(1, width-2)
	if len(m.questionPrompts) == 0 {
		return ""
	}
	if m.questionTab < 0 || m.questionTab >= len(m.questionPrompts) {
		m.questionTab = 0
	}
	q := m.questionPrompts[m.questionTab]
	m.clampQuestionCursor()

	var tabs []string
	for i, prompt := range m.questionPrompts {
		label := prompt.Header
		if label == "" {
			label = fmt.Sprintf("Question %d", i+1)
		}
		if m.questionAnswered(i) {
			label += " ✓"
		}
		style := hintStyle.Padding(0, 1)
		if i == m.questionTab {
			style = selectedStyle.Padding(0, 1)
		}
		tabs = append(tabs, style.Render(label))
	}

	var body strings.Builder
	body.WriteString(m.styles.Header.Render("Question"))
	body.WriteString("  ")
	body.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, tabs...))
	body.WriteString("\n\n")
	body.WriteString(wrapView(q.Question, contentWidth))
	body.WriteString("\n\n")

	if m.questionTextActive {
		body.WriteString(m.styles.Hint.Render("Custom answer"))
		body.WriteString("\n")
		m.questionInput.MaxWidth = max(1, contentWidth-2)
		body.WriteString(m.questionInput.View())
		body.WriteString("\n")
		body.WriteString(m.styles.Hint.Render("Enter submit · Shift+Enter newline · Esc back to options"))
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(body.String())
	}

	for i := 0; i < questionOptionCount(q); i++ {
		label, desc, custom := questionOption(q, i)
		cursor := "  "
		if i == m.questionCursor[m.questionTab] {
			cursor = "› "
		}
		marker := "○"
		if q.Multiple {
			marker = "[ ]"
			if m.questionSelected[m.questionTab][i] {
				marker = "[x]"
			}
		} else if m.questionSelected[m.questionTab][i] {
			marker = "◉"
		}
		line := fmt.Sprintf("%s%s %s", cursor, marker, label)
		if custom && m.questionCustom[m.questionTab] != "" {
			line += ": " + m.questionCustom[m.questionTab]
		}
		body.WriteString(line)
		if desc != "" {
			body.WriteString(" — ")
			body.WriteString(desc)
		}
		body.WriteString("\n")
	}
	body.WriteString("\n")
	hint := "↑/↓ move · Enter choose · ←/→ question · Esc cancel"
	if q.Multiple {
		hint = "↑/↓ move · Space/Enter toggle · →/Tab confirm · Esc cancel"
	}
	body.WriteString(m.styles.Hint.Render(hint))
	return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(body.String())
}

func (m model) handleQuestionKeys(msg tea.KeyPressMsg, tiCmd, vpCmd tea.Cmd) (tea.Model, tea.Cmd) {
	if len(m.questionPrompts) == 0 {
		m.showQuestionDialog = false
		return m, tea.Batch(tiCmd, vpCmd)
	}

	keyStr := msg.String()
	if m.questionTextActive {
		switch keyStr {
		case "esc":
			m.questionTextActive = false
			m.questionInput.Reset()
			m.renderTranscript()
			return m, nil
		case "enter":
			answer := strings.TrimSpace(m.questionInput.Value())
			if answer == "" {
				return m, nil
			}
			m.questionCustom[m.questionTab] = answer
			m.selectQuestionOption(m.questionTab, questionOtherIndex(m.questionPrompts[m.questionTab]))
			m.questionTextActive = false
			m.questionInput.Reset()
			return m.advanceOrSubmitQuestion()
		}
		var cmd tea.Cmd
		m.questionInput, cmd = m.questionInput.Update(msg)
		return m, cmd
	}

	switch keyStr {
	case "esc":
		m.clearQuestionPrompt()
		return m, nil
	case "left", "h":
		m.questionTab = (m.questionTab - 1 + len(m.questionPrompts)) % len(m.questionPrompts)
		m.clampQuestionCursor()
		m.renderTranscript()
		return m, nil
	case "right", "l", "tab":
		m.questionTab = (m.questionTab + 1) % len(m.questionPrompts)
		m.clampQuestionCursor()
		m.renderTranscript()
		return m, nil
	case "up", "k":
		if m.questionCursor[m.questionTab] > 0 {
			m.questionCursor[m.questionTab]--
		}
		return m, nil
	case "down", "j":
		if m.questionCursor[m.questionTab] < questionOptionCount(m.questionPrompts[m.questionTab])-1 {
			m.questionCursor[m.questionTab]++
		}
		return m, nil
	case " ":
		if m.questionCursorIsOther() {
			m.activateQuestionTextInput()
			return m, nil
		}
		m.toggleQuestionSelection(m.questionTab, m.questionCursor[m.questionTab])
		return m, nil
	case "enter":
		if m.questionCursorIsOther() {
			m.activateQuestionTextInput()
			return m, nil
		}
		q := m.questionPrompts[m.questionTab]
		if q.Multiple {
			m.toggleQuestionSelection(m.questionTab, m.questionCursor[m.questionTab])
			return m, nil
		}
		m.selectQuestionOption(m.questionTab, m.questionCursor[m.questionTab])
		return m.advanceOrSubmitQuestion()
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m *model) activateQuestionTextInput() {
	m.questionTextActive = true
	m.questionInput.SetValue(m.questionCustom[m.questionTab])
	m.questionInput.Focus()
}

func (m *model) clampQuestionCursor() {
	if len(m.questionPrompts) == 0 || m.questionTab < 0 || m.questionTab >= len(m.questionPrompts) {
		return
	}
	count := questionOptionCount(m.questionPrompts[m.questionTab])
	if count <= 0 {
		m.questionCursor[m.questionTab] = 0
		return
	}
	if m.questionCursor[m.questionTab] < 0 {
		m.questionCursor[m.questionTab] = 0
	}
	if m.questionCursor[m.questionTab] >= count {
		m.questionCursor[m.questionTab] = count - 1
	}
}

func (m model) questionCursorIsOther() bool {
	if len(m.questionPrompts) == 0 || m.questionTab < 0 || m.questionTab >= len(m.questionPrompts) {
		return false
	}
	return m.questionCursor[m.questionTab] == questionOtherIndex(m.questionPrompts[m.questionTab])
}

func (m *model) selectQuestionOption(qIdx, optionIdx int) {
	if qIdx < 0 || qIdx >= len(m.questionPrompts) {
		return
	}
	if m.questionSelected[qIdx] == nil {
		m.questionSelected[qIdx] = map[int]bool{}
	}
	if m.questionPrompts[qIdx].Multiple {
		m.questionSelected[qIdx][optionIdx] = true
		return
	}
	m.questionSelected[qIdx] = map[int]bool{optionIdx: true}
}

func (m *model) toggleQuestionSelection(qIdx, optionIdx int) {
	if qIdx < 0 || qIdx >= len(m.questionPrompts) {
		return
	}
	if m.questionSelected[qIdx] == nil {
		m.questionSelected[qIdx] = map[int]bool{}
	}
	if !m.questionPrompts[qIdx].Multiple {
		m.selectQuestionOption(qIdx, optionIdx)
		return
	}
	if m.questionSelected[qIdx][optionIdx] {
		delete(m.questionSelected[qIdx], optionIdx)
	} else {
		m.questionSelected[qIdx][optionIdx] = true
	}
}

func (m model) advanceOrSubmitQuestion() (tea.Model, tea.Cmd) {
	if next := m.nextUnansweredQuestion(); next >= 0 {
		m.questionTab = next
		m.renderTranscript()
		return m, nil
	}
	return m.submitQuestionAnswers()
}

func (m model) nextUnansweredQuestion() int {
	for offset := 1; offset <= len(m.questionPrompts); offset++ {
		idx := (m.questionTab + offset) % len(m.questionPrompts)
		if !m.questionAnswered(idx) {
			return idx
		}
	}
	return -1
}

func (m model) questionAnswered(idx int) bool {
	if idx < 0 || idx >= len(m.questionPrompts) || idx >= len(m.questionSelected) {
		return false
	}
	otherIdx := questionOtherIndex(m.questionPrompts[idx])
	for selected := range m.questionSelected[idx] {
		if selected == otherIdx {
			if strings.TrimSpace(m.questionCustom[idx]) != "" {
				return true
			}
			continue
		}
		return true
	}
	return false
}

type questionAnswerPayload struct {
	Header   string                `json:"header,omitempty"`
	Question string                `json:"question"`
	Answers  []questionAnswerValue `json:"answers"`
}

type questionAnswerValue struct {
	Label  string `json:"label"`
	Text   string `json:"text,omitempty"`
	Custom bool   `json:"custom,omitempty"`
}

func (m model) submitQuestionAnswers() (tea.Model, tea.Cmd) {
	payload, _ := json.Marshal(m.buildQuestionAnswerPayload())
	toolMsg := agent.Message{Role: "tool", ToolID: m.questionToolCallID, Content: string(payload)}
	m.messages = append(m.messages, message{
		role: roleAssistant,
		text: "✅ answered question prompt",
		raw:  &toolMsg,
	})
	m.clearQuestionPrompt()
	m.input.Reset()
	m.renderTranscript()
	m.viewport.GotoBottom()
	m.saveSession()
	if m.agent == nil {
		return m, nil
	}
	return m, m.askAgent()
}

func (m model) buildQuestionAnswerPayload() []questionAnswerPayload {
	out := make([]questionAnswerPayload, 0, len(m.questionPrompts))
	for i, q := range m.questionPrompts {
		answers := []questionAnswerValue{}
		for optionIdx := 0; optionIdx < questionOptionCount(q); optionIdx++ {
			if !m.questionSelected[i][optionIdx] {
				continue
			}
			label, _, custom := questionOption(q, optionIdx)
			value := questionAnswerValue{Label: label}
			if custom {
				value.Text = strings.TrimSpace(m.questionCustom[i])
				value.Custom = true
			}
			answers = append(answers, value)
		}
		out = append(out, questionAnswerPayload{Header: q.Header, Question: q.Question, Answers: answers})
	}
	return out
}

func (m *model) clearQuestionPrompt() {
	m.showQuestionDialog = false
	m.questionToolCallID = ""
	m.questionPrompts = nil
	m.questionTab = 0
	m.questionCursor = nil
	m.questionSelected = nil
	m.questionCustom = nil
	m.questionTextActive = false
	m.questionInput.Reset()
	m.input.Focus()
}

func questionOptionCount(q tool.QuestionPrompt) int {
	if questionExistingOtherIndex(q) >= 0 {
		return len(q.Options)
	}
	return len(q.Options) + 1
}

func questionOtherIndex(q tool.QuestionPrompt) int {
	if idx := questionExistingOtherIndex(q); idx >= 0 {
		return idx
	}
	return len(q.Options)
}

func questionOption(q tool.QuestionPrompt, idx int) (label, desc string, custom bool) {
	if idx >= 0 && idx < len(q.Options) {
		opt := q.Options[idx]
		return opt.Label, opt.Description, isQuestionOtherLabel(opt.Label)
	}
	return questionOtherLabel, "Type your own answer", true
}

func questionExistingOtherIndex(q tool.QuestionPrompt) int {
	for i, opt := range q.Options {
		if isQuestionOtherLabel(opt.Label) {
			return i
		}
	}
	return -1
}

func isQuestionOtherLabel(label string) bool {
	lower := strings.ToLower(strings.TrimSpace(label))
	return strings.Contains(lower, "something else") || strings.Contains(lower, "other") || strings.Contains(lower, "own answer") || strings.Contains(lower, "custom")
}
