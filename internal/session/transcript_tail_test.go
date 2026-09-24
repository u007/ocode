package session

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// assistantText is a completed reply row.
func assistantText(content string) agent.Message {
	return agent.Message{Role: "assistant", Content: content}
}

// assistantToolCalls is an assistant row that only dispatched tools — no reply
// content landed, so the turn is still owed one.
func assistantToolCalls() agent.Message {
	return agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}}
}

// permissionAskContent is the tool-result a permission ask pauses on.
func permissionAskContent() string {
	return tool.SentinelPermissionAsk + `{"tool":"bash","reason":"rm -rf"}`
}

// questionAskContent is the tool-result an unanswered question pauses on.
func questionAskContent() string {
	return tool.SentinelQuestionPrompt + `[{"header":"Pick","question":"Which?"}]` +
		"\n\n" + tool.SentinelWaitingForUser
}

// dismissedQuestionContent is the in-place rewrite written when the user
// cancels a question prompt (Cancel / Escape / X).
func dismissedQuestionContent() string {
	return tool.QuestionDismissedResult
}

// TestTranscriptTailVerdict pins the single classification rule shared by the
// resident agent's memory and the decoded stored rows (spec §3.1).
func TestTranscriptTailVerdict(t *testing.T) {
	tests := []struct {
		name string
		msgs []agent.Message
		want transcriptTailVerdict
	}{
		{"empty transcript", nil, tailComplete},
		{"assistant with content", []agent.Message{assistantText("done")}, tailComplete},
		{
			"assistant with notice only",
			[]agent.Message{{Role: "assistant", Notice: "LSP server not installed"}},
			tailComplete,
		},
		{"assistant with only tool_calls", []agent.Message{assistantToolCalls()}, tailUnfinished},
		{"content-less assistant", []agent.Message{{Role: "assistant"}}, tailUnfinished},
		{"user row", []agent.Message{{Role: "user", Content: "hi"}}, tailUnfinished},
		{"answered-ask tool row", []agent.Message{
			assistantToolCalls(),
			{Role: "tool", ToolID: "call-1", Content: `{"answer":"Production"}`},
		}, tailUnfinished},
		{"unanswered permission sentinel", []agent.Message{
			assistantToolCalls(),
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent()},
		}, tailWaiting},
		{"unanswered question sentinel", []agent.Message{
			assistantToolCalls(),
			{Role: "tool", ToolID: "call-1", Content: questionAskContent()},
		}, tailWaiting},
		{
			"multi-ask round with one unanswered",
			[]agent.Message{
				assistantToolCalls(),
				{Role: "tool", ToolID: "call-1", Content: `{"answer":"yes"}`},
				{Role: "tool", ToolID: "call-2", Content: permissionAskContent()},
			},
			tailWaiting,
		},
		{"dismissed question tool row", []agent.Message{
			assistantToolCalls(),
			{Role: "tool", ToolID: "call-1", Content: dismissedQuestionContent()},
		}, tailStopped},
		{
			"dismissed question beside a completed tool result",
			[]agent.Message{
				assistantToolCalls(),
				{Role: "tool", ToolID: "call-1", Content: "bash output"},
				{Role: "tool", ToolID: "call-2", Content: dismissedQuestionContent()},
			},
			tailStopped,
		},
		{
			"multi-ask round with a dismissal and an unanswered ask",
			[]agent.Message{
				assistantToolCalls(),
				{Role: "tool", ToolID: "call-1", Content: dismissedQuestionContent()},
				{Role: "tool", ToolID: "call-2", Content: questionAskContent()},
			},
			tailWaiting,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := transcriptTailVerdictFor(tt.msgs); got != tt.want {
				t.Fatalf("verdict = %v, want %v", got, tt.want)
			}
			wantUnfinished := tt.want == tailUnfinished
			if got := TranscriptTailUnfinished(tt.msgs); got != wantUnfinished {
				t.Fatalf("TranscriptTailUnfinished = %v, want %v", got, wantUnfinished)
			}
		})
	}
}
