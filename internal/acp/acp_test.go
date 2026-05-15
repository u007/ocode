package acp

import (
	"encoding/json"
	"testing"
)

func TestInputMessageUnmarshal(t *testing.T) {
	data := `{"type":"message","content":"hello","sessionId":"test-123"}`
	var msg InputMessage
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Type != "message" {
		t.Errorf("expected type 'message', got %q", msg.Type)
	}
	if msg.Content != "hello" {
		t.Errorf("expected content 'hello', got %q", msg.Content)
	}
	if msg.SessionID != "test-123" {
		t.Errorf("expected sessionId 'test-123', got %q", msg.SessionID)
	}
}

func TestOutputMessageMarshal(t *testing.T) {
	msg := OutputMessage{
		Type:      "response",
		Content:   "world",
		SessionID: "test-123",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["type"] != "response" {
		t.Errorf("expected type 'response', got %v", parsed["type"])
	}
	if parsed["content"] != "world" {
		t.Errorf("expected content 'world', got %v", parsed["content"])
	}
}

func TestOutputMessageError(t *testing.T) {
	msg := OutputMessage{
		Type:    "error",
		Message: "something went wrong",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["message"] != "something went wrong" {
		t.Errorf("expected message 'something went wrong', got %v", parsed["message"])
	}
}
