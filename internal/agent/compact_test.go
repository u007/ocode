package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

func tcCall(id, name string) ToolCall {
	tc := ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = "{}"
	return tc
}

// safeCut should never split an assistant{ToolCalls} from its matching tool
// reply: cuts that would orphan a tool message must move backward.
func TestSafeCutRespectsToolPairs(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "ask"},
		{Role: "assistant", ToolCalls: []ToolCall{tcCall("call1", "read")}},
		{Role: "tool", ToolID: "call1", Content: "result1"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
		{Role: "assistant", Content: "ok"},
	}

	// Cut between assistant{ToolCalls} and tool reply is unsafe — should move down.
	got := safeCut(msgs, 3)
	if got >= 3 {
		t.Fatalf("safeCut(3) returned %d, expected < 3 (must include assistant{call1} when its tool result is in suffix)", got)
	}
	if got > 2 {
		t.Fatalf("safeCut(3) returned %d, expected ≤ 2", got)
	}

	// Cut at clean boundary (between assistant{done} and user{next}) is safe.
	got = safeCut(msgs, 5)
	if got != 5 {
		t.Fatalf("safeCut(5) returned %d, expected 5 (clean turn boundary)", got)
	}
}

func TestSafeCutReverseDirection(t *testing.T) {
	// Test that safeCut also walks back when an assistant with tool_calls in
	// the suffix is missing its matching tool result (the reverse of the
	// existing check which guards against orphaned tool results).

	t.Run("orphaned_result_in_suffix", func(t *testing.T) {
		// Cut between assistant(call2) and its tool result: the tool result
		// in the suffix has no matching assistant — must walk back.
		msgs := []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "ask"},
			{Role: "assistant", ToolCalls: []ToolCall{tcCall("call1", "read")}},
			{Role: "tool", ToolID: "call1", Content: "result1"},
			{Role: "assistant", Content: "done"},
			{Role: "user", Content: "followup"},
			{Role: "assistant", ToolCalls: []ToolCall{tcCall("call2", "write")}},
			{Role: "tool", ToolID: "call2", Content: "result2"},
			{Role: "assistant", Content: "finished"},
		}
		// Cut between assistant(call2) and tool(call2) — tool result orphaned in suffix.
		got := safeCut(msgs, 7)
		if got == 7 {
			t.Errorf("safeCut(7) = %d, must not equal 7 (would orphan tool result call2 in suffix)", got)
		}
		// Clean cut after the full pair is safe.
		got = safeCut(msgs, 8)
		if got != 8 {
			t.Errorf("safeCut(8) = %d, want 8 (clean boundary after tool pair)", got)
		}
	})

	t.Run("orphaned_call_in_suffix", func(t *testing.T) {
		// Cut between tool(call1) and user: the assistant(call1) in the
		// suffix has no matching tool result — must walk back.
		msgs := []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "ask"},
			{Role: "assistant", ToolCalls: []ToolCall{tcCall("call1", "read")}},
			{Role: "tool", ToolID: "call1", Content: "result1"},
			{Role: "user", Content: "next"},
		}
		// Cut at index 4 (the user "next"): suffix = [user(next)]
		// No tool pairs in suffix → safe. Assistant(call1) with its result
		// are both in the prefix, which is fine.
		got := safeCut(msgs, 4)
		if got != 4 {
			t.Errorf("safeCut(4) = %d, want 4 (no tool pairs in suffix)", got)
		}
	})

	t.Run("orphaned_call_in_suffix_requires_walkback", func(t *testing.T) {
		// Assistant(call2) has no matching tool result in the conversation.
		// The cut at the user boundary would put assistant(call2) in the
		// suffix without its result — must walk back to exclude it.
		msgs := []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "ask"},
			{Role: "assistant", ToolCalls: []ToolCall{tcCall("call1", "read")}},
			{Role: "tool", ToolID: "call1", Content: "result1"},
			{Role: "assistant", Content: "done"},
			{Role: "user", Content: "followup"},
			{Role: "assistant", ToolCalls: []ToolCall{tcCall("call2", "write")}},
			// No tool result for call2!
		}
		// findTurnBoundary(1) would give index 5 (user "followup").
		// safeCut(5): suffix=[user(followup), assistant(call2)]
		//   suffixCallIDs = {"call2"}, suffixResultIDs = {}
		//   Reverse check: "call2" not in suffixResultIDs → unsafe!
		got := safeCut(msgs, 5)
		if got == 5 {
			t.Errorf("safeCut(5) = %d, must not equal 5 (would orphan assistant tool_call call2 in suffix)", got)
		}
		if got > 5 {
			t.Errorf("safeCut(5) = %d, must not exceed 5 (cut beyond len would be invalid)", got)
		}
	})
}

