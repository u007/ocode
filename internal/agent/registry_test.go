package agent

import (
	"encoding/json"
	"strings"
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
	if next == nil {
		t.Fatal("expected an agent after docs")
	}
	if len(PrimaryAgentSpecs()) == len(DefaultAgents) && next.Name != "build" {
		t.Errorf("expected build after docs (wrap), got %v", next)
	}

	next = NextAgentSpec("nonexistent")
	if next == nil || next.Name != "build" {
		t.Errorf("expected build for nonexistent, got %v", next)
	}
}

func TestPermissionManager(t *testing.T) {
	pm := NewPermissionManager()

	if pm.Check("unknown_tool") != PermissionAsk {
		t.Error("expected ask for unknown tool")
	}
	if pm.Check("read") != PermissionAllow {
		t.Error("expected allow for read")
	}
	if pm.Check("bash") != PermissionAsk {
		t.Error("expected ask for bash")
	}
	if pm.Check("write") != PermissionAllow {
		t.Error("expected allow for write")
	}
	if pm.Check("apply_patch") != PermissionAllow {
		t.Error("expected allow for apply_patch")
	}
	if pm.Check("delete") != PermissionAsk {
		t.Error("expected ask for delete")
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
	if len(rules) < 2 {
		t.Errorf("expected at least 2 rules, got %d", len(rules))
	}
	if rules["bash"] != PermissionDeny {
		t.Error("expected deny for bash in rules")
	}
	if rules["mcp_*"] != PermissionAsk {
		t.Error("expected ask for mcp_* in rules")
	}
}

func TestPermissionManagerBashPrefixRules(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetBashPrefixRule("git", PermissionAllow)

	decision := pm.Decide("bash", json.RawMessage(`{"command":"git status"}`))
	if decision.Level != PermissionAllow {
		t.Fatalf("expected git prefix allow, got %s", decision.Level)
	}

	decision = pm.Decide("bash", json.RawMessage(`{"command":"curl https://example.com"}`))
	if decision.Level != PermissionAsk || decision.Request == nil || decision.Request.Prefix != "curl" {
		t.Fatalf("expected curl prefix ask request, got %+v", decision)
	}
}

// TestPermissionPrefixBeatseSafeCommand verifies that an explicit prefix deny rule
// wins over the safe-command allowlist (prefix rules take priority).
func TestPermissionPrefixBeatsSafeCommand(t *testing.T) {
	// "git status" is in bashSubcommandAllow; a deny prefix rule must still win.
	pm := NewPermissionManager()
	pm.SetBashPrefixRule("git", PermissionDeny)
	decision := pm.Decide("bash", json.RawMessage(`{"command":"git status"}`))
	if decision.Level != PermissionDeny {
		t.Fatalf("expected prefix deny to win over safe command, got %s", decision.Level)
	}

	// No prefix rule set: safe command is allowed.
	pm2 := NewPermissionManager()
	decision2 := pm2.Decide("bash", json.RawMessage(`{"command":"git status"}`))
	if decision2.Level != PermissionAllow {
		t.Fatalf("expected safe command allow when no prefix rule, got %s", decision2.Level)
	}
}

// TestGitAlwaysAllowPersistsAtSubcommandGranularity reproduces the permission
// dialog "reappears forever" bug: choosing "always allow" for `git push` used to
// offer a blanket "git" prefix rule that SetBashPrefixRule deliberately refuses
// to persist, so Decide kept returning Ask. The request must now be offered at
// two-word granularity ("git push") so the rule actually persists and the
// command auto-allows on the next Decide — without auto-allowing force-push or
// other harmful git subcommands.
func TestGitAlwaysAllowPersistsAtSubcommandGranularity(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetMode(PermissionModeNormal)

	args := json.RawMessage(`{"command":"git push origin main"}`)
	dec := pm.Decide("bash", args)
	if dec.Level != PermissionAsk || dec.Request == nil {
		t.Fatalf("expected initial ask for git push, got %+v", dec)
	}
	if dec.Request.Scope != PermissionScopeBashPrefix || dec.Request.Prefix != "git push" {
		t.Fatalf("expected two-word bash-prefix request, got scope=%s prefix=%q", dec.Request.Scope, dec.Request.Prefix)
	}

	// Emulate "always allow this rule" persistence.
	pm.SetBashPrefixRule(dec.Request.Prefix, PermissionAllow)

	if got := pm.Decide("bash", args).Level; got != PermissionAllow {
		t.Fatalf("git push must auto-allow after persist; got %s (dialog would loop)", got)
	}
	// Harmful variants must still require explicit approval.
	if got := pm.Decide("bash", json.RawMessage(`{"command":"git push --force origin main"}`)).Level; got != PermissionAsk {
		t.Fatalf("git push --force must still ask; got %s", got)
	}
	if got := pm.Decide("bash", json.RawMessage(`{"command":"git revert HEAD"}`)).Level; got != PermissionAsk {
		t.Fatalf("git revert must still ask; got %s", got)
	}
	// A blanket "git" deny must still govern every subcommand.
	pm2 := NewPermissionManager()
	pm2.SetBashPrefixRule("git", PermissionDeny)
	if got := pm2.Decide("bash", args).Level; got != PermissionDeny {
		t.Fatalf("broad git deny must win over git push; got %s", got)
	}
}

// TestRedirectFdDupParsing guards the shell-parser bug where "2>&1" was
// tokenized as a "2>" redirect plus a "&" operator plus a "1" command word —
// surfacing in the permission dialog as a bogus `always allow bash prefix "1"`.
// fd-duplication and &> redirects must not produce stray "1"/"2" command words,
// while a real background "&" must still split commands.
func TestRedirectFdDupParsing(t *testing.T) {
	noFdWord := func(t *testing.T, command string, wantCmds int) {
		t.Helper()
		cmds, err := parseShellCommandLine(command)
		if err != nil {
			t.Fatalf("%q: parse error %v", command, err)
		}
		if wantCmds >= 0 && len(cmds) != wantCmds {
			t.Fatalf("%q: got %d sub-commands, want %d (%+v)", command, len(cmds), wantCmds, cmds)
		}
		for _, c := range cmds {
			if len(c.cmdWords) > 0 && (c.cmdWords[0] == "1" || c.cmdWords[0] == "2") {
				t.Fatalf("%q: bogus fd command word %q", command, c.cmdWords[0])
			}
		}
	}

	// The reported command: cd && bun ... 2>&1 | tail -20 → cd, bun, tail (no "1").
	noFdWord(t, "cd /tmp/x && bun run lint:fix:changed 2>&1 | tail -20", 3)
	noFdWord(t, "foo 2>&1", 1)
	noFdWord(t, "foo 1>&2", 1)
	noFdWord(t, "foo >&2", 1)
	noFdWord(t, "foo 2>&-", 1)

	// &> / &>> redirect both streams to a file — the file is a checkable target.
	for _, command := range []string{"foo &> out.txt", "foo &>> out.txt", "foo > out.txt 2>&1"} {
		cmds, err := parseShellCommandLine(command)
		if err != nil {
			t.Fatalf("%q: parse error %v", command, err)
		}
		if len(cmds) != 1 || len(cmds[0].redirections) != 1 || cmds[0].redirections[0] != "out.txt" {
			t.Fatalf("%q: expected single redirection to out.txt, got %+v", command, cmds)
		}
	}

	// A genuine background "&" must still split into two commands.
	noFdWord(t, "echo hi & echo bye", 2)
}

func TestPermissionManagerYoloAllowsBash(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetMode(PermissionModeYOLO)

	decision := pm.Decide("bash", json.RawMessage(`{"command":"curl https://example.com"}`))
	if decision.Level != PermissionAllow {
		t.Fatalf("expected yolo allow, got %s", decision.Level)
	}
}

func TestSubAgentSpecs(t *testing.T) {
	if len(DefaultSubAgents) != 4 {
		t.Fatalf("expected 4 default sub-agents, got %d", len(DefaultSubAgents))
	}

	names := map[string]bool{
		"general": false,
		"explore": false,
		"scout":   false,
		"context": false,
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

	a := NewAgent(mock, nil, nil, nil)
	a.SetSpec(&AgentSpec{
		Name:  "debug",
		Tools: []string{"read", "bash", "grep"},
		Mode:  ModeDebug,
	})

	tools := a.GetTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 allowed wired tool, got %d", len(tools))
	}
	if tools[0].Name() != "bash" {
		t.Fatalf("expected bash tool, got %q", tools[0].Name())
	}

	defs := a.GetToolDefinitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool definition, got %d", len(defs))
	}
	if defs[0]["name"] != "bash" {
		t.Fatalf("expected bash definition, got %#v", defs[0]["name"])
	}
}

