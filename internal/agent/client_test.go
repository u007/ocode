package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestReconcileOpenAIResponsesToolPairs_DropsOrphanedOutput(t *testing.T) {
	// A function_call_output whose call_id has no matching function_call
	// must be dropped — the Responses API rejects it with "No tool call
	// found for function call output with call_id ...".
	input := []map[string]interface{}{
		{
			"type":    "message",
			"role":    "user",
			"content": "do something",
		},
		{
			"type":    "function_call_output",
			"call_id": "call-orphan",
			"output":  "stray result",
		},
	}

	out := reconcileOpenAIResponsesToolPairs(input)

	for _, item := range out {
		if item["type"] == "function_call_output" && item["call_id"] == "call-orphan" {
			t.Fatalf("expected orphaned function_call_output to be dropped, got: %+v", out)
		}
	}
	if len(out) != 1 {
		t.Fatalf("expected only the message item to remain, got %d items: %+v", len(out), out)
	}
}

func TestReconcileOpenAIResponsesToolPairs_KeepsMatchedPair(t *testing.T) {
	input := []map[string]interface{}{
		{"type": "function_call", "call_id": "call-1", "name": "bash", "arguments": `{"command":"ls"}`},
		{"type": "function_call_output", "call_id": "call-1", "output": "ok"},
	}

	out := reconcileOpenAIResponsesToolPairs(input)

	if len(out) != 2 {
		t.Fatalf("expected matched pair to be preserved, got %d items: %+v", len(out), out)
	}
}

func TestReconcileOpenAIResponsesToolPairs_FillsMissingOutput(t *testing.T) {
	input := []map[string]interface{}{
		{"type": "function_call", "call_id": "call-1", "name": "bash", "arguments": `{"command":"ls"}`},
	}

	out := reconcileOpenAIResponsesToolPairs(input)

	if len(out) != 2 {
		t.Fatalf("expected auto-filled output, got %d items: %+v", len(out), out)
	}
	if out[1]["type"] != "function_call_output" || out[1]["call_id"] != "call-1" {
		t.Fatalf("expected synthesised output for call-1, got: %+v", out[1])
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

func TestParseOpenAIChatCompletionsStream_EmptyArgumentsNormalized(t *testing.T) {
	// An empty-string arguments field is what OpenAI-compatible providers emit
	// for a ZERO-PARAMETER tool (todoread and plan_exit both declare
	// `properties: {}`); providers differ on whether they send "{}" or omit the
	// field. It must normalize to "{}", not fail.
	//
	// This previously errored. That was wrong twice over: the error is not
	// matched by isRetryableLLMClientError, so it ended the turn instead of
	// being retried as its comment claimed, and it made every zero-parameter
	// tool call provider-dependent. Empty-argument hazards are caught where
	// they matter — exec.go rejects an empty bash command.
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-a","function":{"name":"todoread","arguments":""}}]}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), nil, nil)
	if err != nil {
		t.Fatalf("empty arguments on a zero-parameter tool must not error: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Arguments != "{}" {
		t.Fatalf("empty arguments not normalized to \"{}\": %q", msg.ToolCalls[0].Function.Arguments)
	}
}

func TestParseOpenAIChatCompletionsStream_MalformedArgumentsErrors(t *testing.T) {
	// Non-empty but unparseable arguments ARE fatal: executing them would run
	// a tool call the model did not actually express.
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-a","function":{"name":"bash","arguments":"{\"cmd\": "}}]}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), nil, nil)
	if err == nil {
		t.Fatalf("expected error for malformed arguments, got msg with %d tool calls", len(msg.ToolCalls))
	}
	if !strings.Contains(err.Error(), "invalid tool call arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseOpenAIChatCompletionsStream_SplitsParallelCallsSharingAnIndex(t *testing.T) {
	// Accumulation was keyed on tool_calls[i].index alone. When a provider
	// repeats or resets index across parallel calls, two distinct calls landed
	// in the same slot: the later name/id overwrote the earlier one and the
	// argument fragments were CONCATENATED into one string. The result is valid
	// JSON with duplicate keys, so it passed the json.Valid check below and was
	// executed last-wins — silently running one tool call the model never made
	// and dropping the other entirely.
	//
	// This is not hypothetical: 60 such calls are present in on-disk session
	// history, e.g. a task_status call whose arguments assembled to
	// {"task_id":"agent-run-2","task_id":"agent-run-3"}. Reproduced here.
	//
	// A delta carrying a different non-empty id is a new call regardless of
	// index, which is the one signal every provider must get right — the id is
	// what the tool result is correlated against.
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-a","function":{"name":"task_status","arguments":"{\"task_id\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"agent-run-2\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-b","function":{"name":"task_status","arguments":"{\"task_id\":\"agent-run-3\"}"}}]}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), nil, nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("two calls sharing an index must stay separate, got %d: %+v", len(msg.ToolCalls), msg.ToolCalls)
	}
	if msg.ToolCalls[0].ID != "call-a" || msg.ToolCalls[0].Function.Arguments != `{"task_id":"agent-run-2"}` {
		t.Fatalf("first call corrupted: %+v", msg.ToolCalls[0])
	}
	if msg.ToolCalls[1].ID != "call-b" || msg.ToolCalls[1].Function.Arguments != `{"task_id":"agent-run-3"}` {
		t.Fatalf("second call corrupted: %+v", msg.ToolCalls[1])
	}
}

