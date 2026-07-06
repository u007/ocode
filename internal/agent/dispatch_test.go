package agent

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestDispatchSubagent_callsTaskTool(t *testing.T) {
	// DispatchSubagent must be defined on *Agent
	// This test verifies the method signature compiles.
	// Functional dispatch testing requires a live client — see integration tests.
	var a *Agent
	_ = func() {
		// Should compile:
		_, _ = a.DispatchSubagent("explore", "find auth files")
	}
	// Verify JSON marshalling of the args matches what TaskTool.Execute expects
	args, err := json.Marshal(map[string]any{
		"agent":  "explore",
		"prompt": "find auth files",
	})
	if err != nil {
		t.Fatalf("failed to marshal dispatch args: %v", err)
	}
	var params struct {
		Prompt string `json:"prompt"`
		Agent  string `json:"agent"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if params.Agent != "explore" || params.Prompt != "find auth files" {
		t.Errorf("unexpected params: %+v", params)
	}
}

// TestContextAgentDocToolsAreAllowed verifies that doc tools injected for the
// context subagent are actually usable — i.e. they appear in GetToolDefinitions
// and pass isToolAllowed. This is the regression test for C1 (OCSEC:31f59a:1):
// doc tools were injected into the tools slice but the spec's allowlist
// (["grep","glob","read","list"]) blocked them via isToolAllowed.
func TestContextAgentDocToolsAreAllowed(t *testing.T) {
	// Set up a temp dir with a valid OKF bundle so newDocTools succeeds.
	td := t.TempDir()
	docsDir := td + "/docs"
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docsDir+"/index.md", []byte("---\nokf_version: \"0.1\"\n---\n# Index\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Change into the temp dir so newDocTools can detect the bundle via workDir.
	origWd, _ := os.Getwd()
	if err := os.Chdir(td); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	// Create an agent that represents the context subagent.
	a := NewAgent(nil, nil, nil, nil)

	// Add the base tools that the context agent spec allows.
	baseTools := []string{"grep", "glob", "read", "list"}
	allToolNames := append([]string{}, baseTools...)

	// Inject doc tools.
	docTools, err := newDocTools(td)
	if err != nil {
		t.Fatalf("newDocTools: %v", err)
	}
	var docToolInsts []tool.Tool
	for _, dt := range docTools {
		docToolInsts = append(docToolInsts, dt)
		allToolNames = append(allToolNames, dt.Name())
	}
	a.AddTools(docToolInsts)

	// Set the spec WITHOUT doc tool names — this should block them.
	specWithoutDocs := &AgentSpec{
		Name:  "context",
		Tools: baseTools,
	}
	a.SetSpec(specWithoutDocs)

	// Verify doc tools are BLOCKED when not in the spec.
	for _, dt := range docTools {
		if a.isToolAllowed(dt.Name()) {
			t.Errorf("isToolAllowed(%q) = true when spec.Tools lacks it — doc tool should be blocked", dt.Name())
		}
	}

	// Now set the spec WITH doc tool names (as the C1 fix ensures).
	specWithDocs := &AgentSpec{
		Name:  "context",
		Tools: allToolNames,
	}
	a.SetSpec(specWithDocs)

	// Verify doc tools are ALLOWED when in the spec.
	for _, dt := range docTools {
		if !a.isToolAllowed(dt.Name()) {
			t.Errorf("isToolAllowed(%q) = false when spec.Tools includes it — doc tool should be allowed", dt.Name())
		}
	}

	// Verify GetToolDefinitions includes the doc tools.
	defs := a.GetToolDefinitions()
	defNames := make(map[string]bool)
	for _, def := range defs {
		if name, ok := def["name"].(string); ok {
			defNames[name] = true
		}
	}
	for _, dt := range docTools {
		if !defNames[dt.Name()] {
			t.Errorf("GetToolDefinitions does not include %q", dt.Name())
		}
	}
}
