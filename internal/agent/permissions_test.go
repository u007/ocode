package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPermissions_ToolAllow_BypassesSensitiveAndOutOfWorkdir locks in the
// current intended Decide() semantics: a tool-level "allow" rule (typically
// set via "always allow this rule/tool") short-circuits the sensitive-path,
// out-of-workdir, and delete prompts. This is a deliberate UX trade-off — if
// the policy is ever tightened so allow no longer bypasses these scopes,
// update this test to assert the new (asking) behaviour.
func TestPermissions_ToolAllow_BypassesSensitiveAndOutOfWorkdir(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args string
	}{
		{"write_sensitive", "write", `{"file_path":".env","content":"x"}`},
		{"write_out_of_scope", "write", `{"file_path":"/var/tmp/foreign/file","content":"x"}`},
		{"delete_in_workdir", "delete", `{"file_path":"sub/file.txt"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm := NewPermissionManager()
			pm.SetWorkDir("/Users/test/project")
			pm.SetRule(tc.tool, PermissionAllow)
			dec := pm.Decide(tc.tool, json.RawMessage(tc.args))
			if dec.Level != PermissionAllow {
				t.Fatalf("tool=%s args=%s: expected Allow under tool-allow rule, got %s", tc.tool, tc.args, dec.Level)
			}
		})
	}
}

// TestPermissions_NoToolAllow_StillAsksForSensitive verifies the gates remain
// effective when there is no explicit tool-level allow: sensitive paths,
// out-of-workdir paths, and delete still produce an Ask decision with the
// correct rule scope. This guards against accidentally turning the bypass
// into a default.
func TestPermissions_NoToolAllow_StillAsksForSensitive(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		ruleSet  PermissionLevel
		args     string
		wantRule string
	}{
		{
			name:     "sensitive_path_under_ask",
			tool:     "write",
			ruleSet:  PermissionAsk,
			args:     `{"file_path":".env","content":"x"}`,
			wantRule: "tool.write.sensitive_path",
		},
		{
			name:     "out_of_workdir_under_ask",
			tool:     "write",
			ruleSet:  PermissionAsk,
			args:     `{"file_path":"/var/tmp/foreign/file","content":"x"}`,
			wantRule: "tool.write.out_of_scope",
		},
		{
			name:     "delete_under_default_ask",
			tool:     "delete",
			ruleSet:  PermissionAsk,
			args:     `{"file_path":"sub/file.txt"}`,
			wantRule: "tool.delete.delete",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm := NewPermissionManager()
			pm.SetWorkDir("/Users/test/project")
			pm.SetRule(tc.tool, tc.ruleSet)
			dec := pm.Decide(tc.tool, json.RawMessage(tc.args))
			if dec.Level != PermissionAsk {
				t.Fatalf("expected Ask, got %s", dec.Level)
			}
			if dec.Request == nil {
				t.Fatalf("expected non-nil Request with rule %q", tc.wantRule)
			}
			if dec.Request.Rule != tc.wantRule {
				t.Fatalf("rule = %q, want %q", dec.Request.Rule, tc.wantRule)
			}
		})
	}
}

// TestPermissions_YOLO_Allows confirms YOLO mode short-circuits before any
// path-based gate. (Sanity check; not part of the regression set.)
func TestPermissions_YOLO_Allows(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir("/Users/test/project")
	pm.SetMode(PermissionModeYOLO)
	dec := pm.Decide("write", json.RawMessage(`{"file_path":".env","content":"x"}`))
	if dec.Level != PermissionAllow {
		t.Fatalf("YOLO should allow, got %s", dec.Level)
	}
}

func TestPermissions_BashAutoAllowInRoot_PersistsProjectScopedRule(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	filePath := filepath.Join(workDir, "internal", "tui", "model.go")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("package tui\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)

	cmd := fmt.Sprintf(`{"command":"sed -n '1,3p' %s"}`, filePath)
	dec := pm.Decide("bash", json.RawMessage(cmd))
	if dec.Level != PermissionAllow {
		t.Fatalf("expected first in-root sed command to auto-allow, got %s", dec.Level)
	}

	key := bashInRootKey("sed", resolvedWorkDir)
	if _, exists := pm.bashPrefixes[key]; exists {
		t.Fatalf("did not expect mutating sed mode to persist in-root key %q", key)
	}
}

func TestPermissions_BashPersistedRule_DoesNotBypassOutOfRoot(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)
	pm.bashPrefixes[bashInRootKey("sed", resolvedWorkDir)] = PermissionAllow

	dec := pm.Decide("bash", json.RawMessage(`{"command":"sed -n '1,3p' /etc/hosts"}`))
	if dec.Level != PermissionAsk {
		t.Fatalf("expected out-of-root command to ask, got %s", dec.Level)
	}
}

func TestPermissions_BashPrefixAllowStillAllowsOutsideRoot(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)
	pm.SetBashPrefixRule("sed", PermissionAllow)

	dec := pm.Decide("bash", json.RawMessage(`{"command":"sed -n '1,3p' /etc/hosts"}`))
	if dec.Level != PermissionAllow {
		t.Fatalf("expected explicit sed prefix allow to allow out-of-root command, got %s", dec.Level)
	}
}

func TestPermissions_ExportConfigSkipsInternalInRootRules(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)
	pm.bashPrefixes[bashInRootKey("cat", resolvedWorkDir)] = PermissionAllow

	exported := pm.ExportConfig()
	for k := range exported.Bash.Prefixes {
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			t.Fatalf("unexpected internal in-root rule exported: %q", k)
		}
	}
}

func TestPermissions_BashAutoAllowInRoot_Awk(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	filePath := filepath.Join(workDir, "data.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)

	cmd := fmt.Sprintf(`{"command":"awk '{print $1}' %s"}`, filePath)
	dec := pm.Decide("bash", json.RawMessage(cmd))
	if dec.Level != PermissionAllow {
		t.Fatalf("expected in-root awk command to auto-allow, got %s", dec.Level)
	}

	if pm.bashPrefixes[bashInRootKey("awk", resolvedWorkDir)] != PermissionAllow {
		t.Fatalf("expected persisted project-scoped awk rule")
	}
}

// TestPermissions_PathScopedDoesNotBypass locks in the fix for the bug where
// a path-scoped prefix (e.g. grep) would auto-allow against an out-of-root
// path because a separate "safe command" allowlist matched the prefix and
// ignored path scoping. Out-of-root reads must always ask.
func TestPermissions_PathScopedDoesNotBypass(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)

	cases := []string{
		`{"command":"grep secret /etc/passwd"}`,
		`{"command":"cat /etc/hosts"}`,
		`{"command":"head /var/log/system.log"}`,
		`{"command":"wc -l /etc/passwd"}`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			dec := pm.Decide("bash", json.RawMessage(cmd))
			if dec.Level != PermissionAsk {
				t.Fatalf("expected ask for %s, got %s", cmd, dec.Level)
			}
		})
	}
}

// TestPermissions_PathScopedAllowsArglessAndStdin verifies that commands
// with no path arguments (which read stdin or the cwd) still auto-allow.
// Previously these returned false from canAutoAllowInRoot because of a
// len(paths)==0 early-return.
func TestPermissions_PathScopedAllowsArglessAndStdin(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)

	// `ls` with no args lists cwd; `find` with no args walks cwd; `grep`
	// with a bare pattern reads stdin. All inherently within workdir.
	cases := []string{
		`{"command":"ls"}`,
		`{"command":"find"}`,
		`{"command":"grep foo"}`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			dec := pm.Decide("bash", json.RawMessage(cmd))
			if dec.Level != PermissionAllow {
				t.Fatalf("expected allow for %s, got %s", cmd, dec.Level)
			}
		})
	}
}

// TestPermissions_FindUnsafeFlagsAsk verifies that find with -exec or
// -delete is not auto-allowed even when the search path is in-root.
func TestPermissions_FindUnsafeFlagsAsk(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)

	cases := []string{
		`{"command":"find . -name foo -exec rm {} ;"}`,
		`{"command":"find . -delete"}`,
		`{"command":"find . -fls out.txt"}`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			dec := pm.Decide("bash", json.RawMessage(cmd))
			if dec.Level != PermissionAsk {
				t.Fatalf("expected ask for %s, got %s", cmd, dec.Level)
			}
		})
	}
}

// TestPermissions_GitMutatingSubcommandsAsk verifies that destructive git
// subcommands are no longer auto-allowed (previously the generic `git `
// prefix in isSafeBashCommand allowed them all).
func TestPermissions_GitMutatingSubcommandsAsk(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir("/Users/test/project")

	cases := []string{
		`{"command":"git push --force"}`,
		`{"command":"git reset --hard HEAD"}`,
		`{"command":"git clean -fdx"}`,
		`{"command":"git checkout -- ."}`,
		`{"command":"git branch -D main"}`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			dec := pm.Decide("bash", json.RawMessage(cmd))
			if dec.Level != PermissionAsk {
				t.Fatalf("expected ask for %s, got %s", cmd, dec.Level)
			}
		})
	}
}

// TestPermissions_GitReadOnlySubcommandsAllow verifies the safe git
// subcommands still auto-allow.
func TestPermissions_GitReadOnlySubcommandsAllow(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir("/Users/test/project")

	cases := []string{
		`{"command":"git status"}`,
		`{"command":"git diff HEAD~1"}`,
		`{"command":"git log --oneline -10"}`,
		`{"command":"git show HEAD"}`,
		`{"command":"git rev-parse HEAD"}`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			dec := pm.Decide("bash", json.RawMessage(cmd))
			if dec.Level != PermissionAllow {
				t.Fatalf("expected allow for %s, got %s", cmd, dec.Level)
			}
		})
	}
}

// TestPermissions_AlwaysAllowCommands verifies argless commands auto-allow.
func TestPermissions_AlwaysAllowCommands(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir("/Users/test/project")

	cases := []string{
		`{"command":"pwd"}`,
		`{"command":"whoami"}`,
		`{"command":"date"}`,
		`{"command":"echo hello world"}`,
		`{"command":"which go"}`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			dec := pm.Decide("bash", json.RawMessage(cmd))
			if dec.Level != PermissionAllow {
				t.Fatalf("expected allow for %s, got %s", cmd, dec.Level)
			}
		})
	}
}

// TestPermissions_ThreeWordSubcommand verifies docker compose subcommands
// match the three-word allowlist.
func TestPermissions_ThreeWordSubcommand(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir("/Users/test/project")

	if dec := pm.Decide("bash", json.RawMessage(`{"command":"docker compose ps"}`)); dec.Level != PermissionAllow {
		t.Fatalf("docker compose ps expected allow, got %s", dec.Level)
	}
	if dec := pm.Decide("bash", json.RawMessage(`{"command":"docker compose down"}`)); dec.Level != PermissionAsk {
		t.Fatalf("docker compose down expected ask, got %s", dec.Level)
	}
}

// TestPermissions_NewReadOnlyTools verifies the newly added read-only tools
// (file, stat, jq, diff, md5sum, xxd, tree, etc.) auto-allow on in-root paths.
func TestPermissions_NewReadOnlyTools(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	filePath := filepath.Join(workDir, "data.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)

	cases := []string{
		fmt.Sprintf(`{"command":"file %s"}`, filePath),
		fmt.Sprintf(`{"command":"stat %s"}`, filePath),
		fmt.Sprintf(`{"command":"md5sum %s"}`, filePath),
		fmt.Sprintf(`{"command":"sha256sum %s"}`, filePath),
		fmt.Sprintf(`{"command":"xxd %s"}`, filePath),
		fmt.Sprintf(`{"command":"jq . %s"}`, filePath),
		fmt.Sprintf(`{"command":"du -sh %s"}`, filePath),
		`{"command":"tree"}`,
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			dec := pm.Decide("bash", json.RawMessage(cmd))
			if dec.Level != PermissionAllow {
				t.Fatalf("expected allow for %s, got %s", cmd, dec.Level)
			}
		})
	}
}

func TestPermissions_BashAutoAllow_NeverAutoModeAsks(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	filePath := filepath.Join(workDir, "data.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)
	pm.bashPrefixModes["awk"] = bashPrefixModeNever

	cmd := fmt.Sprintf(`{"command":"awk '{print $1}' %s"}`, filePath)
	dec := pm.Decide("bash", json.RawMessage(cmd))
	if dec.Level != PermissionAsk {
		t.Fatalf("expected awk never_auto mode to ask, got %s", dec.Level)
	}
}

func TestPermissions_ExportConfigPreservesAutoPermissionEnabled(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetAutoPermissionEnabled(true)

	exported := pm.ExportConfig()
	if exported.Auto == nil {
		t.Fatal("expected exported auto block")
	}
	if !exported.Auto.Enabled {
		t.Fatal("expected exported auto.enabled to be true")
	}

	roundTrip := NewPermissionManager()
	roundTrip.LoadFromOcode(exported)
	if !roundTrip.AutoPermissionEnabled() {
		t.Fatal("expected LoadFromOcode to restore auto-permission enabled state")
	}
}

func TestPermissions_AdvancedBashFeatures(t *testing.T) {
	workDir := t.TempDir()
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	pm := NewPermissionManager()
	pm.SetWorkDir(resolvedWorkDir)

	// Set temporary HOME to a temp directory outside of workdir
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	t.Run("tokenizer_and_compound_splitting", func(t *testing.T) {
		cmds, err := parseShellCommandLine(`cd ` + resolvedWorkDir + ` && grep -rn "pattern" .`)
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		if len(cmds) != 2 {
			t.Fatalf("expected 2 commands, got %d", len(cmds))
		}
		if cmds[0].cmdWords[0] != "cd" {
			t.Errorf("expected first command cd, got %q", cmds[0].cmdWords[0])
		}
		if cmds[1].cmdWords[0] != "grep" {
			t.Errorf("expected second command grep, got %q", cmds[1].cmdWords[0])
		}
	})

	t.Run("env_variables_stripped_and_checked", func(t *testing.T) {
		// Valid in-root env variable path
		cmd := fmt.Sprintf(`{"command":"CONFIG_FILE=%s/config.json go test"}`, resolvedWorkDir)
		dec := pm.Decide("bash", json.RawMessage(cmd))
		if dec.Level != PermissionAllow {
			t.Fatalf("expected allow for in-root env var path, got %s", dec.Level)
		}

		// Invalid out-of-root env variable path
		cmd2 := `{"command":"CONFIG_FILE=/etc/passwd go test"}`
		dec2 := pm.Decide("bash", json.RawMessage(cmd2))
		if dec2.Level != PermissionAsk {
			t.Fatalf("expected ask for out-of-root env var path, got %s", dec2.Level)
		}
	})

	t.Run("redirections_checked", func(t *testing.T) {
		// Safe in-root redirection
		cmd := fmt.Sprintf(`{"command":"echo hello > %s/out.txt"}`, resolvedWorkDir)
		dec := pm.Decide("bash", json.RawMessage(cmd))
		if dec.Level != PermissionAllow {
			t.Fatalf("expected allow for in-root redirection, got %s", dec.Level)
		}

		// Unsafe out-of-root redirection
		cmd2 := `{"command":"echo hello > /tmp/out.txt"}`
		dec2 := pm.Decide("bash", json.RawMessage(cmd2))
		if dec2.Level != PermissionAsk {
			t.Fatalf("expected ask for out-of-root redirection, got %s", dec2.Level)
		}

		// Safe /dev/null bypass
		cmd3 := `{"command":"echo hello > /dev/null"}`
		dec3 := pm.Decide("bash", json.RawMessage(cmd3))
		if dec3.Level != PermissionAllow {
			t.Fatalf("expected allow for /dev/null redirection, got %s", dec3.Level)
		}
	})

	t.Run("cd_checks", func(t *testing.T) {
		// cd to in-root path
		cmd := fmt.Sprintf(`{"command":"cd %s"}`, resolvedWorkDir)
		dec := pm.Decide("bash", json.RawMessage(cmd))
		if dec.Level != PermissionAllow {
			t.Fatalf("expected allow for cd to in-root, got %s", dec.Level)
		}

		// cd to out-of-root path
		cmd2 := `{"command":"cd /tmp"}`
		dec2 := pm.Decide("bash", json.RawMessage(cmd2))
		if dec2.Level != PermissionAsk {
			t.Fatalf("expected ask for cd to out-of-root, got %s", dec2.Level)
		}

		// cd with no args (defaults to HOME, which is tempHome outside workdir)
		cmd3 := `{"command":"cd"}`
		dec3 := pm.Decide("bash", json.RawMessage(cmd3))
		if dec3.Level != PermissionAsk {
			t.Fatalf("expected ask for cd with no args (out-of-root HOME), got %s", dec3.Level)
		}
	})

	t.Run("tilde_expansion", func(t *testing.T) {
		// Path with ~ in HOME (outside workdir)
		cmd := `{"command":"ls ~/Downloads"}`
		dec := pm.Decide("bash", json.RawMessage(cmd))
		if dec.Level != PermissionAsk {
			t.Fatalf("expected ask for ~ path outside workdir, got %s", dec.Level)
		}
	})

	t.Run("compound_command_decide", func(t *testing.T) {
		// Compound where all are safe
		cmd := fmt.Sprintf(`{"command":"cd %s && grep foo ."}`, resolvedWorkDir)
		dec := pm.Decide("bash", json.RawMessage(cmd))
		if dec.Level != PermissionAllow {
			t.Fatalf("expected allow for compound command containing safe cd and grep, got %s", dec.Level)
		}

		// Compound containing one unsafe command
		cmd2 := fmt.Sprintf(`{"command":"cd %s && cat /etc/passwd"}`, resolvedWorkDir)
		dec2 := pm.Decide("bash", json.RawMessage(cmd2))
		if dec2.Level != PermissionAsk {
			t.Fatalf("expected ask for compound command with unsafe subcommand, got %s", dec2.Level)
		}
	})

	t.Run("auto_permission_default_off", func(t *testing.T) {
		pm := NewPermissionManager()
		if pm.AutoPermissionEnabled() {
			t.Fatal("expected new PermissionManager to default to auto-permission off")
		}
	})

	t.Run("auto_permission_toggle", func(t *testing.T) {
		pm := NewPermissionManager()
		pm.SetAutoPermissionEnabled(true)
		if !pm.AutoPermissionEnabled() {
			t.Fatal("expected auto-permission enabled after SetAutoPermissionEnabled(true)")
		}
		pm.SetAutoPermissionEnabled(false)
		if pm.AutoPermissionEnabled() {
			t.Fatal("expected auto-permission disabled after SetAutoPermissionEnabled(false)")
		}
	})

	t.Run("auto_permission_setter_nil_safe", func(t *testing.T) {
		// nil receiver must not panic; getter on nil returns false.
		var pm *PermissionManager
		pm.SetAutoPermissionEnabled(true)
		if pm.AutoPermissionEnabled() {
			t.Fatal("expected nil PermissionManager to report auto-permission off")
		}
	})

	t.Run("auto_permission_does_not_bypass_hard_blocks", func(t *testing.T) {
		// Hard-blocks must remain deterministic. Toggling auto-permission
		// on must not turn a hard-blocked command into an allow.
		pm := NewPermissionManager()
		pm.SetAutoPermissionEnabled(true)
		dec := pm.Decide("bash", json.RawMessage(`{"command":"rm -rf /"}`))
		if dec.Level != PermissionDeny {
			t.Fatalf("expected hard-block to deny regardless of auto-permission, got %s", dec.Level)
		}
	})
}