func TestParseOpenAIChatCompletionsStream_RepeatedIDIsOneCall(t *testing.T) {
	// The converse guard: many providers echo the same id on every fragment of
	// a call. Splitting on "id is present" rather than "id CHANGED" would shatter
	// one call into one call per fragment, which is a worse failure than the bug
	// being fixed — so this pins the boundary.
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-a","function":{"name":"bash","arguments":"{\"command"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-a","function":{"arguments":"\":\"ls\"}"}}]}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), nil, nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("a repeated id is one call, got %d: %+v", len(msg.ToolCalls), msg.ToolCalls)
	}
	if msg.ToolCalls[0].Function.Arguments != `{"command":"ls"}` {
		t.Fatalf("arguments not reassembled: %q", msg.ToolCalls[0].Function.Arguments)
	}
}

func TestParseOpenAIChatCompletionsStream_PassesDuplicateKeyArgumentsThrough(t *testing.T) {
	// Duplicate keys used to be fatal here, which ended the whole turn: the
	// error matches nothing in isRetryableLLMClientError, so the model never
	// saw what was wrong and could not re-issue the call. The policy now lives
	// at the dispatch site, where a skipped call still gets a paired tool
	// result — see TestAgentSkipsDuplicateKeyToolArguments, which carries the
	// "never executes" guarantee this test used to hold.
	//
	// The parser's job ends at reporting what arrived, verbatim: it must not
	// drop, reorder, or rewrite the argument bytes on the way, or the dispatch
	// site cannot name both conflicting values.
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-a","function":{"name":"read","arguments":"{\"path\":\"a.txt\",\"path\":\"b.txt\"}"}}]}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	msg, _, err := parseOpenAIChatCompletionsStream(strings.NewReader(stream), nil, nil)
	if err != nil {
		t.Fatalf("duplicate keys are no longer a parse error: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != `{"path":"a.txt","path":"b.txt"}` {
		t.Fatalf("arguments must reach dispatch unmodified, got %s", got)
	}
}

func TestDuplicateTopLevelKey_OnlyInspectsTopLevel(t *testing.T) {
	// The check is deliberately top-level only. Measured against local session
	// history, 58 of the 60 duplicate-key tool calls repeat a top-level key
	// (`path`, `command`, `task_id`); the other 2 repeat a key inside a nested
	// array element (a `multiedit` edits[] entry). Recursing would add traversal
	// for those 2, and a nested repeat is far likelier to be legitimate
	// model-authored content than a merged call. This pins the boundary so it
	// stays a decision rather than an accident.
	key, first, second := duplicateTopLevelKey(`{"path":"a","end_line":1,"path":"b"}`)
	if key != "path" {
		t.Fatalf("top-level repeat must be reported, got %q", key)
	}
	// Both values, not just the key: the tool result is the model's only chance
	// to see which two values it gave. A message it cannot act on turns one
	// dead turn into a loop of them.
	if first != `"a"` || second != `"b"` {
		t.Fatalf("both conflicting values must be reported, got %s and %s", first, second)
	}
	if key, _, _ := duplicateTopLevelKey(`{"edits":[{"newString":"x","newString":"y"}]}`); key != "" {
		t.Fatalf("nested repeat must not be reported, got %q", key)
	}
	// A key reused across two different nested objects is not a repeat at all.
	if key, _, _ := duplicateTopLevelKey(`{"a":{"k":1},"b":{"k":2}}`); key != "" {
		t.Fatalf("same key in sibling nested objects is not a repeat, got %q", key)
	}
	if key, _, _ := duplicateTopLevelKey(`{}`); key != "" {
		t.Fatalf("empty object: %q", key)
	}
}

