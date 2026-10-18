package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// displayPolicy resolves a shared chat_verbosity config the way newModel does.
func displayPolicy(t *testing.T, cfg config.ChatVerbosityConfig) config.ChatDisplayPolicy {
	t.Helper()
	policy, err := config.ResolveChatVerbosityPolicy(cfg)
	if err != nil {
		t.Fatalf("ResolveChatVerbosityPolicy() error = %v", err)
	}
	return policy
}

// thinkingDisplayModel is a renderable model with the given display policy.
// Its viewport is tall enough that the transcript never scrolls or wraps.
func thinkingDisplayModel(t *testing.T, cfg config.ChatVerbosityConfig) model {
	t.Helper()
	return model{
		width:                100,
		height:               60,
		activeTab:            tabChat,
		input:                textarea.New(),
		viewport:             fastviewport.New(80, 40),
		styles:               ApplyThemeColors("tokyonight"),
		showThinking:         true,
		streamingThinkingIdx: -1,
		chatVerbosity:        config.NormalizeChatVerbosityConfig(cfg),
		chatDisplay:          displayPolicy(t, cfg),
	}
}

func displayUserMsg(text string) message {
	return message{role: roleUser, text: text, raw: &agent.Message{Role: "user", Content: text}}
}

// displayThinkingMsg builds a thinking block of n numbered lines, e.g.
// prefix-01 .. prefix-12. Zero-padded so "prefix-01" never matches "prefix-010".
func displayThinkingMsg(prefix string, n int) message {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s-%02d", prefix, i+1)
	}
	return message{role: roleThinking, text: strings.Join(lines, "\n")}
}

// displayAnswerMsg is final assistant answer text, as appendAgentMessage stores it.
func displayAnswerMsg(text string) message {
	return message{role: roleAssistant, text: text, raw: &agent.Message{Role: "assistant", Content: text}}
}

// displayToolCallMsg is a tool-call-only assistant message: the rendered text
// carries the call hint, but the raw content is empty.
func displayToolCallMsg() message {
	return message{
		role: roleAssistant,
		text: "read_file path=main.go",
		raw:  &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1", Type: "function"}}},
	}
}

// renderedThinking renders the transcript and returns its visible (ANSI-free)
// text, which is what the tests assert against.
func renderedThinking(m *model) string {
	m.renderTranscript()
	return strings.Join(m.rawTranscriptLines, "\n")
}

func TestThinkingAutoExpandsUntilAnswerStarts(t *testing.T) {
	m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: "quiet"})
	m.messages = []message{displayUserMsg("q"), displayThinkingMsg("step", 12)}

	shown := renderedThinking(&m)
	if !strings.Contains(shown, "step-01") || strings.Contains(shown, "click to expand") {
		t.Fatalf("auto with no answer should show the full block, got:\n%s", shown)
	}

	m.messages = append(m.messages, displayAnswerMsg("done"))
	shown = renderedThinking(&m)
	if strings.Contains(shown, "step-01") || !strings.Contains(shown, "step-12") || !strings.Contains(shown, "click to expand") {
		t.Fatalf("auto after answer text should collapse to the 8-line tail, got:\n%s", shown)
	}
}

func TestThinkingAutoIgnoresToolCallOnlyMessages(t *testing.T) {
	m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: "quiet"})
	m.messages = []message{displayUserMsg("q"), displayThinkingMsg("step", 12), displayToolCallMsg()}

	if shown := renderedThinking(&m); !strings.Contains(shown, "step-01") {
		t.Fatalf("a tool-call-only message must not collapse the block, got:\n%s", shown)
	}

	m.messages = append(m.messages, displayAnswerMsg("done"))
	if shown := renderedThinking(&m); strings.Contains(shown, "step-01") {
		t.Fatalf("answer text after the tool call should collapse the block, got:\n%s", shown)
	}
}

// A local notice (no raw agent message) between the thinking block and the
// answer is not answer text, so it must not collapse an auto block.
func TestThinkingAutoIgnoresLocalNotices(t *testing.T) {
	m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: "quiet"})
	m.messages = []message{
		displayUserMsg("q"),
		displayThinkingMsg("step", 12),
		{role: roleAssistant, text: "Reasoning effort set to high (16000 tokens)."},
	}
	if shown := renderedThinking(&m); !strings.Contains(shown, "step-01") {
		t.Fatalf("a local notice must not count as answer text, got:\n%s", shown)
	}
}

func TestThinkingFixedLatestOverrideIgnoresAnswerState(t *testing.T) {
	override := config.ChatVerbosityConfig{Preset: "quiet", Overrides: config.ChatVerbosityOverrides{LatestThinking: "collapsed"}}
	m := thinkingDisplayModel(t, override)
	m.messages = []message{displayUserMsg("q"), displayThinkingMsg("step", 12)}
	if shown := renderedThinking(&m); strings.Contains(shown, "step-01") {
		t.Fatalf("latest_thinking=collapsed should collapse with no answer, got:\n%s", shown)
	}

	expanded := config.ChatVerbosityConfig{Preset: "quiet", Overrides: config.ChatVerbosityOverrides{LatestThinking: "expanded"}}
	m = thinkingDisplayModel(t, expanded)
	m.messages = []message{displayUserMsg("q"), displayThinkingMsg("step", 12), displayAnswerMsg("done")}
	if shown := renderedThinking(&m); !strings.Contains(shown, "step-01") {
		t.Fatalf("latest_thinking=expanded should stay expanded after answer text, got:\n%s", shown)
	}
}

