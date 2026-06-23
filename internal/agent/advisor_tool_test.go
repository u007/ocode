package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// advisorMockLLMClient implements LLMClient for testing.
type advisorMockLLMClient struct {
	response   string
	toolCalls  []ToolCall
	shouldFail bool
}

func (m *advisorMockLLMClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	if m.shouldFail {
		return nil, assertAnError("mock failure")
	}
	return &Message{
		Role:      "assistant",
		Content:   m.response,
		ToolCalls: m.toolCalls,
	}, nil
}

func (m *advisorMockLLMClient) GetProvider() string { return "mock" }
func (m *advisorMockLLMClient) GetModel() string    { return "mock-model" }

func assertAnError(s string) error {
	return &advisorTestError{s: s}
}

type advisorTestError struct{ s string }

func (e *advisorTestError) Error() string { return e.s }

// advisorMockTool is a simple tool.Tool implementation for testing.
type advisorMockTool struct {
	name string
}

func (m *advisorMockTool) Name() string        { return m.name }
func (m *advisorMockTool) Description() string { return "mock tool: " + m.name }
func (m *advisorMockTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": m.name}
}
func (m *advisorMockTool) Execute(args json.RawMessage) (string, error) { return "mock result", nil }
func (m *advisorMockTool) Parallel() bool                               { return false }

func TestAdvisorTool_Name(t *testing.T) {
	tool := AdvisorTool{}
	if tool.Name() != "advisor" {
		t.Errorf("expected 'advisor', got %q", tool.Name())
	}
}

func TestAdvisorTool_Description(t *testing.T) {
	tool := AdvisorTool{}
	desc := tool.Description()
	if !strings.Contains(desc, "strategic advisor") {
		t.Errorf("description should mention strategic advisor, got: %s", desc)
	}
}

func TestAdvisorTool_Parallel(t *testing.T) {
	tool := AdvisorTool{}
	if tool.Parallel() {
		t.Error("advisor tool should not be parallel")
	}
}

func TestAdvisorTool_Definition(t *testing.T) {
	tool := AdvisorTool{}
	def := tool.Definition()
	if def["name"] != "advisor" {
		t.Errorf("definition name should be 'advisor', got %v", def["name"])
	}
	desc, _ := def["description"].(string)
	if !strings.Contains(desc, "gathered concrete findings") {
		t.Errorf("definition description should guide gathering concrete findings before calling advisor")
	}
	params, ok := def["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("definition should have parameters")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters should have properties")
	}
	if _, ok := props["prompt"]; !ok {
		t.Error("parameters should have 'prompt' property")
	}
	// providerID and modelID should NOT be exposed — model is preset via /advisor
	if _, ok := props["providerID"]; ok {
		t.Error("parameters should NOT have 'providerID' property (model is preset)")
	}
	if _, ok := props["modelID"]; ok {
		t.Error("parameters should NOT have 'modelID' property (model is preset)")
	}
	promptProp, _ := props["prompt"].(map[string]interface{})
	promptDesc, _ := promptProp["description"].(string)
	for _, want := range []string{"files already read with key findings", "grep/test/command outputs", "exact decision or questions"} {
		if !strings.Contains(promptDesc, want) {
			t.Errorf("prompt description missing %q", want)
		}
	}
	required, ok := params["required"].([]interface{})
	if !ok {
		// Could be a []string literal from the Go struct definition
		required2, ok2 := params["required"].([]string)
		if !ok2 {
			t.Fatal("parameters should have 'required' list as either []string or []interface{}")
		}
		if len(required2) != 1 || required2[0] != "prompt" {
			t.Errorf("required should be ['prompt'], got %v", required2)
		}
	} else {
		if len(required) != 1 || required[0] != "prompt" {
			t.Errorf("required should be ['prompt'], got %v", required)
		}
	}
}

