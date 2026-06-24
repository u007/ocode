package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// TestExplorerDispatch_Integration makes real LLM calls and verifies the model
// produces a proper "task" tool call with agent="explore" when asked to search
// the codebase. Skip with -short flag or when no API key is available.
func TestExplorerDispatch_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Determine which provider/model to test from environment
	// Set OPENCODE_TEST_MODEL="opencode-go/deepseek-v4-flash" or similar to run
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

	// Build the tools list with the task tool definition
	// Create a minimal agent for TaskTool initialization
	minimalAgent := &Agent{
		client: client,
		config: cfg,
	}
	taskTool := TaskTool{mainAgent: minimalAgent, registry: DefaultAgentRegistry}
	tools := []map[string]interface{}{
		taskTool.Definition(),
	}

	// System prompt establishing the assistant's role
	systemMsg := Message{
		Role:    "system",
		Content: "You are a helpful assistant. When asked to search the codebase, use the task tool with agent=\"explore\" to delegate the search.",
	}

	// User message asking to search the codebase
	userMsg := Message{
		Role:    "user",
		Content: "Find all authentication-related files in the codebase. Look for files that handle user authentication, login, or session management.",
	}

	messages := []Message{systemMsg, userMsg}

	// Make the actual LLM call
	t.Logf("Calling model %s to search codebase", model)
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

	// Find the task tool call
	var taskCall *ToolCall
	for i := range resp.ToolCalls {
		if resp.ToolCalls[i].Function.Name == "task" {
			taskCall = &resp.ToolCalls[i]
			break
		}
	}

	if taskCall == nil {
		// Log what tool calls were made for debugging
		names := make([]string, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			names[i] = tc.Function.Name
		}
		t.Fatalf("expected 'task' tool call, got: %v; content: %s", names, truncate(resp.Content, 500))
	}

	t.Logf("Task tool call ID: %s", taskCall.ID)
	t.Logf("Task tool call arguments: %s", taskCall.Function.Arguments)

	// Parse the arguments to verify they contain agent="explore"
	// Models may use "agent" or "subagent_type" field (both accepted by TaskTool.Execute)
	var args struct {
		Prompt       string `json:"prompt"`
		Agent        string `json:"agent"`
		SubagentType string `json:"subagent_type"`
	}
	if err := json.Unmarshal([]byte(taskCall.Function.Arguments), &args); err != nil {
		t.Fatalf("failed to parse task tool arguments: %v", err)
	}

	// Verify the agent is explore (either via "agent" or "subagent_type" field)
	foundAgent := args.Agent
	if foundAgent == "" {
		foundAgent = args.SubagentType
	}
	if foundAgent != "explore" {
		t.Errorf("expected agent/agent_type 'explore', got agent=%q subagent_type=%q", args.Agent, args.SubagentType)
	}

	// Verify we have a prompt
	if args.Prompt == "" {
		t.Errorf("expected non-empty prompt in task tool call")
	}

	t.Logf("✓ Model produced valid task tool call with agent=explore, prompt: %s", truncate(args.Prompt, 100))
}

// TestExplorerDispatch_MultipleModels tests multiple models in sequence.
// Requires OPENCODE_TEST_MODELS="opencode-go/deepseek-v4-flash,opencode-go/mimo-v2.5,opencode-go/minimax-m3"
func TestExplorerDispatch_MultipleModels(t *testing.T) {
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
			TestExplorerDispatch_Integration(t)
		})
	}
}

// TestExplorerDispatch_WithTools verifies the model can use explore agent
// with specific tools available. This tests a more realistic scenario where
// the model has access to multiple tools.
func TestExplorerDispatch_WithTools(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

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

	// Build tools list with task tool and other common tools
	minimalAgent := &Agent{
		client: client,
		config: cfg,
	}
	taskTool := TaskTool{mainAgent: minimalAgent, registry: DefaultAgentRegistry}
	readTool := tool.ReadTool{}
	globTool := tool.GlobTool{}
	grepTool := tool.GrepTool{}

	tools := []map[string]interface{}{
		taskTool.Definition(),
		readTool.Definition(),
		globTool.Definition(),
		grepTool.Definition(),
	}

	// System prompt
	systemMsg := Message{
		Role:    "system",
		Content: "You are a helpful assistant. When asked to search the codebase, use the task tool with agent=\"explore\" to delegate the search.",
	}

	// User message
	userMsg := Message{
		Role:    "user",
		Content: "Find where the LLM client is initialized and how providers are configured.",
	}

	messages := []Message{systemMsg, userMsg}

	// Make the LLM call
	t.Logf("Calling model %s with multiple tools", model)
	resp, err := client.Chat(messages, tools)
	if err != nil {
		t.Fatalf("LLM call failed: %v", err)
	}

	if resp == nil {
		t.Fatal("received nil response from LLM")
	}

	// Verify the response contains a tool call
	if len(resp.ToolCalls) == 0 {
		t.Fatalf("expected tool calls in response, got none; content: %s", truncate(resp.Content, 500))
	}

	// Log all tool calls for debugging
	for i, tc := range resp.ToolCalls {
		t.Logf("Tool call %d: %s", i, tc.Function.Name)
	}

	// Find the task tool call
	var taskCall *ToolCall
	for i := range resp.ToolCalls {
		if resp.ToolCalls[i].Function.Name == "task" {
			taskCall = &resp.ToolCalls[i]
			break
		}
	}

	if taskCall == nil {
		names := make([]string, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			names[i] = tc.Function.Name
		}
		t.Fatalf("expected 'task' tool call, got: %v; content: %s", names, truncate(resp.Content, 500))
	}

	// Parse arguments
	// Models may use "agent" or "subagent_type" field (both accepted by TaskTool.Execute)
	var args struct {
		Prompt       string `json:"prompt"`
		Agent        string `json:"agent"`
		SubagentType string `json:"subagent_type"`
	}
	if err := json.Unmarshal([]byte(taskCall.Function.Arguments), &args); err != nil {
		t.Fatalf("failed to parse task tool arguments: %v", err)
	}

	// Verify the agent is explore (either via "agent" or "subagent_type" field)
	foundAgent := args.Agent
	if foundAgent == "" {
		foundAgent = args.SubagentType
	}
	if foundAgent != "explore" {
		t.Errorf("expected agent/agent_type 'explore', got agent=%q subagent_type=%q", args.Agent, args.SubagentType)
	}

	t.Logf("✓ Model produced valid task tool call with agent=explore")
}