func TestChatOpenAI_RoutesOpenCodeGoGPT56ToResponses(t *testing.T) {
	// opencode-go/gpt-5.6-luna must be served via the OpenAI Responses API
	// (responses-lite), not chat/completions, or its tool calls come back empty.
	c := &GenericClient{Provider: "opencode-go", Model: "gpt-5.6-luna", APIKey: "k"}
	url := c.openAIResponsesURL()
	if !strings.HasSuffix(url, "/responses") {
		t.Fatalf("expected responses URL for opencode-go gpt-5.6, got %q", url)
	}
	if !openAICodexResponsesLite(c.Model) {
		t.Fatalf("gpt-5.6-luna should be detected as responses-lite")
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

func TestAccountIDFromCache(t *testing.T) {
	client := &GenericClient{
		Provider:  "openai",
		Model:     "gpt-5.4",
		APIKey:    "test-token",
		UseOAuth:  true,
		AccountID: "cached-account-123",
	}
	if client.AccountID != "cached-account-123" {
		t.Errorf("expected cached AccountID, got %q", client.AccountID)
	}
}

func TestAccountID_NotJWTParsed(t *testing.T) {
	client := &GenericClient{
		Provider:  "openai",
		Model:     "gpt-5.4",
		APIKey:    "not-a-real-jwt",
		UseOAuth:  true,
		AccountID: "from-cache",
	}
	if client.AccountID != "from-cache" {
		t.Errorf("expected AccountID from cache, got %q", client.AccountID)
	}
}

// TestChatOpenAI_MaxTokensFromContextOverride locks in the compaction fix:
// when a caller sets ctxKeyMaxTokens on the context, chatOpenAI must send
// that value as max_tokens instead of leaving it unset (which some
// OpenAI-compatible routers, e.g. opencode-go, default to the model's
// remaining context window rather than its real completion cap, and reject
// once that computed default exceeds it).
func TestChatOpenAI_MaxTokensFromContextOverride(t *testing.T) {
	var gotMaxTokens interface{}
	sawMaxTokensKey := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("bad request body: %v", err)
		}
		gotMaxTokens, sawMaxTokensKey = payload["max_tokens"]
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fw := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fw.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fw.Flush()
	}))
	defer srv.Close()

	c := &GenericClient{
		APIKey:   "test",
		Model:    "deepseek-v4-flash",
		BaseURL:  srv.URL,
		Provider: "opencode-go",
	}

	ctx := context.WithValue(context.Background(), ctxKeyMaxTokens, 8192)
	if _, err := c.chatOpenAI(ctx, []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("chatOpenAI error: %v", err)
	}
	if !sawMaxTokensKey {
		t.Fatalf("expected max_tokens in payload, got none")
	}
	if got, want := gotMaxTokens, float64(8192); got != want {
		t.Fatalf("max_tokens = %v, want %v", got, want)
	}

	// Without the context override, max_tokens must stay unset — this must
	// not become the default behaviour for every OpenAI-compatible call.
	sawMaxTokensKey = false
	if _, err := c.chatOpenAI(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("chatOpenAI error: %v", err)
	}
	if sawMaxTokensKey {
		t.Fatalf("expected max_tokens omitted without override, got %v", gotMaxTokens)
	}
}

func TestChatOpenAIOrcaRouterUsesQualifiedRouterModel(t *testing.T) {
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("bad request body: %v", err)
		}
		gotModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := &GenericClient{
		APIKey:   "test",
		Model:    "auto",
		BaseURL:  srv.URL,
		Provider: "orcarouter",
	}
	if _, err := c.chatOpenAI(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("chatOpenAI error: %v", err)
	}
	if gotModel != "orcarouter/auto" {
		t.Fatalf("request model = %q, want %q", gotModel, "orcarouter/auto")
	}
}

