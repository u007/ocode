package agent

import (
	"strings"
	"testing"
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
		{Role: "user", Content: "u1"},   // 1
		{Role: "assistant", Content: ""}, // 2
		{Role: "user", Content: "u2"},   // 3
		{Role: "assistant", Content: ""}, // 4
		{Role: "user", Content: "u3"},   // 5
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
	}
	for _, c := range cases {
		if got := findPrefixEnd(c.msgs); got != c.want {
			t.Errorf("%s: findPrefixEnd = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestBuildSummaryPromptIncludesToolInfo(t *testing.T) {
	middle := []Message{
		{Role: "user", Content: "edit foo.go"},
		{Role: "assistant", ToolCalls: []ToolCall{tcCall("c1", "edit_file")}},
		{Role: "tool", ToolID: "c1", Content: "wrote 12 lines"},
		{Role: "assistant", Content: "done"},
	}
	prompt, dropped := buildSummaryPrompt(middle, 50000)
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
	prompt, dropped := buildSummaryPrompt(middle, 1000)
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
