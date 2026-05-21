package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

func TestAgentRegistryBuiltins(t *testing.T) {
	reg := NewAgentRegistry()

	if got := reg.Get("build"); got == nil || got.Mode != AgentModePrimary {
		t.Fatalf("expected build primary agent, got %#v", got)
	}
	if got := reg.Get("plan"); got == nil || got.Mode != AgentModePrimary {
		t.Fatalf("expected plan primary agent, got %#v", got)
	}
	if got := reg.Get("general"); got == nil || got.Mode != AgentModeSubagent {
		t.Fatalf("expected general subagent, got %#v", got)
	}
	if got := reg.Get("explore"); got == nil || got.Mode != AgentModeSubagent {
		t.Fatalf("expected explore subagent, got %#v", got)
	}
	if got := reg.Get("scout"); got == nil || got.Mode != AgentModeSubagent {
		t.Fatalf("expected scout subagent, got %#v", got)
	}
	if got := reg.Get("nonexistent"); got != nil {
		t.Fatalf("expected nil for nonexistent agent, got %#v", got)
	}
}

func TestAgentRegistrySubAgentsDeterministic(t *testing.T) {
	reg := NewAgentRegistry()
	subs := reg.SubAgents()
	if len(subs) == 0 {
		t.Fatal("expected non-empty subagents")
	}

	names := make([]string, len(subs))
	for i, a := range subs {
		names[i] = a.Name
	}

	if !sort.StringsAreSorted(names) {
		t.Fatalf("expected sorted subagent names, got %v", names)
	}

	subs2 := reg.SubAgents()
	if len(subs) != len(subs2) {
		t.Fatalf("non-deterministic result, len %d vs %d", len(subs), len(subs2))
	}
	for i := range subs {
		if subs[i].Name != subs2[i].Name {
			t.Fatalf("non-deterministic result at %d: %s vs %s", i, subs[i].Name, subs2[i].Name)
		}
	}
}

func TestAgentRegistryPrimaryAgentsDeterministic(t *testing.T) {
	reg := NewAgentRegistry()
	prims := reg.PrimaryAgents()
	if len(prims) == 0 {
		t.Fatal("expected non-empty primary agents")
	}

	names := make([]string, len(prims))
	for i, a := range prims {
		names[i] = a.Name
	}

	if !sort.StringsAreSorted(names) {
		t.Fatalf("expected sorted primary agent names, got %v", names)
	}
}

func TestAgentRegistryGetAll(t *testing.T) {
	reg := NewAgentRegistry()
	all := reg.All()
	if len(all) < 5 {
		t.Fatalf("expected at least 5 agents, got %d", len(all))
	}
}

func TestLoadMarkdownAgentsGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agentsDir := filepath.Join(home, ".config", "opencode", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `---
description: Commits and pushes safely
mode: subagent
permission:
  read: allow
  bash: allow
  edit: deny
---
You are a git commit and push agent.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "git-commit-push.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewAgentRegistry()
	reg.LoadMarkdownAgents()

	ga := reg.Get("git-commit-push")
	if ga == nil {
		t.Fatal("expected git-commit-push to be loaded")
	}
	if ga.Name != "git-commit-push" {
		t.Fatalf("expected git-commit-push name, got %s", ga.Name)
	}
	if ga.Mode != AgentModeSubagent {
		t.Fatalf("expected subagent mode, got %s", ga.Mode)
	}
	if ga.Description != "Commits and pushes safely" {
		t.Fatalf("expected description, got %s", ga.Description)
	}
	if ga.SystemPrompt != "You are a git commit and push agent.\n" {
		t.Fatalf("expected prompt body, got %q", ga.SystemPrompt)
	}
	if ga.Source != filepath.Join(agentsDir, "git-commit-push.md") {
		t.Fatalf("expected source path, got %s", ga.Source)
	}
}

func TestLoadMarkdownAgentsProjectOverrideGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	globalDir := filepath.Join(home, ".config", "opencode", "agents")
	os.MkdirAll(globalDir, 0755)
	os.WriteFile(filepath.Join(globalDir, "helper.md"), []byte(`---
description: global helper
mode: subagent
---
global prompt
`), 0644)

	wd, _ := os.Getwd()
	projectDir := filepath.Join(wd, ".opencode", "agents")
	os.MkdirAll(projectDir, 0755)
	os.WriteFile(filepath.Join(projectDir, "helper.md"), []byte(`---
description: project helper
mode: subagent
---
project prompt
`), 0644)
	defer os.RemoveAll(filepath.Join(wd, ".opencode"))

	reg := NewAgentRegistry()
	reg.LoadMarkdownAgents()

	helper := reg.Get("helper")
	if helper == nil {
		t.Fatal("expected helper agent")
	}
	if helper.Description != "project helper" {
		t.Fatalf("expected project override, got %s", helper.Description)
	}
	if helper.SystemPrompt != "project prompt\n" {
		t.Fatalf("expected project prompt, got %q", helper.SystemPrompt)
	}
}

func TestLoadMarkdownAgentsCustomOverrideBuiltin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	globalDir := filepath.Join(home, ".config", "opencode", "agents")
	os.MkdirAll(globalDir, 0755)
	os.WriteFile(filepath.Join(globalDir, "general.md"), []byte(`---
description: custom general agent
mode: subagent
---
custom general prompt
`), 0644)

	reg := NewAgentRegistry()
	reg.LoadMarkdownAgents()

	general := reg.Get("general")
	if general == nil {
		t.Fatal("expected general agent")
	}
	if general.Description != "custom general agent" {
		t.Fatalf("expected custom override, got %s", general.Description)
	}
}

func TestAgentLoaderDiagnosticsMissingBody(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agentsDir := filepath.Join(home, ".config", "opencode", "agents")
	os.MkdirAll(agentsDir, 0755)

	os.WriteFile(filepath.Join(agentsDir, "nobody.md"), []byte(`---
description: missing body
mode: subagent
---
`), 0644)

	os.WriteFile(filepath.Join(agentsDir, "valid.md"), []byte(`---
description: has body
mode: subagent
---
has content
`), 0644)

	reg := NewAgentRegistry()
	diags := reg.LoadMarkdownAgents()

	foundMissing := false
	for _, d := range diags {
		if d.Level == "error" {
			foundMissing = true
			break
		}
	}
	if !foundMissing {
		t.Fatalf("expected missing-body error diagnostic, got %#v", diags)
	}
	if reg.Get("valid") == nil {
		t.Fatal("expected valid agent to load despite sibling error")
	}
}

func TestAgentLoaderDiagnosticsUnsupportedFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agentsDir := filepath.Join(home, ".config", "opencode", "agents")
	os.MkdirAll(agentsDir, 0755)
	os.WriteFile(filepath.Join(agentsDir, "with-model.md"), []byte(`---
description: has model field
mode: subagent
model: gpt-5
---
valid prompt
`), 0644)

	reg := NewAgentRegistry()
	diags := reg.LoadMarkdownAgents()

	foundWarn := false
	for _, d := range diags {
		if d.Level == "warning" {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Fatalf("expected unsupported field warning diagnostic, got %#v", diags)
	}
	if reg.Get("with-model") == nil {
		t.Fatal("expected agent to load despite unsupported field")
	}
}

func TestAgentLoaderDiagnosticsInvalidMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agentsDir := filepath.Join(home, ".config", "opencode", "agents")
	os.MkdirAll(agentsDir, 0755)
	os.WriteFile(filepath.Join(agentsDir, "badmode.md"), []byte(`---
description: bad mode
mode: invalid
---
prompt
`), 0644)

	reg := NewAgentRegistry()
	diags := reg.LoadMarkdownAgents()

	foundError := false
	for _, d := range diags {
		if d.Level == "error" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Fatalf("expected invalid-mode error diagnostic, got %#v", diags)
	}
}

func TestLoadMarkdownAgentsDefaultModeAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agentsDir := filepath.Join(home, ".config", "opencode", "agents")
	os.MkdirAll(agentsDir, 0755)
	os.WriteFile(filepath.Join(agentsDir, "helper.md"), []byte(`---
description: helper agent
---
helper prompt
`), 0644)

	reg := NewAgentRegistry()
	reg.LoadMarkdownAgents()

	helper := reg.Get("helper")
	if helper == nil {
		t.Fatal("expected helper agent")
	}
	if helper.Mode != AgentModeAll {
		t.Fatalf("expected default mode all, got %s", helper.Mode)
	}
}

func TestTaskToolSchemaListsRegistrySubAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agentsDir := filepath.Join(home, ".config", "opencode", "agents")
	os.MkdirAll(agentsDir, 0755)
	os.WriteFile(filepath.Join(agentsDir, "git-commit-push.md"), []byte(`---
description: Commits and pushes safely
mode: subagent
---
You are a git commit and push agent.
`), 0644)

	reg := NewAgentRegistry()
	reg.LoadMarkdownAgents()

	a := &Agent{tools: make(map[string]tool.Tool)}
	taskTool := TaskTool{mainAgent: a, registry: reg}
	def := taskTool.Definition()

	params := def["parameters"].(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	agentProp := props["agent"].(map[string]interface{})

	enum, ok := agentProp["enum"].([]string)
	if !ok {
		t.Fatal("expected enum field in agent property")
	}

	foundGCP := false
	for _, name := range enum {
		if name == "git-commit-push" {
			foundGCP = true
			break
		}
	}
	if !foundGCP {
		t.Fatalf("expected git-commit-push in task schema enum, got %v", enum)
	}

	if agentProp["description"].(string) == "" {
		t.Fatal("expected non-empty agent description")
	}
}

func TestTaskToolSchemaExcludesPrimaryAgents(t *testing.T) {
	reg := NewAgentRegistry()
	a := &Agent{tools: make(map[string]tool.Tool)}
	taskTool := TaskTool{mainAgent: a, registry: reg}
	def := taskTool.Definition()

	params := def["parameters"].(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	agentProp := props["agent"].(map[string]interface{})
	enum := agentProp["enum"].([]string)

	for _, name := range enum {
		if name == "build" || name == "plan" {
			t.Fatalf("primary agent %s must not appear in task schema enum", name)
		}
	}
}

func TestTaskToolExplicitUnknownAgentErrors(t *testing.T) {
	reg := NewAgentRegistry()
	a := &Agent{tools: make(map[string]tool.Tool)}
	taskTool := TaskTool{mainAgent: a, registry: reg}

	_, err := taskTool.Execute(json.RawMessage(`{"prompt": "test", "agent": "nonexistent"}`))
	if err == nil {
		t.Fatal("expected error for unknown explicit agent")
	}
}

func TestTaskToolReturnsChildResult(t *testing.T) {
	reg := NewAgentRegistry()
	mock := &MockClient{
		Response: &Message{Role: "assistant", Content: "task completed"},
	}
	a := &Agent{
		client:      mock,
		tools:       make(map[string]tool.Tool),
		config:      &config.Config{},
		permissions: NewPermissionManager(),
		mode:        ModeBuild,
	}
	taskTool := TaskTool{mainAgent: a, registry: reg}
	a.tools["task"] = taskTool

	a.config.Ocode = config.OcodeConfig{Permissions: config.PermissionConfig{Mode: "yolo"}}

	result, err := taskTool.Execute(json.RawMessage(`{"prompt": "test", "agent": "general"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result from task execution")
	}
	if !strings.Contains(result, "task completed") {
		t.Fatalf("expected result to contain 'task completed', got %q", result)
	}
}

