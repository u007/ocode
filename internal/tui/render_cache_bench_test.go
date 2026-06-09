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
// transcript plus one in-flight thinking block of thinkBytes characters,
// mirroring the streaming state where renderTranscript runs ~20×/sec. The
// thinking block is the only message that changes between deltas — and because
// its content changes, it is a render-cache MISS on every delta.
func buildHeavyTranscriptModel(nPairs, thinkBytes int) model {
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
	// In-flight thinking block sized to thinkBytes — a realistic multi-KB
	// reasoning turn, not a few chars.
	think := strings.Repeat("reasoning about the problem step by step, ", (thinkBytes/42)+1)
	m.messages = append(m.messages, message{role: roleThinking, text: think[:thinkBytes]})
	m.streamingThinkingIdx = len(m.messages) - 1
	m.showThinking = true
	return m
}

// benchStreaming primes the cache then measures the per-delta renderTranscript
// cost with the thinking block mutated each iteration (a cache miss every time,
// exactly like a live stream).
func benchStreaming(b *testing.B, nPairs, thinkBytes int) {
	b.Helper()
	m := buildHeavyTranscriptModel(nPairs, thinkBytes)
	m.renderTranscript() // prime the cache for the unchanged messages
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.messages[m.streamingThinkingIdx].text += "x" // force a thinking cache miss
		m.renderTranscript()
	}
}

// Sweep A — fixed 8KB thinking block, growing transcript. Climbing time here
// means the whole-transcript residual (wrapView / stripANSI / maxLineWidth in
// SetContent / O(N²) lookupToolName) re-running over the unchanged prefix.
func BenchmarkStreamSweepA_Pairs10(b *testing.B)   { benchStreaming(b, 10, 8*1024) }
func BenchmarkStreamSweepA_Pairs100(b *testing.B)  { benchStreaming(b, 100, 8*1024) }
func BenchmarkStreamSweepA_Pairs1000(b *testing.B) { benchStreaming(b, 1000, 8*1024) }

// Sweep B — small transcript, growing thinking block. Climbing time here means
// the streaming block re-renders itself each delta (and is wrapped twice: once
// in renderMessageBlock, again in the whole-transcript wrapView).
func BenchmarkStreamSweepB_Block1KB(b *testing.B)  { benchStreaming(b, 5, 1*1024) }
func BenchmarkStreamSweepB_Block4KB(b *testing.B)  { benchStreaming(b, 5, 4*1024) }
func BenchmarkStreamSweepB_Block16KB(b *testing.B) { benchStreaming(b, 5, 16*1024) }