func TestSafeCutNoToolCalls(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
	}
	for _, cut := range []int{0, 1, 2, 3, 4} {
		if got := safeCut(msgs, cut); got != cut {
			t.Errorf("safeCut(%d) returned %d, expected %d (no tool pairs to protect)", cut, got, cut)
		}
	}
}

func TestFindTurnBoundary(t *testing.T) {
	msgs := []Message{
		{Role: "system"},
		{Role: "user", Content: "u1"},    // 1
		{Role: "assistant", Content: ""}, // 2
		{Role: "user", Content: "u2"},    // 3
		{Role: "assistant", Content: ""}, // 4
		{Role: "user", Content: "u3"},    // 5
		{Role: "assistant", Content: ""}, // 6
	}
	if got := findTurnBoundary(msgs, 1); got != 5 {
		t.Errorf("findTurnBoundary(1) = %d, want 5", got)
	}
	if got := findTurnBoundary(msgs, 2); got != 3 {
		t.Errorf("findTurnBoundary(2) = %d, want 3", got)
	}
	if got := findTurnBoundary(msgs, 5); got != 0 {
		t.Errorf("findTurnBoundary(5) = %d, want 0 (only 3 user turns exist)", got)
	}
}

func TestFindPrefixEnd(t *testing.T) {
	cases := []struct {
		name string
		msgs []Message
		want int
	}{
		{"only system", []Message{{Role: "system"}}, 1},
		{"system then user", []Message{{Role: "system"}, {Role: "user"}}, 2},
		{"system+system+user", []Message{{Role: "system"}, {Role: "system"}, {Role: "user"}}, 3},
		{"no system, starts with user", []Message{{Role: "user"}}, 1},
		{"no system, starts with assistant", []Message{{Role: "assistant"}}, 0},
		{"empty", nil, 0},
		{"base systems then compaction summary", []Message{
			{Role: "system", Content: "base-prompt-1"},
			{Role: "system", Content: "base-prompt-2"},
			{Role: "system", Content: compactionSummaryMarker + "\nsome summary"},
			{Role: "user", Content: "hello"},
		}, 2},
		{"compaction summary at start", []Message{
			{Role: "system", Content: compactionSummaryMarker + "\nsome summary"},
			{Role: "user", Content: "hello"},
		}, 0},
	}
	for _, c := range cases {
		if got := findPrefixEnd(c.msgs); got != c.want {
			t.Errorf("%s: findPrefixEnd = %d, want %d", c.name, got, c.want)
		}
	}
}

type fakeCompactClient struct{}

func (fakeCompactClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	return &Message{Role: "assistant", Content: "compressed summary"}, nil
}

func (fakeCompactClient) GetProvider() string { return "" }

func (fakeCompactClient) GetModel() string { return "fake-compact-model" }