func TestAgentPermissionMapping(t *testing.T) {
	tests := []struct {
		group     string
		level     string
		allowName string
		denyName  string
		isDeny    bool
	}{
		{"read", "deny", "read", "bash", false},
		{"edit", "deny", "write", "read", false},
		{"glob", "allow", "glob", "bash", false},
		{"grep", "allow", "grep", "bash", false},
		{"bash", "deny", "bash", "read", false},
		{"task", "deny", "task", "read", false},
		{"webfetch", "allow", "webfetch", "bash", false},
		{"skill", "allow", "skill", "bash", false},
		{"question", "allow", "question", "bash", false},
		{"lsp", "allow", "lsp", "bash", false},
	}

	for _, tt := range tests {
		t.Run(tt.group+"_"+tt.level, func(t *testing.T) {
			pm := buildPermissionManagerFromAgent(map[string]interface{}{
				tt.group: tt.level,
			})
			if pm.Check(tt.allowName) != PermissionLevel(tt.level) {
				t.Errorf("expected %s for %s, got %s", tt.level, tt.allowName, pm.Check(tt.allowName))
			}
			denied := pm.Check(tt.denyName)
			if tt.isDeny && denied != PermissionDeny {
				t.Errorf("expected deny for %s, got %s", tt.denyName, denied)
			}
		})
	}
}

func TestAgentPermissionUnknownGroupDenies(t *testing.T) {
	diags, pm := buildPermissionManagerFromAgentWithDiags(map[string]interface{}{
		"unknown_tool": "allow",
	})

	if len(diags) == 0 {
		t.Fatal("expected diagnostic for unknown group")
	}
	if diags[0].Message == "" {
		t.Fatal("expected message in diagnostic")
	}

	if pm.Check("bash") != PermissionAsk {
		t.Fatal("unknown group should not affect unrelated tool defaults")
	}
}

func TestChildAgentSession(t *testing.T) {
	childID := childSessionID("parent-123", "helper")

	if childID == "" {
		t.Fatal("expected non-empty child session ID")
	}
	if !contains(childID, "parent-123") && !contains(childID, "helper") {
		t.Fatalf("child ID %q should encode parent and agent", childID)
	}

	meta := childSessionMetadata("parent-123", "helper")
	if meta["parent_session_id"] != "parent-123" {
		t.Fatalf("expected parent_session_id in metadata")
	}
	if meta["agent_name"] != "helper" {
		t.Fatalf("expected agent_name in metadata")
	}
	if _, ok := meta["started_at"]; !ok {
		t.Fatal("expected started_at in metadata")
	}
	if _, ok := meta["status"]; !ok {
		t.Fatal("expected status in metadata")
	}
}

func TestHiddenAgentNotInDescriptionButInEnum(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agentsDir := filepath.Join(home, ".config", "opencode", "agents")
	os.MkdirAll(agentsDir, 0755)
	os.WriteFile(filepath.Join(agentsDir, "secret.md"), []byte(`---
description: hidden helper
mode: subagent
hidden: true
---
secret prompt
`), 0644)

	reg := NewAgentRegistry()
	reg.LoadMarkdownAgents()

	a := &Agent{tools: make(map[string]tool.Tool)}
	taskTool := TaskTool{mainAgent: a, registry: reg}
	def := taskTool.Definition()

	params := def["parameters"].(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	agentProp := props["agent"].(map[string]interface{})

	enum := agentProp["enum"].([]string)
	found := false
	for _, name := range enum {
		if name == "secret" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected hidden agent in enum")
	}

	desc := agentProp["description"].(string)
	if contains(desc, "secret") {
		t.Fatal("expected hidden agent excluded from visible description")
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