// statusResponse builds a minimal HTTP response with the given status and body
// for stubbing llmHTTPClient in retry-classification tests.
func statusResponse(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// openAIChatOKStream is a minimal successful chat-completions SSE stream that
// parseOpenAIChatCompletionsStream accepts (one content delta + DONE).
const openAIChatOKStream = "data: {\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n"

// stubLLMHTTP replaces the package-level llmHTTPClient for the duration of a
// test and restores it (plus llmRetryBaseDelay, zeroed to keep retries fast)
// on cleanup.
func stubLLMHTTP(t *testing.T, transport roundTripFunc) {
	t.Helper()
	originalClient := llmHTTPClient
	originalDelay := llmRetryBaseDelay
	llmRetryBaseDelay = 0
	llmHTTPClient = &http.Client{Timeout: llmRequestTimeout, Transport: transport}
	t.Cleanup(func() {
		llmHTTPClient = originalClient
		llmRetryBaseDelay = originalDelay
	})
}

func TestChatRetries503ThenSucceeds(t *testing.T) {
	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			return statusResponse(http.StatusServiceUnavailable, `{"error":{"message":"upstream unavailable"}}`), nil
		}
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	msg, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected success after 503 retries, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts (2x503 + success), got %d", got)
	}
	if msg == nil || msg.Content != "ok" {
		t.Fatalf("expected final message content %q, got %+v", "ok", msg)
	}
}

func TestChatRetriesGatewayStatusCodes(t *testing.T) {
	for _, code := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var calls int32
			stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				n := atomic.AddInt32(&calls, 1)
				if n == 1 {
					return statusResponse(code, `{"error":"gateway blip"}`), nil
				}
				return statusResponse(http.StatusOK, openAIChatOKStream), nil
			}))

			client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
			if _, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil); err != nil {
				t.Fatalf("code %d: expected transient gateway error to be retried to success, got %v", code, err)
			}
			if got := atomic.LoadInt32(&calls); got != 2 {
				t.Fatalf("code %d: expected 2 attempts, got %d", code, got)
			}
		})
	}
}

func TestChatFailsFastOn500(t *testing.T) {
	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return statusResponse(http.StatusInternalServerError, `{"error":{"message":"boom"}}`), nil
	}))

	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	_, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected fail-fast after 1 attempt for non-gateway 5xx, got %d calls", got)
	}
	if !strings.Contains(err.Error(), "llm request failed after 1 attempt(s)") ||
		!strings.Contains(err.Error(), "opencode error (500)") {
		t.Fatalf("unexpected error format: %v", err)
	}
}

func TestStatusErrorBodyTextDoesNotCauseRetry(t *testing.T) {
	// Deliberate behavior change pinned here: typed provider status errors are
	// classified purely by HTTP code. A 500 whose BODY text contains
	// "timeout"/"eof" must NOT become retryable via the legacy substring
	// checks (it silently was before the typed error existed).
	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return statusResponse(http.StatusInternalServerError, `upstream timeout occurred while reading EOF`), nil
	}))

	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	_, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("body text must not influence status-error classification; expected 1 call, got %d", got)
	}
	var se *providerStatusError
	if !errors.As(err, &se) || se.Code != http.StatusInternalServerError {
		t.Fatalf("expected wrapped *providerStatusError with code 500, got %v", err)
	}
}

func TestEmptyStreamRetriesThenSucceeds(t *testing.T) {
	// A 200 whose SSE stream parses to no content/tool calls surfaces as
	// ErrNoResponseFromProvider, which is retryable.
	emptyStream := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: [DONE]\n"
	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return statusResponse(http.StatusOK, emptyStream), nil
		}
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	msg, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected empty-stream error to be retried to success, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
	if msg == nil || msg.Content != "ok" {
		t.Fatalf("expected final message content %q, got %+v", "ok", msg)
	}
}