func TestForceCompactAsyncIgnoresDisabledAutoCompaction(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Compact.Enabled = false
	cfg.Ocode.Compact.KeepRecentTurns = 3
	cfg.Ocode.Compact.MinMessages = 1
	cfg.Ocode.Compact.SummaryTimeoutSeconds = 1
	cfg.Ocode.Compact.SummaryMaxRetries = 0
	cfg.Ocode.Compact.MaxSummaryInputTokens = 1000

	a := NewAgent(fakeCompactClient{}, nil, cfg, nil)
	results := make(chan CompactResult, 1)
	a.OnCompact = func(res CompactResult) {
		results <- res
	}

	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: "a3"},
		{Role: "user", Content: "u4"},
		{Role: "assistant", Content: "a4"},
	}

	if !a.CompactAsync(msgs) {
		t.Fatal("expected manual compaction to start even when auto compaction is disabled")
	}

	select {
	case res := <-results:
		if !res.OK {
			t.Fatalf("expected compaction to succeed, got result: %+v", res)
		}
		if res.ReplaceTo <= res.ReplaceFrom {
			t.Fatalf("expected a positive splice range, got from=%d to=%d", res.ReplaceFrom, res.ReplaceTo)
		}
		if res.Summary.Role != "system" {
			t.Fatalf("expected summary role system, got %q", res.Summary.Role)
		}
		if !strings.Contains(res.Summary.Content, compactionSummaryMarker) {
			t.Fatalf("expected summary marker in content, got %q", res.Summary.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for manual compaction result")
	}
}

func TestBuildSummaryPromptIncludesToolInfo(t *testing.T) {
	middle := []Message{
		{Role: "user", Content: "edit foo.go"},
		{Role: "assistant", ToolCalls: []ToolCall{tcCall("c1", "edit_file")}},
		{Role: "tool", ToolID: "c1", Content: "wrote 12 lines"},
		{Role: "assistant", Content: "done"},
	}
	prompt, dropped := buildSummaryPrompt(middle, 50000, "")
	if dropped != 0 {
		t.Errorf("did not expect drops; got %d", dropped)
	}
	if !strings.Contains(prompt, "tool_call edit_file") {
		t.Errorf("prompt missing tool_call info: %s", prompt)
	}
	if !strings.Contains(prompt, "tool_result") {
		t.Errorf("prompt missing tool_result info: %s", prompt)
	}
	if !strings.Contains(prompt, "edit foo.go") {
		t.Errorf("prompt missing user content")
	}
}

func TestBuildSummaryPromptDropsOldestWhenOverCap(t *testing.T) {
	long := strings.Repeat("x", 8000)
	middle := []Message{
		{Role: "user", Content: long},
		{Role: "user", Content: long},
		{Role: "user", Content: long},
		{Role: "user", Content: "keepme"},
	}
	// maxInputTokens=1000 → maxChars=4000, so most fragments must be dropped.
	prompt, dropped := buildSummaryPrompt(middle, 1000, "")
	if dropped == 0 {
		t.Errorf("expected drops; got 0")
	}
	if !strings.Contains(prompt, "keepme") {
		t.Errorf("most recent message must always be kept")
	}
}

func TestShouldCompactRespectsMinMessages(t *testing.T) {
	rt := compactRuntime{
		Enabled:        true,
		TokenThreshold: 0.5,
		MinMessages:    8,
		WindowTokens:   100,
	}
	short := []Message{{Role: "user", Content: strings.Repeat("x", 1000)}}
	if need, _ := shouldCompact(short, rt); need {
		t.Errorf("should not compact when below MinMessages floor")
	}
}

func TestShouldCompactUsesUsageOverEstimate(t *testing.T) {
	rt := compactRuntime{
		Enabled:        true,
		TokenThreshold: 0.5,
		MinMessages:    2,
		WindowTokens:   1000,
	}
	tokens := int64(800)
	msgs := []Message{
		{Role: "user", Content: "x"},
		{Role: "assistant", Content: "y", Usage: &TokenUsage{PromptTokens: &tokens}},
	}
	need, used := shouldCompact(msgs, rt)
	if !need {
		t.Errorf("expected compaction: used=%d, threshold=%d", used, int(float64(rt.WindowTokens)*rt.TokenThreshold))
	}
	if used != 800 {
		t.Errorf("used tokens=%d, want 800 (from Usage, not estimate)", used)
	}
}

func TestShouldCompactDisabled(t *testing.T) {
	rt := compactRuntime{Enabled: false}
	msgs := []Message{
		{Role: "user", Content: strings.Repeat("x", 100000)},
	}
	if need, _ := shouldCompact(msgs, rt); need {
		t.Errorf("should not compact when disabled")
	}
}

// TestTokenEstimateHandlesExtendedThinking verifies that reasoning content
// is counted at a higher rate (2 chars/token) than regular text (4 chars/token).
// This documents the per-provider tokenization difference.
func TestTokenEstimateHandlesExtendedThinking(t *testing.T) {
	// Message with only reasoning content (e.g., extended thinking before response)
	m := Message{
		Role:             "assistant",
		Content:          "",                         // no regular content
		ReasoningContent: strings.Repeat("x", 10000), // 10k chars of thinking
	}
	est := tokenEstimate(m)

	// Expected: (10000 / 2) + framingOverheadPerMessage = 5000 + 75 = 5075
	// Old behavior would calculate: (10000 / 4) + 75 = 2500 + 75 = 2575
	// This test ensures new logic is 2x more accurate for reasoning content.
	if est < 4500 {
		t.Errorf("reasoning content underestimated: got %d tokens, expected ~5000+", est)
	}
}

// TestTokenEstimateIncludesFramingOverhead verifies that each message's
// framing overhead (role, JSON structure) is accounted for.
func TestTokenEstimateIncludesFramingOverhead(t *testing.T) {
	m := Message{Role: "user", Content: "hello"}
	est := tokenEstimate(m)

	// Expected: (5 chars / 4) + framingOverheadPerMessage = 1 + 75 = 76
	if est < framingOverheadPerMessage {
		t.Errorf("framing overhead missing: got %d tokens, expected >= %d", est, framingOverheadPerMessage)
	}
}

// TestShouldCompactUsesLatestUsage verifies that shouldCompact uses the most
// recent Usage.PromptTokens value (which represents cumulative tokens at that point).
func TestShouldCompactUsesLatestUsage(t *testing.T) {
	rt := compactRuntime{
		Enabled:        true,
		TokenThreshold: 0.6,
		MinMessages:    3,
		WindowTokens:   1000, // threshold: 600 tokens
	}

	t200 := int64(200)
	t700 := int64(700)

	msgs := []Message{
		{Role: "user", Content: "x", Usage: &TokenUsage{PromptTokens: &t200}},
		{Role: "assistant", Content: "y"},
		{Role: "user", Content: "z", Usage: &TokenUsage{PromptTokens: &t700}},
	}

	need, used := shouldCompact(msgs, rt)
	if used != 700 {
		t.Errorf("used tokens=%d, want 700 (most recent Usage.PromptTokens)", used)
	}
	if !need {
		t.Errorf("expected compaction (700 >= threshold %d)", int(float64(rt.WindowTokens)*rt.TokenThreshold))
	}
}

// TestCurrentContextEstimateSkipsZeroUsage verifies that a Usage struct with
// all-zero fields is not treated as real data and falls through to heuristic.
func TestCurrentContextEstimateSkipsZeroUsage(t *testing.T) {
	t0 := int64(0)
	msgs := []Message{
		{Role: "assistant", Usage: &TokenUsage{PromptTokens: &t0}},
		{Role: "user", Content: strings.Repeat("x", 400)},
	}
	_, source := CurrentContextEstimate(msgs)
	if source != "estimated" {
		t.Errorf("expected source=%q for zero Usage, got %q", "estimated", source)
	}
}

// TestShouldCompactFallbackWhenUsageMissing verifies that when no message has
// Usage data, we fall back to character estimation with a safety margin.
func TestShouldCompactFallbackWhenUsageMissing(t *testing.T) {
	rt := compactRuntime{
		Enabled:        true,
		TokenThreshold: 0.5,
		MinMessages:    2,
		WindowTokens:   1000,
	}

	msgs := []Message{
		{Role: "user", Content: strings.Repeat("x", 2000)},      // No Usage
		{Role: "assistant", Content: strings.Repeat("y", 1000)}, // No Usage
	}

	need, used := shouldCompact(msgs, rt)
	// With fallback estimate and safety margin (15%), we should get a reasonable estimate.
	// Without proper handling, this would use inaccurate char/token ratio.
	if used < 500 {
		t.Errorf("fallback estimate too low: %d tokens (likely underestimating)", used)
	}
	// Trigger threshold is 500, so with ~900+ estimated tokens, compaction should trigger
	if !need {
		t.Logf("fallback estimate: %d tokens, threshold: %d", used, int(float64(rt.WindowTokens)*rt.TokenThreshold))
		t.Errorf("expected compaction to trigger with fallback estimate")
	}
}

// TestShouldCompactFallbackWithSafetyMargin verifies that when no Usage data
// is available, we use the character heuristic with a 15% safety margin.
func TestShouldCompactFallbackWithSafetyMargin(t *testing.T) {
	rt := compactRuntime{
		Enabled:        true,
		TokenThreshold: 0.3, // 300 tokens threshold
		MinMessages:    3,
		WindowTokens:   1000,
	}

	msgs := []Message{
		{Role: "user", Content: "x"},
		{Role: "assistant", Content: strings.Repeat("y", 3000)}, // ~750 tokens
		{Role: "user", Content: "z"},
	}

	need, used := shouldCompact(msgs, rt)
	// With no Usage data: estimate is ~860 tokens (3000 chars / 4 + framing + safety),
	// with 15% margin applied. This should exceed the 300 token threshold.
	if !need {
		t.Errorf("fallback estimate should trigger: used=%d, threshold=%d", used, int(float64(rt.WindowTokens)*rt.TokenThreshold))
	}
}

func TestBuildSummaryPromptIncludesStructuredTemplate(t *testing.T) {
	middle := []Message{{Role: "user", Content: "hi"}}
	prompt, _ := buildSummaryPrompt(middle, 50000, "")
	wantSections := []string{"## Goal", "## Constraints & Preferences", "## Progress", "### Done", "### In Progress", "### Blocked", "## Key Decisions", "## Next Steps", "## Critical Context", "## Relevant Files"}
	for _, s := range wantSections {
		if !strings.Contains(prompt, s) {
			t.Errorf("structured template missing section %q", s)
		}
	}
}

func TestBuildSummaryPromptIncludesPreviousSummaryWhenProvided(t *testing.T) {
	middle := []Message{{Role: "user", Content: "new turn"}}
	prev := "## Goal\n- prior task summary\n\n## Progress\n### Done\n- read foo.go"
	prompt, _ := buildSummaryPrompt(middle, 50000, prev)
	if !strings.Contains(prompt, "<previous-summary>") {
		t.Errorf("anchor tag missing")
	}
	if !strings.Contains(prompt, "prior task summary") {
		t.Errorf("previous summary body missing")
	}
	if !strings.Contains(prompt, "Update the summary above") {
		t.Errorf("update instruction missing")
	}
}

func TestBuildSummaryPromptCreateInstructionWhenNoPrevious(t *testing.T) {
	prompt, _ := buildSummaryPrompt([]Message{{Role: "user", Content: "x"}}, 50000, "")
	if !strings.Contains(prompt, "Create a new summary") {
		t.Errorf("create instruction missing")
	}
	// The instruction text references <previous-summary> in passing; only
	// the multi-line opening tag (followed by a newline) signals an active anchor.
	if strings.Contains(prompt, "<previous-summary>\n") {
		t.Errorf("anchor block should not be active without previous summary")
	}
}

func TestBuildSummaryPromptRespectsCapWithPreviousSummary(t *testing.T) {
	prev := strings.Repeat("p", 6000)
	middle := []Message{
		{Role: "user", Content: strings.Repeat("x", 4000)},
		{Role: "user", Content: "keepme"},
	}
	prompt, dropped := buildSummaryPrompt(middle, 1000, prev)
	if len(prompt) > 1000*charsPerToken {
		t.Fatalf("prompt exceeded cap: got %d want <= %d", len(prompt), 1000*charsPerToken)
	}
	if !strings.Contains(prompt, "keepme") {
		t.Fatalf("most recent message must remain after fitting prompt")
	}
	if dropped == 0 {
		t.Fatalf("expected oldest message to drop when previous summary consumes budget")
	}
	if !strings.Contains(prompt, "<previous-summary>") {
		t.Fatalf("anchored prompt missing previous-summary block")
	}
	if !strings.Contains(prompt, "chars truncated") {
		t.Fatalf("expected previous summary or conversation segment to be truncated to fit cap")
	}
	if !strings.Contains(prompt, "[NOTE: 1 earlier messages omitted") {
		t.Fatalf("expected omission note after dropping oldest fragment")
	}
}

func TestFindPreviousSummary(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "[ocode:environment]\nenv"},
		{Role: "user", Content: "ask"},
		{Role: "assistant", Content: "ok"},
		{Role: "system", Content: compactionSummaryMarker + "\nheader\n\nbody-1"},
		{Role: "user", Content: "more"},
		{Role: "assistant", Content: "yep"},
	}
	body, idx := findPreviousSummary(msgs)
	if idx != 3 {
		t.Errorf("expected idx=3, got %d", idx)
	}
	if !strings.Contains(body, "body-1") {
		t.Errorf("body missing content: %q", body)
	}
	if strings.HasPrefix(body, compactionSummaryMarker) {
		t.Errorf("marker not stripped from returned body")
	}
}

