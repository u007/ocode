package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

func newPermissionsRuleModel(t *testing.T) *model {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp+"/.config")

	cfg := config.Config{}
	return &model{
		config: &cfg,
		agent:  agent.NewAgent(retryTestClient{}, nil, &cfg, nil),
		input:  textarea.New(),
	}
}

// persistedBashPrefixes reads the durable rule set. runPermissionsCmd flushes
// permDirty through persistPermissions() as its last step, so the dirty map is
// empty by the time the command returns — the config file is what proves the
// write actually happened.
func persistedBashPrefixes(t *testing.T) map[string]string {
	t.Helper()
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	return cfg.Permissions.Bash.Prefixes
}

func lastAssistantText(m *model) string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == roleAssistant {
			return m.messages[i].text
		}
	}
	return ""
}

// TestPermissionsCmdRejectsUnstorableBashRule pins the fix for a silent
// no-op: SetBashPrefixRule discards a rule it refuses, so `/permissions
// bash:git allow` used to print "Set allow permission for …" and store nothing.
// The TUI must now report the rejection instead of claiming success.
func TestPermissionsCmdRejectsUnstorableBashRule(t *testing.T) {
	m := newPermissionsRuleModel(t)

	runPermissionsCmd(m, []string{"bash:git", "allow"})

	msg := lastAssistantText(m)
	if !strings.Contains(msg, "Cannot set bash rule") {
		t.Fatalf("message = %q, want a rejection", msg)
	}
	if strings.Contains(msg, "Set allow permission") {
		t.Fatalf("message = %q, must not claim success", msg)
	}
	if got := m.agent.Permissions().BashPrefixRules(); len(got) != 0 {
		t.Fatalf("a rejected rule was stored: %#v", got)
	}
	if got := persistedBashPrefixes(t); len(got) != 0 {
		t.Fatalf("a rejected rule was persisted: %#v", got)
	}
}

// TestPermissionsCmdAcceptsGranularBashRule: the guard must not narrow what a
// user may configure — a two-word rule is the normal case.
func TestPermissionsCmdAcceptsGranularBashRule(t *testing.T) {
	m := newPermissionsRuleModel(t)

	runPermissionsCmd(m, []string{"bash:git push", "deny"})

	if msg := lastAssistantText(m); !strings.Contains(msg, "Set deny permission") {
		t.Fatalf("message = %q, want the success line", msg)
	}
	if got := m.agent.Permissions().BashPrefixRules()["git push"]; got != agent.PermissionDeny {
		t.Fatalf("stored rule = %q, want deny", got)
	}
	if got := persistedBashPrefixes(t)["git push"]; got != "deny" {
		t.Fatalf("persisted rule = %#v, want git push=deny", persistedBashPrefixes(t))
	}
}

// TestPermissionsCmdRejectsEmptyBashPrefix: `/permissions bash: allow` has no
// prefix to store, which is the same silent-discard class.
func TestPermissionsCmdRejectsEmptyBashPrefix(t *testing.T) {
	m := newPermissionsRuleModel(t)

	runPermissionsCmd(m, []string{"bash:", "allow"})

	if msg := lastAssistantText(m); !strings.Contains(msg, "Cannot set bash rule") {
		t.Fatalf("message = %q, want a rejection", msg)
	}
	if len(m.agent.Permissions().BashPrefixRules()) != 0 {
		t.Fatal("an empty-prefix rule was stored")
	}
	if got := persistedBashPrefixes(t); len(got) != 0 {
		t.Fatalf("an empty-prefix rule was persisted: %#v", got)
	}
}

// TestSetPermissionRuleReportsUnstorableRule covers the permission-dialog path:
// the user's "always allow" still approves the call, but the dialog must not
// claim a durable rule that was discarded.
func TestSetPermissionRuleReportsUnstorableRule(t *testing.T) {
	m := newPermissionsRuleModel(t)
	req := agent.PermissionRequest{
		ToolName: "bash",
		Scope:    agent.PermissionScopeBashPrefix,
		Prefix:   "git",
		Rule:     "bash.prefix.git",
	}

	err := m.setPermissionRule(req, agent.PermissionAllow)
	if err == nil {
		t.Fatal("setPermissionRule returned nil for an unstorable rule")
	}
	if !strings.Contains(err.Error(), "cannot be stored") {
		t.Fatalf("error = %v, want an explanation", err)
	}
	if len(m.permDirty.bashPrefixes) != 0 {
		t.Fatalf("an unstorable rule was queued for persistence: %#v", m.permDirty.bashPrefixes)
	}
}

func TestSetPermissionRuleAppliesStorableRule(t *testing.T) {
	m := newPermissionsRuleModel(t)
	req := agent.PermissionRequest{
		ToolName: "bash",
		Scope:    agent.PermissionScopeBashPrefix,
		Prefix:   "sed",
		Rule:     "bash.prefix.sed",
	}

	if err := m.setPermissionRule(req, agent.PermissionAllow); err != nil {
		t.Fatalf("setPermissionRule: %v", err)
	}
	if got := m.agent.Permissions().BashPrefixRules()["sed"]; got != agent.PermissionAllow {
		t.Fatalf("stored rule = %q, want allow", got)
	}
	if got := m.permDirty.bashPrefixes["sed"]; got != "allow" {
		t.Fatalf("queued rule = %q, want allow", got)
	}
}