// TestEmptyResponsesRetryAfterReasoningDeltas reproduces the bug where an
// OpenAI Responses-API empty-response error (no text, no tool calls) arrives
// AFTER reasoning-summary deltas were already streamed. The deltaEmitted gate
// must NOT suppress the retry for this error class: the final response produced
// nothing of substance, so retrying is safe (only cosmetic reasoning text can
// duplicate). Before the fix this failed fast at 1 attempt.
func TestEmptyResponsesRetryAfterReasoningDeltas(t *testing.T) {
	var calls int32
	// Attempt 1: reasoning deltas then an empty final response.
	emptyWithReasoning := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"model\":\"gpt-5.6-test\"}\n\n" +
		"event: response.reasoning_summary_text.delta\n" +
		"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"hmm thinking\"}\n\n" +
		"data: [DONE]\n"
	// Attempt 2: a normal successful response.
	okResponses := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"model\":\"gpt-5.6-test\"}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"model\":\"gpt-5.6-test\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n" +
		"data: [DONE]\n"

	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return statusResponse(http.StatusOK, emptyWithReasoning), nil
		}
		return statusResponse(http.StatusOK, okResponses), nil
	}))

	// opencode-go + gpt-5.6 prefix routes Chat → chatOpenAIResponses.
	client := &GenericClient{Provider: "opencode-go", Model: "gpt-5.6-test", BaseURL: "https://example.test/v1"}
	// OnDelta set so the reasoning delta arms deltaEmitted (the bug condition).
	client.OnDelta = func(kind, text string) {}
	msg, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected empty-response error to be retried to success, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 attempts (empty w/ reasoning deltas retried), got %d", got)
	}
	if msg == nil || msg.Content != "ok" {
		t.Fatalf("expected final message content %q, got %+v", "ok", msg)
	}
}

func TestIsRetryableLLMClientError_HTTP2StreamErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"internal_error retries", errors.New("openai responses stream error: stream error: stream ID 3; INTERNAL_ERROR; received from peer"), true},
		{"refused_stream retries", errors.New("stream error: stream ID 5; REFUSED_STREAM"), true},
		{"enhance_your_calm retries", errors.New("stream error: stream ID 7; ENHANCE_YOUR_CALM"), true},
		{"connect_error retries", errors.New("stream error: stream ID 9; CONNECT_ERROR"), true},
		{"protocol_error does not retry (client-caused, would fail identically)", errors.New("stream error: stream ID 3; PROTOCOL_ERROR"), false},
		{"frame_size_error does not retry (client-caused)", errors.New("stream error: stream ID 3; FRAME_SIZE_ERROR"), false},
		{"unrelated error does not retry", errors.New("some other failure"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableLLMClientError(tc.err); got != tc.want {
				t.Errorf("isRetryableLLMClientError(%q) = %v, want %v", tc.err.Error(), got, tc.want)
			}
		})
	}
}