func TestAdvisorTool_ResolveModel(t *testing.T) {
	tests := []struct {
		name     string
		envModel string
		cfgModel string
		cfgProv  string
		expected string
	}{
		{
			name:     "env var",
			envModel: "google/gemini-2.5-pro",
			expected: "google/gemini-2.5-pro",
		},
		{
			name:     "config provider+model",
			cfgProv:  "anthropic",
			cfgModel: "claude-opus-4",
			expected: "anthropic/claude-opus-4",
		},
		{
			name:     "config model only",
			cfgModel: "claude-sonnet-4-6",
			expected: "claude-sonnet-4-6",
		},
		{
			name:     "default fallback",
			expected: defaultAdvisorModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envModel != "" {
				t.Setenv("OPENCODE_ADVISOR_MODEL", tt.envModel)
			}
			tool := AdvisorTool{
				cfg: &config.Config{
					Ocode: config.OcodeConfig{
						Advisor: config.AdvisorConfig{
							Provider: tt.cfgProv,
							Model:    tt.cfgModel,
						},
					},
				},
			}
			got := tool.resolveModel()
			if got != tt.expected {
				t.Errorf("resolveModel() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAdvisorTool_Execute_EmptyPrompt(t *testing.T) {
	tool := AdvisorTool{}
	_, err := tool.Execute(json.RawMessage(`{"prompt":""}`))
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Errorf("expected prompt error, got %v", err)
	}
}

func TestAdvisorTool_Execute_RecursionGuard(t *testing.T) {
	advisorRecursionGuard.Store(true)
	defer advisorRecursionGuard.Store(false)

	tool := AdvisorTool{}
	_, err := tool.Execute(json.RawMessage(`{"prompt":"hello"}`))
	if err == nil || !strings.Contains(err.Error(), "recursively") {
		t.Errorf("expected recursion error, got %v", err)
	}
}

func TestAdvisorTool_getAdvisorTools_NilMainAgent(t *testing.T) {
	tool := AdvisorTool{}
	tools := tool.getAdvisorTools()
	if tools != nil {
		t.Errorf("expected nil tools when mainAgent is nil, got %d", len(tools))
	}
}

func TestAdvisorTool_getAdvisorTools_WithAgent(t *testing.T) {
	client := &advisorMockLLMClient{response: "test"}
	agent := NewAgent(client, nil, nil, nil)
	// NewAgent registers default tools (bash, bash_output, kill_shell, wait, etc.)
	// and we add a few more for testing.
	agent.tools["read"] = &advisorMockTool{name: "read"}
	agent.tools["glob"] = &advisorMockTool{name: "glob"}
	agent.tools["grep"] = &advisorMockTool{name: "grep"}
	agent.tools["list"] = &advisorMockTool{name: "list"}
	agent.tools["lsp"] = &advisorMockTool{name: "lsp"}

	tool := AdvisorTool{mainAgent: agent}
	advisorTools := tool.getAdvisorTools()

	// Build the set of returned tool names
	returned := make(map[string]bool)
	for _, tl := range advisorTools {
		returned[tl.Name()] = true
	}

	// All advisorAllowedTools should be present if the agent has them
	expected := []string{
		"read", "glob", "grep", "list", "lsp",
		"bash", "bash_output", "kill_shell",
		"webfetch", "websearch",
		"repo_clone", "repo_overview",
	}
	for _, name := range expected {
		if !returned[name] {
			// repo_clone, repo_overview, webfetch, websearch might not be
			// registered by NewAgent — that's OK, the test just checks that
			// the filtering works for what IS registered.
			if name != "repo_clone" && name != "repo_overview" && name != "webfetch" && name != "websearch" {
				t.Errorf("expected tool %q to be returned but it was missing", name)
			}
		}
	}

	// Should not include tools not in the allow-list
	if _, ok := returned["write"]; ok {
		t.Error("advisor tools should not include 'write'")
	}
	if _, ok := returned["delete"]; ok {
		t.Error("advisor tools should not include 'delete'")
	}
	if _, ok := returned["edit"]; ok {
		t.Error("advisor tools should not include 'edit'")
	}
	if _, ok := returned["apply_patch"]; ok {
		t.Error("advisor tools should not include 'apply_patch'")
	}
}

// Verify the tool implements the tool.Tool interface.
var _ tool.Tool = AdvisorTool{}

func TestCleanEnvForTerminal(t *testing.T) {
	// Set CLAUDECODE in the environment to check if it's stripped
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("SOME_OTHER_VAR", "value")

	env := cleanEnvForTerminal()

	// Verify CLAUDECODE is NOT present
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			t.Error("CLAUDECODE environment variable was not stripped")
		}
	}

	// Verify fake terminal variables are present
	var hasTermProgram, hasTerm, hasColorTerm, hasShell bool
	for _, e := range env {
		if strings.HasPrefix(e, "TERM_PROGRAM=") {
			hasTermProgram = true
		}
		if strings.HasPrefix(e, "TERM=") {
			hasTerm = true
		}
		if strings.HasPrefix(e, "COLORTERM=") {
			hasColorTerm = true
		}
		if strings.HasPrefix(e, "SHELL=") {
			hasShell = true
		}
	}

	if !hasTermProgram {
		t.Error("missing TERM_PROGRAM in clean env")
	}
	if !hasTerm {
		t.Error("missing TERM in clean env")
	}
	if !hasColorTerm {
		t.Error("missing COLORTERM in clean env")
	}
	if !hasShell {
		t.Error("missing SHELL in clean env")
	}
}