// Older thinking follows older_thinking; the latest block follows
// latest_thinking. Each preset is checked on the same two-turn transcript.
func TestThinkingPresetDefaultsForOlderAndLatest(t *testing.T) {
	transcript := func(answered bool) []message {
		msgs := []message{
			displayUserMsg("first"),
			displayThinkingMsg("old", 12),
			displayAnswerMsg("first answer"),
			displayUserMsg("second"),
			displayThinkingMsg("new", 12),
		}
		if answered {
			msgs = append(msgs, displayAnswerMsg("second answer"))
		}
		return msgs
	}
	tests := []struct {
		preset   string
		answered bool
		wantOld  bool
		wantNew  bool
	}{
		{preset: "full", answered: false, wantOld: true, wantNew: true},
		{preset: "full", answered: true, wantOld: true, wantNew: true},
		{preset: "balanced", answered: false, wantOld: false, wantNew: true},
		{preset: "balanced", answered: true, wantOld: false, wantNew: true},
		{preset: "quiet", answered: false, wantOld: false, wantNew: true},
		{preset: "quiet", answered: true, wantOld: false, wantNew: false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/answered=%v", tt.preset, tt.answered), func(t *testing.T) {
			m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: tt.preset})
			m.messages = transcript(tt.answered)
			shown := renderedThinking(&m)
			if got := strings.Contains(shown, "old-01"); got != tt.wantOld {
				t.Errorf("older block fully visible = %v, want %v:\n%s", got, tt.wantOld, shown)
			}
			if got := strings.Contains(shown, "new-01"); got != tt.wantNew {
				t.Errorf("latest block fully visible = %v, want %v:\n%s", got, tt.wantNew, shown)
			}
		})
	}
}

// clickThinkingHeader releases a left click on the first wrapped line of the
// thinking block at messageIndex, using the same coordinates a user would.
func clickThinkingHeader(t *testing.T, m model, messageIndex int) model {
	t.Helper()
	m.renderTranscript()
	for _, region := range m.thinkingRegions {
		if region.messageIndex != messageIndex {
			continue
		}
		y := appHeaderHeight + 1 + (region.startLine - m.viewport.YOffset())
		// A click is handled on release: the press only starts a selection.
		updated, _, handled := m.handleMouseAction(tea.Mouse{X: 10, Y: y, Button: tea.MouseLeft}, false)
		if !handled {
			t.Fatalf("click on thinking block %d was not handled", messageIndex)
		}
		got, ok := updated.(model)
		if !ok {
			t.Fatalf("handleMouseAction returned %T, want model", updated)
		}
		return got
	}
	t.Fatalf("no thinking region for message %d in %+v", messageIndex, m.thinkingRegions)
	return m
}

// A click flips what is on screen, even when the block is expanded by the
// policy and has no stored choice. The stored choice then survives the answer
// arriving and a preset change.
func TestThinkingManualClickWinsOverPolicy(t *testing.T) {
	m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: "quiet"})
	m.messages = []message{
		displayUserMsg("first"),
		displayThinkingMsg("old", 12),
		displayAnswerMsg("first answer"),
		displayUserMsg("second"),
		displayThinkingMsg("new", 12),
	}

	// The latest block is auto-expanded (no answer yet); one click collapses it.
	m = clickThinkingHeader(t, m, 4)
	if shown := renderedThinking(&m); strings.Contains(shown, "new-01") {
		t.Fatalf("click on an auto-expanded block should collapse it, got:\n%s", shown)
	}

	// The older block is collapsed by policy; one click expands it.
	m = clickThinkingHeader(t, m, 1)
	if shown := renderedThinking(&m); !strings.Contains(shown, "old-01") {
		t.Fatalf("click on a collapsed older block should expand it, got:\n%s", shown)
	}

	// The answer arrives: auto would collapse the latest block (already
	// collapsed by the click), and the older block stays expanded by choice.
	m.messages = append(m.messages, displayAnswerMsg("second answer"))
	if shown := renderedThinking(&m); !strings.Contains(shown, "old-01") {
		t.Fatalf("manual expand of the older block must survive the answer, got:\n%s", shown)
	}

	// A preset change re-applies defaults only where there is no manual choice.
	m.chatVerbosity = config.ChatVerbosityConfig{Preset: "balanced"}
	m.chatDisplay = displayPolicy(t, m.chatVerbosity)
	if shown := renderedThinking(&m); !strings.Contains(shown, "old-01") {
		t.Fatalf("preset change must not clobber the manual older expand, got:\n%s", shown)
	}
}