func TestProviderStatusErrorClassification(t *testing.T) {
	cases := []struct {
		code              int
		wantServerUnavail bool
		wantRateLimit     bool
	}{
		{http.StatusBadRequest, false, false},
		{http.StatusUnauthorized, false, false},
		{http.StatusTooManyRequests, false, true},
		{http.StatusInternalServerError, false, false},
		{http.StatusBadGateway, true, false},
		{http.StatusServiceUnavailable, true, false},
		{http.StatusGatewayTimeout, true, false},
	}
	for _, tc := range cases {
		err := &providerStatusError{Provider: "test-provider", Code: tc.code, Body: "boom"}
		if got := isServerUnavailableError(err); got != tc.wantServerUnavail {
			t.Errorf("code %d: isServerUnavailableError = %v, want %v", tc.code, got, tc.wantServerUnavail)
		}
		if got := isRateLimitError(err); got != tc.wantRateLimit {
			t.Errorf("code %d: isRateLimitError = %v, want %v", tc.code, got, tc.wantRateLimit)
		}
		if got := isRetryableLLMClientError(err); got {
			t.Errorf("code %d: typed status errors must never be retryable via body/transport checks", tc.code)
		}
	}

	// Message format preserved byte-for-byte so downstream string consumers
	// (isRateLimitError fallback, TUI " (429)" match) keep working.
	e := &providerStatusError{Provider: "test-provider", Code: 503, Body: "boom"}
	if got, want := e.Error(), "test-provider error (503): boom"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	// Untyped errors still classify via the legacy substring fallback.
	if !isRateLimitError(errors.New("x error (429): slow down")) {
		t.Fatal("untyped 429 substring fallback broken")
	}
	if isRateLimitError(errors.New("x error (503): down")) {
		t.Fatal("untyped 503 must not classify as rate limit")
	}

	// Classification works through the final %w wrap produced by Chat.
	wrapped := fmt.Errorf("llm request failed after 1 attempt(s): %w",
		&providerStatusError{Provider: "p", Code: http.StatusServiceUnavailable, Body: ""})
	if !isServerUnavailableError(wrapped) {
		t.Fatal("errors.As must see through the attempt-count wrap")
	}
	if !strings.Contains(wrapped.Error(), "p error (503): ") {
		t.Fatalf("empty body must render trailing colon-space exactly, got %q", wrapped.Error())
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty", "", 0},
		{"zero", "0", 0},
		{"delta 5", "5", 5 * time.Second},
		{"delta 10", "10", 10 * time.Second},
		{"delta trimmed", "  5 ", 5 * time.Second},
		{"leading zeros", "01", 1 * time.Second},
		{"plus rejected", "+10", 0},
		{"minus rejected", "-5", 0},
		{"float rejected", "5.5", 0},
		{"bad", "bad", 0},
		{"empty with space", "   ", 0},
		{"overflow big", "99999999999999999999", 60 * time.Second},
		{"overflow just over maxSecs", "9223372037", 60 * time.Second},
		{"maxSecs ok", "9223372036", 9223372036 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRetryAfter(tc.in); got != tc.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
	// HTTP-date future (~10s)
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	if got < 9*time.Second || got > 11*time.Second {
		t.Errorf("parseRetryAfter future date %q = %v, want ~10s", future, got)
	}
	// HTTP-date past -> 0
	past := time.Now().Add(-10 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("parseRetryAfter past date %q = %v, want 0", past, got)
	}
	// Malformed date -> 0
	if got := parseRetryAfter("Fri, 99 Dec 9999 23:59:59 GMT"); got != 0 {
		t.Errorf("parseRetryAfter malformed date = %v, want 0", got)
	}
}

func TestRetryDelayFor429(t *testing.T) {
	// Normal 429 linear
	for i, want := range []time.Duration{3 * time.Second, 3400 * time.Millisecond, 3800 * time.Millisecond, 4200 * time.Millisecond, 4600 * time.Millisecond, 5 * time.Second} {
		err := &providerStatusError{Provider: "openai", Code: http.StatusTooManyRequests, Body: "rate limit"}
		if got := retryDelayFor429(err, i); got != want {
			t.Errorf("normal attempt %d: got %v want %v", i, got, want)
		}
	}
	// Hosted saturated exponential
	for i, want := range []time.Duration{3 * time.Second, 6 * time.Second, 12 * time.Second, 24 * time.Second, 48 * time.Second, 60 * time.Second} {
		err := &providerStatusError{Provider: "runinfra", Code: http.StatusTooManyRequests, Body: `{"error":{"code":"hosted_saturated_large_prompt","message":"deferred"}}`}
		if got := retryDelayFor429(err, i); got != want {
			t.Errorf("hosted attempt %d: got %v want %v", i, got, want)
		}
	}
	// Retry-After header overrides hosted and normal
	errRA := &providerStatusError{Provider: "runinfra", Code: http.StatusTooManyRequests, Body: `hosted_saturated_large_prompt`, RetryAfter: 10 * time.Second}
	if got := retryDelayFor429(errRA, 0); got != 10*time.Second {
		t.Errorf("RetryAfter override: got %v want 10s", got)
	}
	// Retry-After capped at 60s
	errRABig := &providerStatusError{Provider: "openai", Code: http.StatusTooManyRequests, Body: "rate limit", RetryAfter: 120 * time.Second}
	if got := retryDelayFor429(errRABig, 0); got != 60*time.Second {
		t.Errorf("RetryAfter cap: got %v want 60s", got)
	}
	// Untyped hosted detection
	untypedHosted := fmt.Errorf("runinfra error (429): %s", `{"error":{"code":"hosted_saturated_large_prompt"}}`)
	if got := retryDelayFor429(untypedHosted, 2); got != 12*time.Second {
		t.Errorf("untyped hosted: got %v want 12s", got)
	}
	if got := isHostedSaturatedError(untypedHosted); !got {
		t.Errorf("isHostedSaturatedError untyped false")
	}
	// Normal untyped 429 stays linear
	untypedNormal := fmt.Errorf("openai error (429): rate limit")
	if got := retryDelayFor429(untypedNormal, 1); got != 3400*time.Millisecond {
		t.Errorf("untyped normal: got %v want 3.4s", got)
	}
	if isHostedSaturatedError(untypedNormal) {
		t.Errorf("isHostedSaturatedError false positive")
	}
}

func TestChatWithContext_RetryAfterHeaderUsesHeaderDelay(t *testing.T) {
	// Verify that an HTTP 429 with Retry-After header drives the RetryNotifier delay
	// and that the request eventually succeeds. llmRetryWait is stubbed to avoid real sleep.
	origWait := llmRetryWait
	var capturedDelay time.Duration
	llmRetryWait = func(_ context.Context, d time.Duration) bool { capturedDelay = d; return true }
	t.Cleanup(func() { llmRetryWait = origWait })

	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			resp := statusResponse(http.StatusTooManyRequests, `{"error":{"code":"rate_limit","message":"slow down"}}`)
			resp.Header.Set("Retry-After", "2")
			return resp, nil
		}
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	var notifierDelay time.Duration
	client.RetryNotifier = func(_, _ int, delay time.Duration, _ error) { notifierDelay = delay }
	msg, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected success after Retry-After retry, got %v", err)
	}
	if msg == nil || msg.Content != "ok" {
		t.Fatalf("expected ok, got %+v", msg)
	}
	if notifierDelay != 2*time.Second {
		t.Fatalf("RetryNotifier delay = %v, want 2s", notifierDelay)
	}
	if capturedDelay != 2*time.Second {
		t.Fatalf("llmRetryWait delay = %v, want 2s", capturedDelay)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
}

func TestChatWithContext_HostedSaturatedExponentialDelay(t *testing.T) {
	origWait := llmRetryWait
	var delays []time.Duration
	llmRetryWait = func(_ context.Context, d time.Duration) bool { delays = append(delays, d); return true }
	t.Cleanup(func() { llmRetryWait = origWait })

	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 3 {
			return statusResponse(http.StatusTooManyRequests, `{"error":{"code":"hosted_saturated_large_prompt","message":"The shared hosted model is momentarily near its concurrency limit of 64"}}`), nil
		}
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	client := &GenericClient{Provider: "runinfra", Model: "test", BaseURL: "https://example.test/v1"}
	var notifierDelays []time.Duration
	client.RetryNotifier = func(_, _ int, delay time.Duration, _ error) { notifierDelays = append(notifierDelays, delay) }
	msg, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected success after hosted retries, got %v", err)
	}
	if msg.Content != "ok" {
		t.Fatalf("want ok, got %+v", msg)
	}
	want := []time.Duration{3 * time.Second, 6 * time.Second, 12 * time.Second}
	if len(notifierDelays) != len(want) {
		t.Fatalf("notifier delays = %v, want %v", notifierDelays, want)
	}
	for i, w := range want {
		if notifierDelays[i] != w {
			t.Errorf("attempt %d: notifier delay %v want %v", i, notifierDelays[i], w)
		}
		if delays[i] != w {
			t.Errorf("attempt %d: wait delay %v want %v", i, delays[i], w)
		}
	}
}

