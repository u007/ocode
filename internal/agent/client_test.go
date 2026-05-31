package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOpenAIResponsesAutoFillsMissingOutput(t *testing.T) {
	// Simulate the input building logic from chatOpenAIResponses.
	// We have a function_call without a matching function_call_output.

	input := []map[string]interface{}{
		{
			"type":      "function_call",
			"call_id":   "call-1",
			"name":      "bash",
			"arguments": `{"command":"ls"}`,
		},
	}

	// Apply the auto-fill logic (lines 479-500 of client.go).
	outputIDs := make(map[string]bool)
	for _, item := range input {
		if item["type"] == "function_call_output" {
			if id, ok := item["call_id"].(string); ok {
				outputIDs[id] = true
			}
		}
	}
	for _, item := range input {
		if item["type"] == "function_call" {
			if id, ok := item["call_id"].(string); ok && !outputIDs[id] {
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": id,
					"output":  "error: tool result missing",
				})
				outputIDs[id] = true
			}
		}
	}

	// Verify the output was added.
	if len(input) != 2 {
		t.Fatalf("expected 2 items (call + output), got %d", len(input))
	}

	output := input[1]
	if output["type"] != "function_call_output" {
		t.Fatalf("expected type function_call_output, got %v", output["type"])
	}
	if output["call_id"] != "call-1" {
		t.Fatalf("expected call_id call-1, got %v", output["call_id"])
	}
	if output["output"] != "error: tool result missing" {
		t.Fatalf("expected error message, got %v", output["output"])
	}
}

func TestOpenAIResponsesPreservesExistingOutput(t *testing.T) {
	// When a call already has an output, it should not be replaced.

	input := []map[string]interface{}{
		{
			"type":      "function_call",
			"call_id":   "call-1",
			"name":      "bash",
			"arguments": `{"command":"ls"}`,
		},
		{
			"type":    "function_call_output",
			"call_id": "call-1",
			"output":  "file1.txt\nfile2.txt",
		},
	}

	outputIDs := make(map[string]bool)
	for _, item := range input {
		if item["type"] == "function_call_output" {
			if id, ok := item["call_id"].(string); ok {
				outputIDs[id] = true
			}
		}
	}
	for _, item := range input {
		if item["type"] == "function_call" {
			if id, ok := item["call_id"].(string); ok && !outputIDs[id] {
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": id,
					"output":  "error: tool result missing",
				})
				outputIDs[id] = true
			}
		}
	}

	// Should still have 2 items (no extra auto-fill).
	if len(input) != 2 {
		t.Fatalf("expected 2 items (no auto-fill), got %d", len(input))
	}

	output := input[1]
	if output["output"] != "file1.txt\nfile2.txt" {
		t.Fatalf("expected existing output preserved, got %v", output["output"])
	}
}

func TestOpenAIResponsesHandlesMultipleMissingOutputs(t *testing.T) {
	// Multiple calls without outputs should each get a placeholder.

	input := []map[string]interface{}{
		{
			"type":      "function_call",
			"call_id":   "call-1",
			"name":      "bash",
			"arguments": `{"command":"ls"}`,
		},
		{
			"type":      "function_call",
			"call_id":   "call-2",
			"name":      "read",
			"arguments": `{"path":"file.txt"}`,
		},
		{
			"type":    "function_call_output",
			"call_id": "call-1",
			"output":  "existing",
		},
	}

	outputIDs := make(map[string]bool)
	for _, item := range input {
		if item["type"] == "function_call_output" {
			if id, ok := item["call_id"].(string); ok {
				outputIDs[id] = true
			}
		}
	}
	for _, item := range input {
		if item["type"] == "function_call" {
			if id, ok := item["call_id"].(string); ok && !outputIDs[id] {
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": id,
					"output":  "error: tool result missing",
				})
				outputIDs[id] = true
			}
		}
	}

	// Should have 4 items: 2 calls + 2 outputs.
	if len(input) != 4 {
		t.Fatalf("expected 4 items (2 calls + 2 outputs), got %d", len(input))
	}

	// call-1 has existing output.
	if input[2]["output"] != "existing" {
		t.Fatalf("expected existing output for call-1, got %v", input[2]["output"])
	}

	// call-2 should have auto-filled output.
	call2Output := input[3]
	if call2Output["call_id"] != "call-2" {
		t.Fatalf("expected call-2 output, got call_id %v", call2Output["call_id"])
	}
	if call2Output["output"] != "error: tool result missing" {
		t.Fatalf("expected error placeholder for call-2, got %v", call2Output["output"])
	}
}

