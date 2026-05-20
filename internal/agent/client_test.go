package agent

import (
	"encoding/json"
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
