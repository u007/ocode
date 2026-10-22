package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBashPrefixCandidates(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "three-word npm subcommand",
			command: "npm run build",
			want:    []string{"npm run build", "npm run", "npm"},
		},
		{
			name:    "flags do not become part of a rule",
			command: "ls -la",
			want:    []string{"ls"},
		},
		{
			name:    "git stays at the subcommand",
			command: "git status -s",
			want:    []string{"git status"},
		},
		{
			name:    "git never offers a bare git rule",
			command: "git push --force origin main",
			want:    []string{"git push"},
		},
		{
			name:    "path argument is not a reusable prefix",
			command: "go test ./...",
			want:    []string{"go test", "go"},
		},
		{
			name:    "file argument is not a reusable prefix",
			command: "cat README.md",
			want:    []string{"cat"},
		},
		{
			name:    "wrapper is not a literal prefix",
			command: "env npm run build",
			want:    nil,
		},
		{
			name:    "sudo is not a literal prefix",
			command: "sudo npm run build",
			want:    nil,
		},
		{
			name:    "compound line yields nothing",
			command: "cd repo && npm run build",
			want:    nil,
		},
		{
			name:    "shell control keyword yields nothing",
			command: "while true; do ls; done",
			want:    nil,
		},
		{
			name:    "single binary",
			command: "pwd",
			want:    []string{"pwd"},
		},
		{
			name:    "dangerous rm keeps a single-word rule",
			command: "rm -rf build",
			want:    []string{"rm"},
		},
		{
			name:    "quoted word with whitespace is not a reusable prefix",
			command: `npm run "my script"`,
			want:    []string{"npm run", "npm"},
		},
		{
			name:    "non-bare word in the middle stops the candidate list",
			command: `npm "my script" build`,
			want:    []string{"npm"},
		},
		{
			name:    "path word in the middle stops the candidate list",
			command: "cat README.md extra",
			want:    []string{"cat"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BashPrefixCandidates(tc.command)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("BashPrefixCandidates(%q) = %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

// Every offered candidate must actually match the command that produced it via
// the real rule matcher. A candidate that cannot match is a rule the user would
// approve and then be re-asked about forever.
func TestBashPrefixCandidatesMatchTheirCommand(t *testing.T) {
	commands := []string{
		"npm run build",
		"npm run build --silent",
		"ls -la",
		"git status -s",
		"go test ./...",
		"cat README.md",
		"pwd",
		"rm -rf build",
	}
	for _, command := range commands {
		words, ok := singleCommandWords(command)
		if !ok {
			t.Fatalf("expected singleCommandWords(%q) to succeed", command)
		}
		candidates := BashPrefixCandidates(command)
		if len(candidates) == 0 {
			t.Fatalf("expected candidates for %q", command)
		}
		for _, cand := range candidates {
			pm := NewPermissionManager()
			pm.SetBashPrefixRule(cand, PermissionAllow)
			matched, found := pm.matchBashPrefixRuleWords(words, PermissionAllow)
			if !found || matched != cand {
				t.Fatalf("candidate %q did not match %q (got %q, found=%v)", cand, command, matched, found)
			}
		}
	}
}

func TestBashPermissionRequestCarriesCandidates(t *testing.T) {
	req := bashPermissionRequest(json.RawMessage(`{"command":"npm run build"}`), "npm run build", "npm run build", []string{"npm run build", "npm run", "npm"})
	if req.Scope != PermissionScopeBashPrefix {
		t.Fatalf("scope = %q, want bash_prefix", req.Scope)
	}
	if strings.Join(req.PrefixCandidates, "|") != "npm run build|npm run|npm" {
		t.Fatalf("PrefixCandidates = %q", req.PrefixCandidates)
	}

	// Synthetic scopes persist something else (a script grant, a path root, a
	// gate that cannot be ruled on) and must not offer command-prefix rules, even
	// when a caller hands the builder a list.
	interp := bashPermissionRequest(nil, "python3 script.py", "bash.interpreter.python3", []string{"python3 script.py"})
	if len(interp.PrefixCandidates) != 0 {
		t.Fatalf("interpreter ask carried prefix candidates: %q", interp.PrefixCandidates)
	}
	sandboxed := bashPermissionRequest(nil, "cat .env", "sandbox.sensitive", []string{"cat .env"})
	if len(sandboxed.PrefixCandidates) != 0 {
		t.Fatalf("sandbox ask carried prefix candidates: %q", sandboxed.PrefixCandidates)
	}
}

// Decide must offer the narrowest derived rule as the ask's default prefix, and
// carry the full candidate list for the dialog.
func TestDecideOffersNarrowestBashPrefix(t *testing.T) {
	pm := NewPermissionManager()
	// `npm run` is on the built-in subcommand allowlist, so use a subcommand that
	// genuinely asks: `npm install`.
	d := pm.Decide("bash", json.RawMessage(`{"command":"npm install left-pad"}`))
	if d.Level != PermissionAsk || d.Request == nil {
		t.Fatalf("Decide(npm install left-pad) = %v, want ask with a request", d.Level)
	}
	if d.Request.Prefix != "npm install left-pad" {
		t.Fatalf("ask prefix = %q, want the narrowest derived rule", d.Request.Prefix)
	}
	if strings.Join(d.Request.PrefixCandidates, "|") != "npm install left-pad|npm install|npm" {
		t.Fatalf("ask candidates = %q", d.Request.PrefixCandidates)
	}
}

// A broader persisted prefix must stop asking for narrower invocations under
// it. The exact `rules[rulePrefix]` lookup alone re-asks forever because
// rulePrefix is recomputed from each command.
func TestPersistedPrefixAllowsNarrowerInvocations(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetBashPrefixRule("npm install", PermissionAllow)

	if d := pm.Decide("bash", json.RawMessage(`{"command":"npm install left-pad"}`)); d.Level != PermissionAllow {
		t.Fatalf("npm install left-pad with rule 'npm install' = %v, want allow", d.Level)
	}
	if d := pm.Decide("bash", json.RawMessage(`{"command":"npm publish"}`)); d.Level != PermissionAsk {
		t.Fatalf("npm publish with rule 'npm install' = %v, want ask", d.Level)
	}
}

func TestAlwaysRuleChoiceAvailableBashNeedsARealPrefix(t *testing.T) {
	t.Run("bash ask with no prefix offers no rule", func(t *testing.T) {
		req := PermissionRequest{ToolName: "bash", Scope: PermissionScopeTool, Rule: "tool.bash"}
		if AlwaysRuleChoiceAvailable(req) {
			t.Fatal("a bash ask with no command prefix must not offer always-rule")
		}
	})
	t.Run("sensitive path ask offers no rule", func(t *testing.T) {
		req := PermissionRequest{ToolName: "bash", Scope: PermissionScopeTool, Rule: "bash.redirection.sensitive_path", Command: "cat ~/.ssh/id_rsa"}
		if AlwaysRuleChoiceAvailable(req) {
			t.Fatal("a sensitive-path ask must not offer always-rule")
		}
	})
	t.Run("synthetic sandbox scope offers no rule", func(t *testing.T) {
		req := PermissionRequest{ToolName: "bash", Scope: PermissionScopeBashPrefix, Prefix: "sandbox.sensitive", Command: "cat .env"}
		if AlwaysRuleChoiceAvailable(req) {
			t.Fatal("a synthetic sandbox scope must not offer always-rule")
		}
	})
	t.Run("out-of-scope path ask still offers a path rule", func(t *testing.T) {
		req := PermissionRequest{ToolName: "bash", Scope: PermissionScopeTool, Rule: "bash.redirection.out_of_scope", OutOfScopePath: "/tmp/elsewhere"}
		if !AlwaysRuleChoiceAvailable(req) {
			t.Fatal("an out-of-scope path ask must still offer its path root")
		}
	})
	t.Run("interpreter ask still offers its script grant", func(t *testing.T) {
		req := PermissionRequest{ToolName: "bash", Scope: PermissionScopeBashPrefix, Prefix: "bash.interpreter.python3", Command: "python3 script.py"}
		if !AlwaysRuleChoiceAvailable(req) {
			t.Fatal("an interpreter ask must still offer its exact script grant")
		}
	})
	t.Run("real command prefix still offered", func(t *testing.T) {
		req := PermissionRequest{ToolName: "bash", Scope: PermissionScopeBashPrefix, Prefix: "npm run build", PrefixCandidates: []string{"npm run build", "npm run", "npm"}}
		if !AlwaysRuleChoiceAvailable(req) {
			t.Fatal("a real command prefix must offer always-rule")
		}
	})
}
