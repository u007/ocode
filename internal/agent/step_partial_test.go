package agent

import (
	"errors"
	"testing"
)

// scriptedClient replays a fixed script of Chat responses: entry i is returned
// on the i-th call, and an entry with err set fails that round.
type scriptedClient struct {
	calls int
	msgs  []*Message
	errs  []error
}

func (c *scriptedClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	i := c.calls
	c.calls++
	if i >= len(c.msgs) {
		return nil, errors.New("scriptedClient: no response scripted for call " + string(rune('0'+i)))
	}
	if c.errs[i] != nil {
		return nil, c.errs[i]
	}
	return c.msgs[i], nil
}

// newToolCall builds a ToolCall with the anonymous Function struct filled in.
func newToolCall(id, name, args string) ToolCall {
	tc := ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

func (c *scriptedClient) GetProvider() string { return "fake" }
func (c *scriptedClient) GetModel() string    { return "fake-model" }

// TestStepKeepsWorkDoneBeforeMidTurnError is the regression test for the
// transcript-loss half of the "streaming stops halfway and the messages are
// gone on reopen" bug: a turn that streamed real work (assistant text + tool
// rounds) and then hit an LLM error must hand that work back to the caller
// alongside the error. Returning a nil slice throws away everything the user
// already watched stream in, and the server's save path then has nothing to
// persist.
func TestStepKeepsWorkDoneBeforeMidTurnError(t *testing.T) {
	client := &scriptedClient{
		msgs: []*Message{
			{
				Role:      "assistant",
				Content:   "looking at the diff",
				ToolCalls: []ToolCall{newToolCall("call-1", "agent_status", "{}")},
			},
			nil,
		},
		errs: []error{nil, errors.New("stream closed by provider")},
	}

	a := NewAgent(client, nil, nil, nil)
	msgs, err := a.Step([]Message{{Role: "user", Content: "review changes"}})

	if err == nil {
		t.Fatal("expected the mid-turn LLM error to surface")
	}
	if len(msgs) == 0 {
		t.Fatal("Step dropped every message produced before the error; the streamed turn is unrecoverable")
	}
	var sawAssistant, sawToolResult bool
	for _, m := range msgs {
		if m.Role == "assistant" && m.Content == "looking at the diff" {
			sawAssistant = true
		}
		if m.Role == "tool" && m.ToolID == "call-1" {
			sawToolResult = true
		}
	}
	if !sawAssistant {
		t.Errorf("assistant text from the completed round was dropped: %+v", msgs)
	}
	if !sawToolResult {
		t.Errorf("tool result from the completed round was dropped: %+v", msgs)
	}
}