func TestOpenAIResponsesNoCallsNoAutoFill(t *testing.T) {
	// If there are no function_calls, no outputs should be added.

	input := []map[string]interface{}{
		{
			"type":    "message",
			"role":    "user",
			"content": "hello",
		},
	}

	outputIDs := make(map[string]bool)
	for _, item := range input {
		if item["type"] == "function_call_output" {
			if id, ok := item["call_id"].(string); ok {
				outputIDs[id] = true
			}
		}
	}
	for _, item := range input {
		if item["type"] == "function_call" {
			if id, ok := item["call_id"].(string); ok && !outputIDs[id] {
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": id,
					"output":  "error: tool result missing",
				})
				outputIDs[id] = true
			}
		}
	}

	// Should still have 1 item (no auto-fill).
	if len(input) != 1 {
		t.Fatalf("expected 1 item (no auto-fill), got %d", len(input))
	}
}

func TestOpenAIResponsesHandlesJSONArguments(t *testing.T) {
	// Test that JSON arguments are preserved correctly during auto-fill.

	input := []map[string]interface{}{
		{
			"type":      "function_call",
			"call_id":   "call-1",
			"name":      "function",
			"arguments": json.RawMessage(`{"key":"value"}`),
		},
	}

	outputIDs := make(map[string]bool)
	for _, item := range input {
		if item["type"] == "function_call_output" {
			if id, ok := item["call_id"].(string); ok {
				outputIDs[id] = true
			}
		}
	}
	for _, item := range input {
		if item["type"] == "function_call" {
			if id, ok := item["call_id"].(string); ok && !outputIDs[id] {
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": id,
					"output":  "error: tool result missing",
				})
				outputIDs[id] = true
			}
		}
	}

	// Verify both items exist and arguments are intact.
	if len(input) != 2 {
		t.Fatalf("expected 2 items, got %d", len(input))
	}

	if input[0]["call_id"] != "call-1" {
		t.Fatalf("expected call_id preserved, got %v", input[0]["call_id"])
	}
	if input[1]["output"] != "error: tool result missing" {
		t.Fatalf("expected auto-filled output, got %v", input[1]["output"])
	}
}

// ---------------------------------------------------------------------------
// Streaming-parser regression tests (review fixes 8, 9, 17)
// ---------------------------------------------------------------------------

func TestParseOpenAIChatCompletionsStream_MultiToolCall(t *testing.T) {
	// Two tool calls streamed across multiple chunks with indices 0 and 1;
	// arguments arrive as partial fragments. The parser must assemble both
	// in index order with concatenated arguments.
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-a","function":{"name":"bash","arguments":"{\"cmd"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"ls\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call-b","function":{"name":"read","arguments":"{\"path\":\"a.txt\"}"}}]}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), nil, nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if msg == nil {
		t.Fatal("nil msg")
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].ID != "call-a" || msg.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("tool 0 mismatch: %+v", msg.ToolCalls[0])
	}
	if msg.ToolCalls[0].Function.Arguments != `{"cmd":"ls"}` {
		t.Fatalf("tool 0 arguments not reassembled: %q", msg.ToolCalls[0].Function.Arguments)
	}
	if msg.ToolCalls[1].ID != "call-b" || msg.ToolCalls[1].Function.Name != "read" {
		t.Fatalf("tool 1 mismatch: %+v", msg.ToolCalls[1])
	}
}

func TestParseOpenAIChatCompletionsStream_InlineThinkTags(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"A<think>B</think>C"}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	var deltas []string
	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), func(kind, text string) {
		deltas = append(deltas, kind+":"+text)
	}, nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if msg == nil {
		t.Fatal("nil msg")
	}
	if got := msg.Content; got != "AC" {
		t.Fatalf("content mismatch: got %q want %q", got, "AC")
	}
	if got := msg.ReasoningContent; got != "B" {
		t.Fatalf("reasoning mismatch: got %q want %q", got, "B")
	}
	got := strings.Join(deltas, "|")
	want := "text:A|reasoning:B|text:C"
	if got != want {
		t.Fatalf("delta sequence mismatch: got %q want %q", got, want)
	}
}

