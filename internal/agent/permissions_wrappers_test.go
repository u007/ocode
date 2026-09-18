package agent

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Wrapper forms must not hide a destructive git command from the harmful
// gate, the user ban matcher, or the Ask→auto-judge hand-off: path-qualified
// binaries, transparent launchers (env/command/nohup/timeout/xargs/…), and
// shell re-execution (bash -c / eval).
func TestIsHarmfulBashCommandSeesThroughWrappers(t *testing.T) {
	harmful := []string{
		"/usr/bin/git stash",
		"/opt/homebrew/bin/git stash pop",
		"command git stash",
		"env git stash",
		"env -i GIT_DIR=.git git stash",
		"nohup git stash",
		"exec git stash",
		"time git stash",
		"nice -n 5 git stash",
		"timeout 5 git stash",
		"timeout -s KILL 5s git reset --hard",
		"xargs git stash drop",
		"xargs -I{} git stash drop {}",
		"stdbuf -oL git stash",
		"sudo git stash",
		"sudo -u james git stash",
		"bash -c 'git stash'",
		"bash -c 'cd /repo && git stash && go test ./...'",
		"sh -c \"git stash pop\"",
		"zsh -lc 'git stash'",
		"/bin/sh -c 'git stash'",
		"bash -c \"sh -c 'git stash'\"",
		"eval git stash",
		"eval 'git stash pop'",
		"nohup timeout 5 env git stash",
	}
	for _, cmd := range harmful {
		if !IsHarmfulBashCommand(cmd) {
			t.Errorf("IsHarmfulBashCommand(%q) = false, want true", cmd)
		}
	}
	benign := []string{
		"/usr/bin/git stash list",
		"env git status",
		"timeout 5 go test ./...",
		"bash -c 'git stash list'",
		"bash -c 'go test ./... | tail -10'",
		"xargs -n1 echo",
		"command -v git",
		"eval echo hi",
	}
	for _, cmd := range benign {
		if IsHarmfulBashCommand(cmd) {
			t.Errorf("IsHarmfulBashCommand(%q) = true, want false", cmd)
		}
	}
}

func TestIsHarmfulRequestSeesThroughWrappers(t *testing.T) {
	for _, cmd := range []string{
		"cd /repo && bash -c 'git stash'",
		"git stash list | xargs git stash drop",
		"cd /repo && /usr/bin/git stash",
	} {
		if !IsHarmfulRequest(PermissionRequest{ToolName: "bash", Command: cmd}) {
			t.Errorf("IsHarmfulRequest(%q) = false, want true", cmd)
		}
	}
}

// In sandbox, every wrapper form of a destructive git command must Ask (no
// ban configured), and a "$var stash" command whose binary cannot be resolved
// statically must Ask rather than ride the OS-wrapped auto-allow.
func TestDecideSandboxWrappersDoNotBypassHarmfulGit(t *testing.T) {
	orig := sandboxSupported
	sandboxSupported = func() bool { return true }
	t.Cleanup(func() { sandboxSupported = orig })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	pm.SetMode(PermissionModeSandbox)

	for _, cmd := range []string{
		"bash -c 'git stash'",
		"sh -c \"git stash pop\"",
		"eval git stash",
		"command git stash",
		"env git stash",
		"/usr/bin/git stash",
		"timeout 5 git stash",
		"nohup git stash",
		"xargs git stash",
		"git stash list | xargs git stash drop",
		"g=git; $g stash",
		"$GIT stash",
	} {
		dec := pm.Decide("bash", json.RawMessage(fmt.Sprintf(`{"command":%q}`, cmd)))
		if dec.Level != PermissionAsk {
			t.Errorf("sandbox %q = %s, want Ask", cmd, dec.Level)
		}
	}
	for _, cmd := range []string{
		"env git status",
		"/usr/bin/git stash list",
		"timeout 5 go test ./...",
		"bash -c 'git stash list'",
	} {
		dec := pm.Decide("bash", json.RawMessage(fmt.Sprintf(`{"command":%q}`, cmd)))
		if dec.Level != PermissionAllow {
			t.Errorf("sandbox %q = %s, want Allow", cmd, dec.Level)
		}
	}
}

// A "git stash" ban must also catch the wrapped forms, in both modes.
func TestBannedPrefixSeesThroughWrappers(t *testing.T) {
	orig := sandboxSupported
	sandboxSupported = func() bool { return true }
	t.Cleanup(func() { sandboxSupported = orig })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	for _, mode := range []PermissionMode{PermissionModeNormal, PermissionModeSandbox} {
		pm := NewPermissionManager()
		pm.SetWorkDir(t.TempDir())
		pm.SetMode(mode)
		pm.SetBashPrefixRule("git stash", PermissionDeny)
		for _, cmd := range []string{
			"bash -c 'git stash'",
			"sh -c 'git stash pop'",
			"eval git stash",
			"env git stash",
			"command git stash",
			"/usr/bin/git stash",
			"timeout 5 git stash",
			"xargs git stash drop",
		} {
			dec := pm.Decide("bash", json.RawMessage(fmt.Sprintf(`{"command":%q}`, cmd)))
			if dec.Level != PermissionDeny || !dec.HardDeny {
				t.Errorf("[%s] banned %q = %s hard=%v, want hard Deny", mode, cmd, dec.Level, dec.HardDeny)
			}
		}
		for _, cmd := range []string{"/usr/bin/git stash list", "env git stash show -p"} {
			dec := pm.Decide("bash", json.RawMessage(fmt.Sprintf(`{"command":%q}`, cmd)))
			if dec.Level == PermissionDeny {
				t.Errorf("[%s] read-only %q = Deny, want not denied", mode, cmd)
			}
		}
	}
}
