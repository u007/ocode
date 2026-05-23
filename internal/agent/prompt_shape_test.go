package agent

import (
	"strings"
	"testing"
)

// TestBasePromptShape_PerPrimaryAgent asserts the marker order and presence
// for each built-in primary agent. It guards against drift in the prompt
// assembler — adding a new fragment requires updating this test deliberately.
//
// Scope: only marker order + per-mode prompt presence. Does NOT snapshot full
// prompt text (which would churn on every wording tweak). Does NOT cover every
// entrypoint (TUI/CLI/server/ACP/subagent) because they all funnel through
// BasePromptMessages — that's the single chokepoint.
func TestBasePromptShape_PerPrimaryAgent(t *testing.T) {
	primaryModes := []Mode{ModeBuild, ModePlan, ModeReview, ModeDebug, ModeDocs}

	for _, mode := range primaryModes {
		t.Run(string(mode), func(t *testing.T) {
			a := &Agent{
				client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
				mode:   mode,
			}
			msgs := a.BasePromptMessages("")

			wantOrder := []string{
				promptEnvMarker,
				promptProviderMarker,
				promptModeMarker,
			}
			gotOrder := collectMarkers(msgs)
			if !startsWith(gotOrder, wantOrder) {
				t.Fatalf("marker order mismatch:\n  got:  %v\n  want prefix: %v", gotOrder, wantOrder)
			}

			modeMsg := findMarker(msgs, promptModeMarker)
			if modeMsg == "" {
				t.Fatal("mode fragment missing")
			}
			expected := mode.SystemPrompt()
			if expected != "" && !strings.Contains(modeMsg, strings.SplitN(expected, "\n", 2)[0]) {
				t.Errorf("mode fragment does not contain expected mode prompt opening: got %q", modeMsg[:min(120, len(modeMsg))])
			}
		})
	}
}

func TestBasePromptShape_SelectionAppendedLast(t *testing.T) {
	a := &Agent{
		client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
		mode:   ModeBuild,
	}
	msgs := a.BasePromptMessages("user-selected text here")
	markers := collectMarkers(msgs)
	if len(markers) == 0 {
		t.Fatal("no markers")
	}
	if markers[len(markers)-1] != promptSelectionMarker {
		t.Errorf("selection marker should be last; got order %v", markers)
	}
}

func TestPrepareMessages_DoesNotDuplicateFragments(t *testing.T) {
	a := &Agent{
		client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
		mode:   ModeBuild,
	}
	first := a.PrepareMessages(nil, "")
	twice := a.PrepareMessages(first, "")
	if len(first) != len(twice) {
		t.Errorf("PrepareMessages duplicated fragments: first=%d twice=%d", len(first), len(twice))
	}
}

func collectMarkers(msgs []Message) []string {
	var out []string
	for _, m := range msgs {
		if mk := promptMarker(m.Content); mk != "" {
			out = append(out, mk)
		}
	}
	return out
}

func findMarker(msgs []Message, marker string) string {
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, marker) {
			return m.Content
		}
	}
	return ""
}

func startsWith(got, want []string) bool {
	if len(got) < len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
