package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const delayedChatInputDelay = 1500 * time.Millisecond

func joinDelayedChatInputs(inputs []string) string {
	return strings.Join(inputs, "\n\n")
}

func (m *model) queueDelayedChatInput(text string) tea.Cmd {
	m.delayedChatInputs = append(m.delayedChatInputs, text)
	m.delayedChatGeneration++
	generation := m.delayedChatGeneration
	return tea.Tick(delayedChatInputDelay, func(time.Time) tea.Msg {
		return delayedChatInputMsg{generation: generation}
	})
}

func (m *model) hasDelayedChatInput() bool {
	return len(m.delayedChatInputs) > 0
}

func (m *model) takeDelayedChatInput() string {
	if !m.hasDelayedChatInput() {
		return ""
	}
	text := joinDelayedChatInputs(m.delayedChatInputs)
	m.delayedChatInputs = nil
	m.delayedChatGeneration++
	return text
}

func (m *model) invalidateDelayedChatInput() {
	m.delayedChatInputs = nil
	m.delayedChatGeneration++
}
