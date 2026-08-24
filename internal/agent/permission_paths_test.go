package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestAlwaysRuleChoiceAvailable(t *testing.T) {
	cases := []struct {
		name string
		req  PermissionRequest
		want bool
	}{
		{
			name: "plain bash prefix allowed",
			req:  PermissionRequest{ToolName: "bash", Scope: PermissionScopeBashPrefix, Prefix: "rm"},
			want: true,
		},
		{
			name: "git subcommand prefix excluded",
			req:  PermissionRequest{ToolName: "bash", Scope: PermissionScopeBashPrefix, Prefix: "git push"},
			want: false,
		},
		{
			name: "bare git prefix excluded",
			req:  PermissionRequest{ToolName: "bash", Scope: PermissionScopeBashPrefix, Prefix: "git"},
			want: false,
		},
		{
			name: "shell control keyword excluded",
			req:  PermissionRequest{ToolName: "bash", Scope: PermissionScopeBashPrefix, Prefix: "while"},
			want: false,
		},
		{
			name: "non-bash tool unaffected by bash rules",
			req:  PermissionRequest{ToolName: "webfetch", Scope: PermissionScopeTool, Rule: "webfetch.domain.example.com"},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AlwaysRuleChoiceAvailable(tc.req); got != tc.want {
				t.Fatalf("AlwaysRuleChoiceAvailable(%+v) = %v, want %v", tc.req, got, tc.want)
			}
		})
	}
}

func TestAlwaysToolChoiceAvailable(t *testing.T) {
	if AlwaysToolChoiceAvailable(PermissionRequest{ToolName: "bash"}) {
		t.Fatalf("bash must not offer always-tool")
	}
	if !AlwaysToolChoiceAvailable(PermissionRequest{ToolName: "delete"}) {
		t.Fatalf("non-bash tools must offer always-tool")
	}
}

func TestOutOfScopePathRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")

	cases := []struct {
		name string
		req  PermissionRequest
		want string
	}{
		{
			name: "explicit out-of-scope directory",
			req:  PermissionRequest{OutOfScopePath: dir},
			want: dir,
		},
		{
			name: "explicit out-of-scope file resolves to parent",
			req:  PermissionRequest{OutOfScopePath: file},
			want: dir,
		},
		{
			name: "out_of_scope rule falls back to args path",
			req: PermissionRequest{
				Rule: "bash.command.out_of_scope",
				Args: json.RawMessage(`{"path":"` + file + `"}`),
			},
			want: dir,
		},
		{
			name: "path_pattern rule falls back to args file_path",
			req: PermissionRequest{
				Rule: "tool.write.path_pattern",
				Args: json.RawMessage(`{"file_path":"` + file + `"}`),
			},
			want: dir,
		},
		{
			name: "relative path yields empty root",
			req:  PermissionRequest{OutOfScopePath: "relative/path"},
			want: "",
		},
		{
			name: "unrelated request yields empty root",
			req:  PermissionRequest{ToolName: "bash", Command: "ls", Rule: "bash.prefix.ls"},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OutOfScopePathRoot(tc.req); got != tc.want {
				t.Fatalf("OutOfScopePathRoot() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsOutOfScopePathRequest(t *testing.T) {
	if !IsOutOfScopePathRequest(PermissionRequest{OutOfScopePath: "/tmp/x"}) {
		t.Fatalf("OutOfScopePath set → out-of-scope ask")
	}
	if !IsOutOfScopePathRequest(PermissionRequest{Rule: "bash.command.out_of_scope"}) {
		t.Fatalf(".out_of_scope suffix → out-of-scope ask")
	}
	if !IsOutOfScopePathRequest(PermissionRequest{Rule: "tool.edit.path_pattern"}) {
		t.Fatalf(".path_pattern suffix → out-of-scope ask")
	}
	if IsOutOfScopePathRequest(PermissionRequest{Rule: "bash.prefix.rm"}) {
		t.Fatalf("plain prefix rule is not an out-of-scope ask")
	}
}
