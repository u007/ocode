package agent

import (
	"encoding/json"
	"testing"
)

// sandboxDecideTestPM builds a sandbox-mode PermissionManager with an isolated
// HOME (so no real ~/.claude/settings.json deny rules leak in) and a stubbed
// sandbox backend.
func sandboxDecideTestPM(t *testing.T) *PermissionManager {
	t.Helper()
	orig := sandboxSupported
	sandboxSupported = func() bool { return true }
	t.Cleanup(func() { sandboxSupported = orig })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	pm.SetMode(PermissionModeSandbox)
	return pm
}

func decideBash(t *testing.T, pm *PermissionManager, command string) PermissionDecision {
	t.Helper()
	b, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return pm.Decide("bash", b)
}

// TestDecideSandboxCompoundHarmfulGitRequiresAsk: a destructive git form hidden
// behind a benign first word (`cd X && git stash`) must not ride the sandbox
// auto-allow. IsHarmfulBashCommand/isHarmfulForceCommand only inspect a
// command whose first field is "git", and sandbox previously returned Allow
// before the compound parser ran, so the whole family was unguarded inside a
// compound line.
func TestDecideSandboxCompoundHarmfulGitRequiresAsk(t *testing.T) {
	pm := sandboxDecideTestPM(t)

	harmful := []string{
		"cd /tmp && git stash && echo done",
		"git status && git stash",
		"true; git stash",
		"cd /repo && git stash pop",
		"git fetch && git reset --hard HEAD",
		"cd /repo && git checkout main",
		"cd /repo && git clean -fdx",
		"cd /repo && git push --force origin main",
		"echo hi && git pull -f",
	}
	for _, cmd := range harmful {
		dec := decideBash(t, pm, cmd)
		if dec.Level != PermissionAsk {
			t.Errorf("sandbox compound harmful %q = %s (hard=%v), want Ask", cmd, dec.Level, dec.HardDeny)
		}
	}

	// Read-only stash inspection and ordinary operations still auto-allow in
	// sandbox — the per-fragment gate only fires on the harmful/banned forms.
	readOnly := []string{
		"cd /tmp && git stash list",
		"git status && git stash show",
		"cd /repo && git status",
		"cd /tmp && echo hi",
		"go test ./... && git diff HEAD~1",
	}
	for _, cmd := range readOnly {
		dec := decideBash(t, pm, cmd)
		if dec.Level != PermissionAllow {
			t.Errorf("sandbox compound read-only %q = %s, want Allow", cmd, dec.Level)
		}
	}
}

// TestDecideSandboxUserDenyPrefixWins: an explicit user ban
// (permissions.bash.prefixes["git stash"]="deny", as written by "/ban add")
// is hard policy and must be enforced in sandbox for both simple and compound
// commands. Previously sandbox's early auto-allow skipped the deny-prefix
// check entirely, so a banned compound was never inspected. The read-only
// inspection forms ("git stash list"/"show") are carved out of a "git stash"
// ban (see matchBashPrefixRule) and keep auto-allowing.
func TestDecideSandboxUserDenyPrefixWins(t *testing.T) {
	pm := sandboxDecideTestPM(t)
	pm.SetBashPrefixRule("git stash", PermissionDeny)

	banned := []string{
		"git stash",
		"cd /repo && git stash pop",
		"cd /repo && git stash && go test ./...",
		"git status && git stash pop",
	}
	for _, cmd := range banned {
		dec := decideBash(t, pm, cmd)
		if dec.Level != PermissionDeny || !dec.HardDeny {
			t.Errorf("sandbox banned %q = %s (hard=%v), want HardDeny", cmd, dec.Level, dec.HardDeny)
		}
	}

	// Non-banned commands still auto-allow.
	for _, cmd := range []string{"git status", "cd /repo && git diff", "git stash list", "cd /repo && git stash show -p"} {
		dec := decideBash(t, pm, cmd)
		if dec.Level != PermissionAllow {
			t.Errorf("sandbox non-banned %q = %s, want Allow", cmd, dec.Level)
		}
	}
}
