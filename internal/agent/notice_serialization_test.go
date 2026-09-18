package agent

import "testing"

// A UI-only transcript notice (an assistant message with empty content and no
// tool calls, carrying Message.Notice) carries nothing for the model. Anthropic
// drops zero-block messages, but the OpenAI-compatible converter used to emit
// {"role":"assistant","content":""}, which strict servers can reject. It must
// be skipped.
func TestConvertToOpenAIMessagesSkipsNoticeOnlyAssistant(t *testing.T) {
	c := &GenericClient{Provider: "openai", Model: "gpt-4o"}
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "", Notice: "↩ auto-continue — cut off by /max-step, resuming"},
		{Role: "assistant", Content: "done"},
	}

	out, err := c.convertToOpenAIMessages(msgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	for _, m := range out {
		if m["role"] != "assistant" {
			continue
		}
		s, _ := m["content"].(string)
		if s == "" && m["tool_calls"] == nil {
			t.Fatalf("empty notice-only assistant row was serialized: %+v", out)
		}
	}
	// The real assistant text must survive.
	sawDone := false
	for _, m := range out {
		if m["role"] == "assistant" && m["content"] == "done" {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatalf("real assistant turn dropped: %+v", out)
	}
}
