package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// sqlite_schema / sqlite_query are read-only and ride allow inside the
// project; sqlite_exec mutates, so it asks; a path outside the allowed roots
// asks even for the read tools.
func TestSqliteToolPermissions(t *testing.T) {
	root := t.TempDir()
	pm := NewPermissionManager()
	pm.SetWorkDir(root)
	in := filepath.Join(root, "t.db")
	args := func(p string) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"path": p, "sql": "SELECT 1"})
		return b
	}
	cases := []struct {
		tool, path string
		want       PermissionLevel
	}{
		{"sqlite_schema", in, PermissionAllow},
		{"sqlite_query", in, PermissionAllow},
		{"sqlite_query", "rel.db", PermissionAllow},
		{"sqlite_exec", in, PermissionAsk},
		{"sqlite_query", "/etc/outside.db", PermissionAsk},
		{"sqlite_exec", "/etc/outside.db", PermissionAsk},
	}
	for _, tc := range cases {
		if got := pm.Decide(tc.tool, args(tc.path)).Level; got != tc.want {
			t.Errorf("%s %s = %v, want %v", tc.tool, tc.path, got, tc.want)
		}
	}
	if !isReadOnlyTool("sqlite_query") || isReadOnlyTool("sqlite_exec") {
		t.Error("isReadOnlyTool classification wrong")
	}
	for _, m := range []Mode{ModePlan, ModeDebug} {
		if _, ok := gateToolCall(m, "sqlite_query", nil); !ok {
			t.Errorf("sqlite_query blocked in %s mode", m)
		}
		if _, ok := gateToolCall(m, "sqlite_exec", nil); ok {
			t.Errorf("sqlite_exec allowed in %s mode", m)
		}
	}
}
