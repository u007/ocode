package commandctx

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestCollectDocSyncTargetsBriefing pins which root briefing /doc-sync may
// edit: AGENTS.md when present (CLAUDE.md beside it is a pointer), else a
// lone CLAUDE.md — so a CLAUDE.md-only repo is never nudged into recreating a
// duplicate AGENTS.md.
func TestCollectDocSyncTargetsBriefing(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  []string
	}{
		{"agents only", []string{"AGENTS.md"}, []string{"AGENTS.md"}},
		{"both prefers agents", []string{"AGENTS.md", "CLAUDE.md"}, []string{"AGENTS.md"}},
		{"claude only", []string{"CLAUDE.md", "OCODE.md"}, []string{"CLAUDE.md", "OCODE.md"}},
		{"neither", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("x\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := collectDocSyncTargets(dir); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("collectDocSyncTargets = %v, want %v", got, tc.want)
			}
		})
	}
}
