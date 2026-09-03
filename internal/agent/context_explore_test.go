package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTaskDefinition_HidesExploreWhenContextActive verifies that when the
// knowledge bundle is active (DocPromptEnabled + okf_version), the task tool
// hides "explore" from the schema because context subsumes it.
func TestTaskDefinition_HidesExploreWhenContextActive(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("---\nokf_version: \"0.1\"\n---\n# Index\n"), 0644); err != nil {
		t.Fatal(err)
	}

	a := NewAgent(nil, nil, nil, nil)
	a.SetDocPromptEnabled(true)
	a.SetWorkDir(td)

	tt := TaskTool{mainAgent: a}
	def := tt.Definition()
	// Extract enum for agent field
	params, ok := def["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("definition missing parameters")
	}
	agentField, ok := params["properties"].(map[string]interface{})["agent"].(map[string]interface{})
	if !ok {
		t.Fatalf("definition missing agent property")
	}
	enum, _ := agentField["enum"].([]string)
	if len(enum) == 0 {
		// fallback path may use []interface{}
		if raw, ok := agentField["enum"].([]interface{}); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok {
					enum = append(enum, s)
				}
			}
		}
	}
	for _, n := range enum {
		if n == "explore" {
			t.Fatalf("expected explore to be hidden when context is active, enum=%v", enum)
		}
	}
	foundContext := false
	for _, n := range enum {
		if n == "context" {
			foundContext = true
			break
		}
	}
	if !foundContext {
		t.Fatalf("expected context in enum when active, got %v", enum)
	}
	// Description should not mention explore as an option when hidden
	desc, _ := def["description"].(string)
	if desc == "" {
		t.Fatal("description missing")
	}
	// The description is built from visible agent descriptions; ensure explore's
	// description is not present
	if containsSubstrForTest(desc, "explore:") {
		t.Fatalf("description should not list explore when hidden: %q", desc)
	}
}

// TestTaskDefinition_ShowsExploreWhenContextInactive covers the three inactive
// states: DocPrompt disabled, enabled without bundle, and fallback registry/no agent.
func TestTaskDefinition_ShowsExploreWhenContextInactive(t *testing.T) {
	cases := []struct {
		name       string
		enabled    bool
		withBundle bool
	}{
		{"disabled_no_bundle", false, false},
		{"disabled_with_bundle", false, true},
		{"enabled_no_bundle", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			td := t.TempDir()
			if tc.withBundle {
				docsDir := filepath.Join(td, "docs")
				if err := os.MkdirAll(docsDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("---\nokf_version: \"0.1\"\n---\n# Index\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			a := NewAgent(nil, nil, nil, nil)
			a.SetDocPromptEnabled(tc.enabled)
			a.SetWorkDir(td)
			tt := TaskTool{mainAgent: a}
			def := tt.Definition()
			params := def["parameters"].(map[string]interface{})
			agentField := params["properties"].(map[string]interface{})["agent"].(map[string]interface{})
			// extract enum strings
			var enum []string
			switch v := agentField["enum"].(type) {
			case []string:
				enum = v
			case []interface{}:
				for _, e := range v {
					if s, ok := e.(string); ok {
						enum = append(enum, s)
					}
				}
			}
			found := false
			for _, n := range enum {
				if n == "explore" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected explore in enum when inactive (case %s), got %v", tc.name, enum)
			}
		})
	}
}

// TestTaskDefinition_NoMainAgentShowsExplore verifies fallback when TaskTool has no mainAgent.
func TestTaskDefinition_NoMainAgentShowsExplore(t *testing.T) {
	tt := TaskTool{mainAgent: nil}
	def := tt.Definition()
	params := def["parameters"].(map[string]interface{})
	agentField := params["properties"].(map[string]interface{})["agent"].(map[string]interface{})
	var enum []string
	switch v := agentField["enum"].(type) {
	case []string:
		enum = v
	case []interface{}:
		for _, e := range v {
			if s, ok := e.(string); ok {
				enum = append(enum, s)
			}
		}
	}
	found := false
	for _, n := range enum {
		if n == "explore" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected explore when no mainAgent, got %v", enum)
	}
}

// TestContextToolsIncludeExplore verifies context spec now includes explore toolkit.
func TestContextToolsIncludeExplore(t *testing.T) {
	spec := FindSubAgentSpec("context")
	if spec == nil {
		t.Fatal("context spec not found")
	}
	need := []string{"lsp", "bash", "webfetch", "websearch"}
	for _, n := range need {
		found := false
		for _, tool := range spec.Tools {
			if tool == n {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("context spec missing explore tool %q, got %v", n, spec.Tools)
		}
	}
	// context should still have core tools
	for _, n := range []string{"grep", "glob", "read", "list"} {
		found := false
		for _, tool := range spec.Tools {
			if tool == n {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("context spec missing core tool %q", n)
		}
	}
	// explore must still exist in DefaultSubAgents for inactive case
	if FindSubAgentSpec("explore") == nil {
		t.Fatal("explore spec should still exist")
	}
}

// TestIsContextActive mirrors the helper logic directly.
func TestIsContextActive(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("---\nokf_version: \"0.1\"\n---\n# Index\n"), 0644); err != nil {
		t.Fatal(err)
	}
	a := NewAgent(nil, nil, nil, nil)
	tt := TaskTool{mainAgent: a}

	// disabled -> false
	a.SetDocPromptEnabled(false)
	a.SetWorkDir(td)
	if tt.isContextActive() {
		t.Fatal("expected inactive when DocPrompt disabled")
	}
	// enabled with bundle -> true
	a.SetDocPromptEnabled(true)
	if !tt.isContextActive() {
		t.Fatal("expected active when enabled + bundle")
	}
	// enabled without bundle -> false
	a.SetWorkDir(t.TempDir())
	if tt.isContextActive() {
		t.Fatal("expected inactive when enabled but no bundle")
	}
	// nil mainAgent -> false
	tt2 := TaskTool{mainAgent: nil}
	if tt2.isContextActive() {
		t.Fatal("expected inactive when no mainAgent")
	}
}

func containsSubstrForTest(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