func TestChatWithContext_CancellationInterruptsRetry(t *testing.T) {
	// Cancellation during the retry wait must abort without sleeping the full delay.
	// We use a wait func that would block 10s, but context is cancelled after 50ms.
	origWait := llmRetryWait
	// Keep real wait for this test (we test the real implementation, not stub)
	llmRetryWait = origWait
	t.Cleanup(func() { llmRetryWait = origWait })

	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return statusResponse(http.StatusTooManyRequests, `rate limit`), nil
	}))

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the retry wait begins
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := client.ChatWithContext(ctx, []Message{{Role: "user", Content: "hi"}}, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	// Should have returned quickly (~50ms), not waited the full 3s retry delay
	if elapsed > 500*time.Millisecond {
		t.Fatalf("cancellation did not interrupt retry wait: elapsed %v > 500ms (expected ~50ms)", elapsed)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call before cancel, got %d", got)
	}
}

func TestOpencodeSessionHeaderStablePerClient(t *testing.T) {
	newReq := func() *http.Request {
		r, _ := http.NewRequest("POST", "https://opencode.ai/zen/go/v1/chat/completions", nil)
		return r
	}

	for _, provider := range []string{"opencode", "opencode-go"} {
		c := &GenericClient{Provider: provider}
		r1, r2 := newReq(), newReq()
		c.setOpencodeSessionHeader(r1)
		c.setOpencodeSessionHeader(r2)
		got := r1.Header.Get("x-opencode-session")
		if got == "" || got != r2.Header.Get("x-opencode-session") {
			t.Fatalf("%s: fallback session id must be non-empty and stable: %q vs %q", provider, got, r2.Header.Get("x-opencode-session"))
		}
		other := &GenericClient{Provider: provider}
		r3 := newReq()
		other.setOpencodeSessionHeader(r3)
		if r3.Header.Get("x-opencode-session") == got {
			t.Fatalf("%s: distinct clients must not share a fallback session id", provider)
		}
	}

	// Wired agent session id wins over the fallback.
	c := &GenericClient{Provider: "opencode-go", sessionID: "sess-123"}
	r := newReq()
	c.setOpencodeSessionHeader(r)
	if r.Header.Get("x-opencode-session") != "sess-123" {
		t.Fatalf("expected wired session id, got %q", r.Header.Get("x-opencode-session"))
	}

	// Non-opencode providers never send it.
	o := &GenericClient{Provider: "openai"}
	r = newReq()
	o.setOpencodeSessionHeader(r)
	if r.Header.Get("x-opencode-session") != "" {
		t.Fatalf("non-opencode provider must not send x-opencode-session")
	}
}

