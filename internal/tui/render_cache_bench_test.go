package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/u007/ocode/internal/agent"
)

// buildHeavyTranscriptModel constructs a model with a realistic, tool-heavy
// transcript plus one in-flight thinking block, mirroring the streaming state
// where renderTranscript runs ~20×/sec.
func buildHeavyTranscriptModel(nPairs int) model {
	m := model{
		input:    textarea.New(),
		viewport: viewport.New(viewport.WithWidth(100), viewport.WithHeight(30)),
		styles:   ApplyThemeColors("tokyonight"),
		ready:    true,
	}
	toolBody := strings.Repeat("some tool output line with a fair bit of text\n", 30)
	for i := 0; i < nPairs; i++ {
		m.messages = append(m.messages, message{
			role: roleAssistant,
			text: fmt.Sprintf("Assistant reply %d with **bold** and a # heading\n\nfollowed by a paragraph of explanation that spans a couple of lines.", i),
		})
		toolID := fmt.Sprintf("tool-%d", i)
		// The assistant message that declares the tool call (source of the name).
		tc := agent.ToolCall{ID: toolID}
		tc.Function.Name = "read"
		m.messages = append(m.messages, message{
			role: roleAssistant,
			raw: &agent.Message{
				Role:      "assistant",
				ToolCalls: []agent.ToolCall{tc},
			},
		})
		m.messages = append(m.messages, message{
			role: roleAssistant,
			raw:  &agent.Message{Role: "tool", ToolID: toolID, Content: toolBody},
		})
	}
	// In-flight thinking block (the only message that changes between deltas).
	m.messages = append(m.messages, message{role: roleThinking, text: "thinking…"})
	m.streamingThinkingIdx = len(m.messages) - 1
	m.showThinking = true
	return m
}

// BenchmarkRenderTranscriptWarm simulates the streaming hot loop: only the
// thinking block grows; every other message must hit the render cache.
func BenchmarkRenderTranscriptWarm(b *testing.B) {
	m := buildHeavyTranscriptModel(100)
	m.renderTranscript() // prime the cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.messages[m.streamingThinkingIdx].text += "x"
		m.renderTranscript()
	}
}

// BenchmarkRenderTranscriptCold renders with a cleared cache every iteration —
// the pre-fix behaviour, where every message is re-rendered from scratch.
func BenchmarkRenderTranscriptCold(b *testing.B) {
	m := buildHeavyTranscriptModel(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.msgRenderCache = nil
		m.renderTranscript()
	}
}
