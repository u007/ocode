package agent

import "testing"

func cacheMarked(m map[string]interface{}) bool {
	content := m["content"].([]interface{})
	last := content[len(content)-1].(map[string]interface{})
	_, ok := last["cache_control"]
	return ok
}

// The conversation breakpoints must sit on the most recent user turns so the
// next request reads the whole prior transcript from cache. A marker on the
// first user message (the old placement) caches nothing after turn one.
func TestApplyAnthropicConversationBreakpoints_LastTwoUserTurns(t *testing.T) {
	c := &GenericClient{}
	readCall := ToolCall{ID: "t1", Type: "function"}
	readCall.Function.Name = "read"
	readCall.Function.Arguments = "{}"
	msgs, err := c.buildAnthropicMessages([]Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "ok", ToolCalls: []ToolCall{readCall}},
		{Role: "tool", ToolID: "t1", Content: "file body"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	applyAnthropicConversationBreakpoints(msgs)

	var userIdx []int
	for i, m := range msgs {
		if m["role"] == "user" {
			userIdx = append(userIdx, i)
		}
	}
	if len(userIdx) != 3 {
		t.Fatalf("want 3 user-role messages (user, tool_result, user), got %d", len(userIdx))
	}
	if cacheMarked(msgs[userIdx[0]]) {
		t.Error("first user message must not carry a breakpoint")
	}
	if !cacheMarked(msgs[userIdx[1]]) {
		t.Error("tool_result turn (second-to-last user) must carry a breakpoint")
	}
	if !cacheMarked(msgs[userIdx[2]]) {
		t.Error("last user turn must carry a breakpoint")
	}
	for _, m := range msgs {
		if m["role"] == "assistant" && cacheMarked(m) {
			t.Error("assistant turns must not carry breakpoints")
		}
	}
}

func TestApplyAnthropicConversationBreakpoints_SingleTurn(t *testing.T) {
	c := &GenericClient{}
	msgs, err := c.buildAnthropicMessages([]Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	applyAnthropicConversationBreakpoints(msgs)
	if !cacheMarked(msgs[0]) {
		t.Fatal("sole user turn must carry a breakpoint")
	}
}