func TestAgentPermissions(t *testing.T) {
	mock := &MockClient{
		Response: &Message{
			Role:    "assistant",
			Content: "Hello!",
		},
	}

	a := NewAgent(mock, nil, nil, nil)
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

	a := NewAgent(mock, nil, nil, nil)
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

func TestModePromptsIncludeExpectationValidation(t *testing.T) {
	tests := []struct {
		mode Mode
		want []string
	}{
		{ModeBuild, []string{"User Expectation Checklist", "validate every checklist item", "iterate with fixes", "compact context packet"}},
		{ModePlan, []string{"User Expectation Checklist", "Validation Plan", "checklist items they verify"}},
		{ModeReview, []string{"User Expectation Checklist", "missed requirements", "missing validation"}},
		{ModeDebug, []string{"observed failure", "expected behavior", "smallest root cause", "Validate the diagnosis"}},
		{ModeDocs, []string{"validate the documented behavior", "confirm the docs match"}},
	}

	for _, tt := range tests {
		prompt := tt.mode.SystemPrompt()
		for _, want := range tt.want {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s prompt missing %q: %s", tt.mode, want, prompt)
			}
		}
	}
}

func TestSubAgentPromptsIncludeExpectationCoverage(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"general", []string{"User Expectation Checklist", "validate each checklist item", "remaining gaps"}},
		{"explore", []string{"user expectations", "remaining unknowns"}},
		{"scout", []string{"source URLs", "user expectations", "remaining unknowns"}},
	}

	for _, tt := range tests {
		spec := FindSubAgentSpec(tt.name)
		if spec == nil {
			t.Fatalf("missing sub-agent %q", tt.name)
		}
		for _, want := range tt.want {
			if !strings.Contains(spec.SystemPrompt, want) {
				t.Fatalf("%s sub-agent prompt missing %q: %s", tt.name, want, spec.SystemPrompt)
			}
		}
	}
}