func TestFindPreviousSummaryReturnsLatest(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: compactionSummaryMarker + "\n\nfirst"},
		{Role: "user", Content: "x"},
		{Role: "system", Content: compactionSummaryMarker + "\n\nsecond"},
	}
	body, idx := findPreviousSummary(msgs)
	if idx != 2 {
		t.Errorf("expected idx=2 (latest), got %d", idx)
	}
	if !strings.Contains(body, "second") {
		t.Errorf("expected latest 'second', got %q", body)
	}
}

func TestFindPreviousSummaryNone(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "[ocode:environment]\nenv"},
		{Role: "user", Content: "ask"},
		{Role: "assistant", Content: "ok"},
	}
	body, idx := findPreviousSummary(msgs)
	if idx != -1 {
		t.Errorf("expected idx=-1, got %d", idx)
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestPruneToolResultsShrinksLargeOutputs(t *testing.T) {
	big := strings.Repeat("y", 5000)
	middle := []Message{
		{Role: "user", Content: "ask"},
		{Role: "assistant", ToolCalls: []ToolCall{tcCall("c1", "read")}},
		{Role: "tool", ToolID: "c1", Content: big},
		{Role: "assistant", Content: "done"},
	}
	pruned := pruneToolResults(middle, 2000)
	if len(pruned) != len(middle) {
		t.Fatalf("length changed: got %d want %d", len(pruned), len(middle))
	}
	if len(pruned[2].Content) >= len(big) {
		t.Errorf("tool content not pruned: len=%d", len(pruned[2].Content))
	}
	if !strings.Contains(pruned[2].Content, "pruned") {
		t.Errorf("pruned tool content missing pruned marker: %q", pruned[2].Content)
	}
	// Originals must not be mutated.
	if len(middle[2].Content) != len(big) {
		t.Errorf("pruneToolResults mutated original slice")
	}
}

func TestPruneToolResultsLeavesSmallContentAlone(t *testing.T) {
	middle := []Message{
		{Role: "tool", ToolID: "c1", Content: "small output"},
		{Role: "assistant", Content: "ok"},
	}
	pruned := pruneToolResults(middle, 2000)
	if pruned[0].Content != "small output" {
		t.Errorf("small content was modified: %q", pruned[0].Content)
	}
}
