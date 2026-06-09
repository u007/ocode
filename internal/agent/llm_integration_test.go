package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// TestReadToolCall_Integration makes a real LLM call and verifies the model
// produces a proper "read" tool call when asked to read a file.
// Skip with -short flag or when no API key is available.
func TestReadToolCall_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Determine which provider/model to test from environment
	// Set OPENCODE_TEST_MODEL="openai:gpt-4o" or similar to run
	model := os.Getenv("OPENCODE_TEST_MODEL")
	if model == "" {
		t.Skip("OPENCODE_TEST_MODEL not set, skipping integration test")
	}

	cfg := &config.Config{
		Model:    model,
		Provider: make(map[string]interface{}),
	}

	client := NewClient(cfg, model)
	if client == nil {
		t.Fatalf("failed to create client for model %s", model)
	}

	// Create a test file to read
	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.txt"
	expectedContent := "Hello from integration test\nLine 2 of test file"
	if err := os.WriteFile(testFile, []byte(expectedContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Build the tools list with the read tool definition
	readTool := tool.ReadTool{}
	tools := []map[string]interface{}{
		readTool.Definition(),
	}

	// System prompt establishing the assistant's role
	systemMsg := Message{
		Role:    "system",
		Content: "You are a helpful assistant. When asked to read a file, use the read tool.",
	}

	// User message asking to read the file
	userMsg := Message{
		Role:    "user",
		Content: "Please read the file at " + testFile,
	}

	messages := []Message{systemMsg, userMsg}

	// Make the actual LLM call
	t.Logf("Calling model %s to read file %s", model, testFile)
	resp, err := client.Chat(messages, tools)
	if err != nil {
		t.Fatalf("LLM call failed: %v", err)
	}

	if resp == nil {
		t.Fatal("received nil response from LLM")
	}

	t.Logf("Response role: %s", resp.Role)
	t.Logf("Response content length: %d", len(resp.Content))

	// Verify the response contains a tool call
	if len(resp.ToolCalls) == 0 {
		t.Fatalf("expected tool calls in response, got none; content: %s", truncate(resp.Content, 500))
	}

	// Find the read tool call
	var readCall *ToolCall
	for i := range resp.ToolCalls {
		if resp.ToolCalls[i].Function.Name == "read" {
			readCall = &resp.ToolCalls[i]
			break
		}
	}

	if readCall == nil {
		// Log what tool calls were made for debugging
		names := make([]string, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			names[i] = tc.Function.Name
		}
		t.Fatalf("expected 'read' tool call, got: %v; content: %s", names, truncate(resp.Content, 500))
	}

	t.Logf("Read tool call ID: %s", readCall.ID)
	t.Logf("Read tool call arguments: %s", readCall.Function.Arguments)

	// Parse the arguments to verify they contain the file path
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line,omitempty"`
		EndLine   int    `json:"end_line,omitempty"`
	}
	if err := json.Unmarshal([]byte(readCall.Function.Arguments), &args); err != nil {
		t.Fatalf("failed to parse read tool arguments: %v", err)
	}

	// Verify the path matches what we asked for
	if args.Path != testFile {
		t.Errorf("expected path %q, got %q", testFile, args.Path)
	}

	t.Logf("✓ Model produced valid read tool call for path: %s", args.Path)
}

// TestReadToolCall_MultipleModels tests multiple models in sequence.
// Requires OPENCODE_TEST_MODELS="openai:gpt-4o,anthropic:claude-sonnet-4-20250514"
func TestReadToolCall_MultipleModels(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	modelsEnv := os.Getenv("OPENCODE_TEST_MODELS")
	if modelsEnv == "" {
		t.Skip("OPENCODE_TEST_MODELS not set, skipping multi-model integration test")
	}

	models := strings.Split(modelsEnv, ",")
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}

		t.Run(model, func(t *testing.T) {
			t.Setenv("OPENCODE_TEST_MODEL", model)
			TestReadToolCall_Integration(t)
		})
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
