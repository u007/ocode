package agent

import (
	"testing"
)

func TestDefaultAgents(t *testing.T) {
	if len(DefaultAgents) != 5 {
		t.Fatalf("expected 5 default agents, got %d", len(DefaultAgents))
	}

	names := map[string]bool{
		"build":  false,
		"plan":   false,
		"review": false,
		"debug":  false,
		"docs":   false,
	}

	for _, a := range DefaultAgents {
		names[a.Name] = true
		if a.Description == "" {
			t.Errorf("agent %q has no description", a.Name)
		}
		if !a.Mode.Valid() {
			t.Errorf("agent %q has invalid mode: %s", a.Name, a.Mode)
		}
	}

	for name, found := range names {
		if !found {
			t.Errorf("missing default agent: %s", name)
		}
	}
}

func TestFindAgentSpec(t *testing.T) {
	spec := FindAgentSpec("build")
	if spec == nil {
		t.Fatal("expected to find build agent")
	}
	if spec.Name != "build" {
		t.Errorf("expected build, got %s", spec.Name)
	}
	if spec.Mode != ModeBuild {
		t.Errorf("expected ModeBuild, got %s", spec.Mode)
	}

	spec = FindAgentSpec("nonexistent")
	if spec != nil {
		t.Error("expected nil for nonexistent agent")
	}
}

func TestNextAgentSpec(t *testing.T) {
	next := NextAgentSpec("build")
	if next == nil || next.Name != "plan" {
		t.Errorf("expected plan after build, got %v", next)
	}

	next = NextAgentSpec("docs")
	if next == nil || next.Name != "build" {
		t.Errorf("expected build after docs (wrap), got %v", next)
	}

	next = NextAgentSpec("nonexistent")
	if next == nil || next.Name != "build" {
		t.Errorf("expected build for nonexistent, got %v", next)
	}
}

func TestPermissionManager(t *testing.T) {
	pm := NewPermissionManager()

	if pm.Check("bash") != PermissionAllow {
		t.Error("expected allow for unknown tool")
	}

	pm.SetRule("bash", PermissionDeny)
	if pm.Check("bash") != PermissionDeny {
		t.Error("expected deny for bash")
	}

	pm.SetRule("mcp_*", PermissionAsk)
	if pm.Check("mcp_foo") != PermissionAsk {
		t.Error("expected ask for mcp_foo with wildcard")
	}
	if pm.Check("mcp_bar") != PermissionAsk {
		t.Error("expected ask for mcp_bar with wildcard")
	}
}

func TestPermissionManagerLoadFromConfig(t *testing.T) {
	pm := NewPermissionManager()
	cfg := map[string]interface{}{
		"bash":    "deny",
		"write":   "ask",
		"mcp_*":   "ask",
		"invalid": "foo",
	}
	pm.LoadFromConfig(cfg)

	if pm.Check("bash") != PermissionDeny {
		t.Error("expected deny for bash")
	}
	if pm.Check("write") != PermissionAsk {
		t.Error("expected ask for write")
	}
	if pm.Check("mcp_test") != PermissionAsk {
		t.Error("expected ask for mcp_test")
	}
	if pm.Check("read") != PermissionAllow {
		t.Error("expected allow for read")
	}
}

func TestPermissionRules(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetRule("bash", PermissionDeny)
	pm.SetRule("mcp_*", PermissionAsk)

	rules := pm.Rules()
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
	if rules["bash"] != PermissionDeny {
		t.Error("expected deny for bash in rules")
	}
	if rules["mcp_*"] != PermissionAsk {
		t.Error("expected ask for mcp_* in rules")
	}
}

func TestSubAgentSpecs(t *testing.T) {
	if len(DefaultSubAgents) != 3 {
		t.Fatalf("expected 3 default sub-agents, got %d", len(DefaultSubAgents))
	}

	names := map[string]bool{
		"general": false,
		"explore": false,
		"scout":   false,
	}

	for _, sa := range DefaultSubAgents {
		names[sa.Name] = true
		if sa.Description == "" {
			t.Errorf("sub-agent %q has no description", sa.Name)
		}
		if sa.SystemPrompt == "" {
			t.Errorf("sub-agent %q has no system prompt", sa.Name)
		}
	}

	for name, found := range names {
		if !found {
			t.Errorf("missing default sub-agent: %s", name)
		}
	}
}

func TestFindSubAgentSpec(t *testing.T) {
	spec := FindSubAgentSpec("explore")
	if spec == nil {
		t.Fatal("expected to find explore sub-agent")
	}
	if spec.Name != "explore" {
		t.Errorf("expected explore, got %s", spec.Name)
	}
	if len(spec.Tools) == 0 {
		t.Error("explore sub-agent should have tool restrictions")
	}

	spec = FindSubAgentSpec("nonexistent")
	if spec != nil {
		t.Error("expected nil for nonexistent sub-agent")
	}
}

func TestIsAllowedPlanWritePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"PLAN.md", true},
		{"plans/feature.md", true},
		{"docs/plans/architecture.md", true},
		{"my.plan.md", true},
		{"src/main.go", false},
		{"README.md", false},
	}

	for _, tt := range tests {
		result := IsAllowedPlanWritePath(tt.path)
		if result != tt.expected {
			t.Errorf("IsAllowedPlanWritePath(%q) = %v, want %v", tt.path, result, tt.expected)
		}
	}
}

func TestIsAllowedReviewWritePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"REVIEW.md", true},
		{"reviews/code-review.md", true},
		{"my.review.md", true},
		{"src/main.go", false},
		{"PLAN.md", false},
	}

	for _, tt := range tests {
		result := IsAllowedReviewWritePath(tt.path)
		if result != tt.expected {
			t.Errorf("IsAllowedReviewWritePath(%q) = %v, want %v", tt.path, result, tt.expected)
		}
	}
}

func TestAgentSpecToolFiltering(t *testing.T) {
	mock := &MockClient{
		Response: &Message{
			Role:    "assistant",
			Content: "Hello!",
		},
	}

	a := NewAgent(mock, nil, nil)
	a.SetSpec(&AgentSpec{
		Name:  "debug",
		Tools: []string{"read", "bash", "grep"},
		Mode:  ModeDebug,
	})

	tools := a.GetTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools (none added), got %d", len(tools))
	}

	defs := a.GetToolDefinitions()
	if len(defs) != 0 {
		t.Errorf("expected 0 tool definitions, got %d", len(defs))
	}
}

func TestAgentPermissions(t *testing.T) {
	mock := &MockClient{
		Response: &Message{
			Role:    "assistant",
			Content: "Hello!",
		},
	}

	a := NewAgent(mock, nil, nil)
	if a.Permissions() == nil {
		t.Fatal("expected permissions manager to be initialized")
	}

	a.Permissions().SetRule("bash", PermissionDeny)
	if a.Permissions().Check("bash") != PermissionDeny {
		t.Error("expected deny for bash")
	}
}

func TestAgentSpec(t *testing.T) {
	mock := &MockClient{
		Response: &Message{
			Role:    "assistant",
			Content: "Hello!",
		},
	}

	a := NewAgent(mock, nil, nil)
	if a.Spec() != nil {
		t.Error("expected nil spec initially")
	}

	spec := &AgentSpec{Name: "plan", Mode: ModePlan}
	a.SetSpec(spec)
	if a.Spec() != spec {
		t.Error("expected spec to be set")
	}
	if a.Mode() != ModePlan {
		t.Errorf("expected mode to be plan, got %s", a.Mode())
	}
}
