package agent

import (
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// Hooks for the Pulse dashboard assistant (docs/concepts/pulse-assistant.md).
// The assistant is an ordinary server chat session; the server uses these to
// swap its prompt, its tool set and its per-turn context. Only
// SetSystemPromptOverride may be called on a live agent; the other setters are
// build-time only.

// pulseOpenMarker / pulseCloseMarker bracket the board snapshot block.
const (
	pulseOpenMarker  = "[ocode:pulse]"
	pulseCloseMarker = "[/ocode:pulse]"
)

// SetSystemPromptOverride makes prompt the agent's entire base system prompt,
// replacing the environment block, mode fragment, recap contract and project
// context. Empty restores the normal assembly. The value must be a function of
// stable per-session state: it rides the cached system block. Safe to call on
// a live agent: the next BasePromptMessages call picks it up.
func (a *Agent) SetSystemPromptOverride(prompt string) {
	prompt = strings.TrimSpace(prompt)
	a.systemPromptOverride.Store(&prompt)
}

// SetPulseSnapshot installs the callback that renders the Pulse board. It is
// invoked once per Step and its result is appended as a user-role tail block.
// nil (the default) injects nothing.
func (a *Agent) SetPulseSnapshot(fn func() string) {
	a.pulseSnapshot = fn
}

// RestrictToTools replaces the agent's whole tool set (including the builtins
// NewAgent registers) with exactly tools, and turns discovery off so nothing
// can attach further tools. Used by the Pulse assistant, which must not have
// bash, file or task tools.
func (a *Agent) RestrictToTools(tools []tool.Tool) {
	a.tools = make(map[string]tool.Tool, len(tools))
	a.mcpTools = make(map[string]struct{})
	for _, t := range tools {
		a.tools[t.Name()] = t
	}
	a.skipDiscovery = true
}

// injectPulseTail appends the board as one user-role message. User-role on
// purpose: every system-role message is hoisted into the cached system block,
// and the board changes every turn, so a system-role copy would bust the whole
// prompt cache. An empty snapshot injects nothing. base is not mutated.
func injectPulseTail(base []Message, a *Agent) []Message {
	if a == nil || a.pulseSnapshot == nil {
		return base
	}
	board := strings.TrimSpace(a.pulseSnapshot())
	if board == "" {
		return base
	}
	out := make([]Message, 0, len(base)+1)
	out = append(out, base...)
	out = append(out, Message{Role: "user", Content: pulseOpenMarker + "\n" + board + "\n" + pulseCloseMarker})
	return out
}
