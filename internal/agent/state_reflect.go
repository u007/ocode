package agent

import (
	"fmt"
	"strings"
)

// ReflectState holds the last-reflected canonical snapshot for preview and
// browser surfaces. It is owned by the Agent and updated each loop so the
// comparison is against the agent's own baseline, not global or process state.
// Only surfaces whose canonical value changed trigger a new tail message.
// The stable-prefix invariant: messages are always appended at the very end,
// never inserted before existing transcript rows, so any external prompt
// caching of the message-array prefix remains intact.
type ReflectState struct {
	preview string
	browser string
}

// CanonicalSnapshot builds a deterministic, noise-free string for one
// surface. The caller passes the authoritative state value (e.g. a file
// path for preview, a stateKey+url pair for browser); empty means closed.
func CanonicalSnapshot(name, value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", name, value)
}

// ReflectMessage builds a user-role message describing the current surface
// states. It compares prev (agent-owned) against current canonical strings.
// It returns (message, emitted): emitted is true only when at least one
// surface changed, so unchanged loops emit nothing and the message-array
// prefix is byte-identical across loops.
func ReflectMessage(prev ReflectState, cur ReflectState) (Message, bool) {
	changed := prev.preview != cur.preview || prev.browser != cur.browser
	if !changed {
		return Message{}, false
	}

	var parts []string
	if cur.preview != prev.preview {
		if cur.preview == "" {
			parts = append(parts, "[preview closed]")
		} else {
			parts = append(parts, fmt.Sprintf("[preview open] %s", cur.preview))
		}
	}
	if cur.browser != prev.browser {
		if cur.browser == "" {
			parts = append(parts, "[browser closed]")
		} else {
			parts = append(parts, fmt.Sprintf("[browser surface] %s", cur.browser))
		}
	}

	msg := Message{
		Role:    "user",
		Content: "State update: " + strings.Join(parts, ", "),
	}
	return msg, true
}