func TestParseOpenAIChatCompletionsStream_InlineThinkTagsAcrossChunks(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"A<thi"}}]}`,
		`data: {"choices":[{"delta":{"content":"nk>BC"}}]}`,
		`data: {"choices":[{"delta":{"content":"</thi"}}]}`,
		`data: {"choices":[{"delta":{"content":"nk>D"}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	var deltas []string
	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), func(kind, text string) {
		deltas = append(deltas, kind+":"+text)
	}, nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if msg == nil {
		t.Fatal("nil msg")
	}
	if got := msg.Content; got != "AD" {
		t.Fatalf("content mismatch: got %q want %q", got, "AD")
	}
	if got := msg.ReasoningContent; got != "BC" {
		t.Fatalf("reasoning mismatch: got %q want %q", got, "BC")
	}
	got := strings.Join(deltas, "|")
	want := "text:A|reasoning:BC|text:D"
	if got != want {
		t.Fatalf("delta sequence mismatch: got %q want %q", got, want)
	}
}

func TestParseOpenAIChatCompletionsStream_ExplicitReasoningSkipsInlineSplit(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"X<think>Y</think>Z","reasoning_content":"R"}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	var deltas []string
	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), func(kind, text string) {
		deltas = append(deltas, kind+":"+text)
	}, nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if msg == nil {
		t.Fatal("nil msg")
	}
	if got := msg.Content; got != "X<think>Y</think>Z" {
		t.Fatalf("content mismatch: got %q", got)
	}
	if got := msg.ReasoningContent; got != "R" {
		t.Fatalf("reasoning mismatch: got %q want %q", got, "R")
	}
	got := strings.Join(deltas, "|")
	want := "text:X<think>Y</think>Z|reasoning:R"
	if got != want {
		t.Fatalf("delta sequence mismatch: got %q want %q", got, want)
	}
}

func TestChatAnthropic_TruncatedToolJSONFallsBackToEmptyObject(t *testing.T) {
	// Spin up a fake Anthropic endpoint that emits a tool_use block whose
	// input_json_delta fragments do NOT assemble into valid JSON, then ends
	// without a usable signature. The client must catch !json.Valid and fall
	// back to "{}" so the tool call is still dispatched.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fw := w.(http.Flusher)
		write := func(s string) { _, _ = io.WriteString(w, s); fw.Flush() }
		write("data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
		write("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_x\",\"name\":\"bash\"}}\n\n")
		// Deliberately broken / truncated JSON fragment.
		write("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\": \\\"ls\"}}\n\n")
		write("data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		write("data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	c := &GenericClient{
		APIKey:   "test",
		Model:    "claude-test",
		BaseURL:  srv.URL,
		Provider: "anthropic",
	}
	msg, err := c.chatAnthropic(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("chatAnthropic error: %v", err)
	}
	if msg == nil || len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %+v", msg)
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != "{}" {
		t.Fatalf("expected {} fallback, got %q", got)
	}
}

func TestChatAnthropic_OnUsageEmitsIncrementalDeltas(t *testing.T) {
	// Anthropic's message_start carries initial input/output_tokens and each
	// message_delta carries cumulative output_tokens. The streaming OnUsage
	// callback must convert these cumulative snapshots into incremental deltas
	// so subscribers like AgentRun.AddUsage do not over-count.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fw := w.(http.Flusher)
		write := func(s string) { _, _ = io.WriteString(w, s); fw.Flush() }
		write("data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-test\",\"usage\":{\"input_tokens\":100,\"output_tokens\":0}}}\n\n")
		write("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		write("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		// Cumulative output_tokens progression: 10 -> 25 -> 40.
		write("data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":100,\"output_tokens\":10}}\n\n")
		write("data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":100,\"output_tokens\":25}}\n\n")
		write("data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":100,\"output_tokens\":40}}\n\n")
		write("data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		write("data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	var mu sync.Mutex
	var sumIn, sumOut int64
	c := &GenericClient{
		APIKey:   "test",
		Model:    "claude-test",
		BaseURL:  srv.URL,
		Provider: "anthropic",
	}
	c.SetOnUsage(func(in, out int64) {
		mu.Lock()
		defer mu.Unlock()
		sumIn += in
		sumOut += out
	})
	if _, err := c.chatAnthropic(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("chatAnthropic error: %v", err)
	}
	// Subscriber summing deltas must see the final cumulative totals exactly,
	// not the sum of cumulative snapshots (which would be 100+100+100+100=400
	// in and 0+10+25+40=75 out).
	if sumIn != 100 {
		t.Fatalf("expected summed input deltas=100, got %d", sumIn)
	}
	if sumOut != 40 {
		t.Fatalf("expected summed output deltas=40, got %d", sumOut)
	}
}

func TestNormalizeLMStudioBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://localhost:1234", "http://localhost:1234/v1"},
		{"http://localhost:1234/", "http://localhost:1234/v1"},
		{"http://localhost:1234/v1", "http://localhost:1234/v1"},
		{"http://localhost:1234/v1/", "http://localhost:1234/v1"},
		{"http://host.example:8080/v1", "http://host.example:8080/v1"},
		{"  http://localhost:1234/v1  ", "http://localhost:1234/v1"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeLMStudioBaseURL(tc.in); got != tc.want {
			t.Errorf("normalizeLMStudioBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