// A newly streamed block must not inherit a manual choice left at the same
// message index by an earlier session (/new reuses indices).
func TestThinkingManualChoiceDoesNotLeakToNewBlock(t *testing.T) {
	m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: "quiet"})
	m.expandedThinking = map[int]bool{1: false}
	m.streaming = true
	m.messages = []message{displayUserMsg("q")}
	m.applyThinkingDelta("reasoning", strings.Repeat("line\n", 12))

	if _, manual := m.expandedThinking[m.streamingThinkingIdx]; manual {
		t.Fatalf("new streamed block inherited a manual choice: %+v", m.expandedThinking)
	}
	if shown := renderedThinking(&m); !strings.Contains(shown, "click to collapse") {
		t.Fatalf("new streamed block should follow the auto policy (expanded), got:\n%s", shown)
	}
}

// A manual choice stored against an index from a previous transcript must not
// override the policy for a block that reuses that index after a replacement.
func TestThinkingChoicesResetWhenTranscriptReplaced(t *testing.T) {
	m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: "full"})
	m.messages = []message{
		displayUserMsg("q"),
		displayThinkingMsg("old", 12),
		displayAnswerMsg("a"),
	}
	m.expandedThinking = map[int]bool{1: false}
	m.resetThinkingChoices()

	if shown := renderedThinking(&m); !strings.Contains(shown, "old-01") {
		t.Fatalf("stale manual collapse survived a transcript replacement, got:\n%s", shown)
	}
}

func TestDisplayCommandNoArgShowsPolicy(t *testing.T) {
	m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: "quiet"})
	m.handleDisplayCmd(nil)

	got := m.messages[len(m.messages)-1].text
	for _, want := range []string{"Display preset: quiet", "Older thinking: collapsed", "Latest thinking: auto", "/display full|balanced|quiet"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/display output missing %q, got:\n%s", want, got)
		}
	}
}

func TestDisplayCommandRejectsUnknownPreset(t *testing.T) {
	m := thinkingDisplayModel(t, config.ChatVerbosityConfig{Preset: "full"})
	before := m.chatDisplay
	m.handleDisplayCmd([]string{"loud"})

	if got := m.messages[len(m.messages)-1].text; !strings.Contains(got, `Unknown display preset "loud"`) {
		t.Fatalf("expected unknown-preset message, got %q", got)
	}
	if m.chatDisplay != before || m.chatVerbosity.Preset != "full" {
		t.Fatalf("rejected preset changed state: policy=%+v preset=%q", m.chatDisplay, m.chatVerbosity.Preset)
	}
}

// Setting a preset persists it, keeps existing per-category overrides, and
// updates the live policy at once. HOME and XDG_CONFIG_HOME point at a private
// temp dir so the shared test config is not touched.
func TestDisplayCommandSetsAndPersistsPreset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	seed := config.ChatVerbosityConfig{Preset: "full", Overrides: config.ChatVerbosityOverrides{ToolCalls: "collapsed"}}
	if err := config.SaveOcodeChatVerbosity(seed); err != nil {
		t.Fatalf("seed SaveOcodeChatVerbosity() error = %v", err)
	}

	m := thinkingDisplayModel(t, seed)
	m.handleDisplayCmd([]string{"BALANCED"})

	if got := m.messages[len(m.messages)-1].text; got != "Display preset set to balanced." {
		t.Fatalf("confirmation = %q", got)
	}
	if m.chatVerbosity.Preset != "balanced" || m.chatDisplay.OlderThinking != config.ChatDisplayCollapsed || m.chatDisplay.LatestThinking != config.ChatDisplayExpanded {
		t.Fatalf("live policy not updated: preset=%q policy=%+v", m.chatVerbosity.Preset, m.chatDisplay)
	}
	if m.chatDisplay.ToolCalls != config.ChatDisplayCollapsed {
		t.Fatalf("tool_calls override should be kept, got %q", m.chatDisplay.ToolCalls)
	}

	saved, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	if saved.ChatVerbosity.Preset != "balanced" || saved.ChatVerbosity.Overrides.ToolCalls != "collapsed" {
		t.Fatalf("persisted chat_verbosity = %+v, want balanced with tool_calls override", saved.ChatVerbosity)
	}
}

func TestDisplayCommandIsInstantWhileStreaming(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	for _, command := range []string{"/display", "/display quiet"} {
		m := model{
			width:     80,
			height:    20,
			input:     textarea.New(),
			viewport:  fastviewport.New(80, 20),
			streaming: true,
			config:    &config.Config{},
		}
		updated, _ := m.handleCommand(command)
		got := updated.(*model)
		if len(got.queuedItems) != 0 {
			t.Fatalf("expected %s to run immediately while streaming, got queued %#v", command, got.queuedItems)
		}
	}
}

func TestDisplayCommandIsInHelp(t *testing.T) {
	if help := commandHelpText(); !strings.Contains(help, "/display") {
		t.Fatalf("help text does not list /display:\n%s", help)
	}
}