func TestChatOpenAIHTTPSendsOpencodeSessionHeader(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("x-opencode-session"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	c := &GenericClient{Provider: "opencode-go", Model: "mimo-v2.5", BaseURL: srv.URL, APIKey: "k"}
	for i := 0; i < 2; i++ {
		if _, err := c.chatOpenAIHTTP(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
			t.Fatalf("chatOpenAIHTTP: %v", err)
		}
	}
	if len(got) != 2 || got[0] == "" || got[0] != got[1] {
		t.Fatalf("x-opencode-session not sent/stable across requests: %v", got)
	}
}

func TestChatOpenAISendsOpencodeSessionHeader(t *testing.T) {
	var got string
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("x-opencode-session")
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	c := &GenericClient{Provider: "opencode-go", Model: "mimo-v2.5", BaseURL: "https://example.test/v1"}
	if _, err := c.chatOpenAI(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("chatOpenAI: %v", err)
	}
	if got == "" {
		t.Fatal("chatOpenAI did not send x-opencode-session")
	}
}

func TestChatOpenAIResponsesSendsOpencodeSessionHeader(t *testing.T) {
	var got string
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("x-opencode-session")
		body := "event: response.created\ndata: {\"type\":\"response.created\",\"model\":\"gpt-5.6-test\"}\n\n" +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"model\":\"gpt-5.6-test\"}\n\n" +
			"data: [DONE]\n"
		return statusResponse(http.StatusOK, body), nil
	}))

	c := &GenericClient{Provider: "opencode-go", Model: "gpt-5.6-test", BaseURL: "https://example.test/v1"}
	if _, err := c.chatOpenAIResponses(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("chatOpenAIResponses: %v", err)
	}
	if got == "" {
		t.Fatal("chatOpenAIResponses did not send x-opencode-session")
	}
}

func TestChatAnthropicSendsOpencodeSessionHeader(t *testing.T) {
	var got string
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("x-opencode-session")
		body := "data: {\"type\":\"message_start\",\"message\":{\"model\":\"minimax-m2\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n"
		return statusResponse(http.StatusOK, body), nil
	}))

	c := &GenericClient{Provider: "opencode-go", Model: "minimax-m2", BaseURL: "https://example.test/v1"}
	if _, err := c.chatAnthropic(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("chatAnthropic: %v", err)
	}
	if got == "" {
		t.Fatal("chatAnthropic did not send x-opencode-session")
	}
}
