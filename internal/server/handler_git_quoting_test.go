package server

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Git C-quotes any path with non-ASCII bytes under the default
// core.quotepath, so these names reach the parsers in quoted form unless the
// probes ask for `-z`. The em dash is the real-world trigger.
const (
	nonASCIITracked   = "James Tan — CV.pdf"
	nonASCIIUntracked = "Résumé — draft.md"
)

func setupNonASCIIRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, nonASCIITracked), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", nonASCIITracked)
	runGit(t, dir, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, nonASCIITracked), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, nonASCIIUntracked), []byte("draft"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGitStatusListsNonASCIIPathsUnquoted(t *testing.T) {
	dir := setupNonASCIIRepo(t)
	st, err := gitStatusForDir(dir)
	if err != nil {
		t.Fatalf("gitStatusForDir: %v", err)
	}
	for _, want := range []string{nonASCIITracked, nonASCIIUntracked} {
		if !slices.Contains(st.ChangedFiles, want) {
			t.Fatalf("ChangedFiles missing %q (raw name), got %q", want, st.ChangedFiles)
		}
	}
}

func TestGitWorkspaceDiffsKeepNonASCIIPathsUnquoted(t *testing.T) {
	dir := setupNonASCIIRepo(t)
	files := diffFilesForDir(dir, false, "")
	byPath := map[string]GitDiffFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	tracked, ok := byPath[nonASCIITracked]
	if !ok {
		t.Fatalf("workspace missing modified tracked file %q, got %+v", nonASCIITracked, files)
	}
	if tracked.Patch == "" {
		t.Fatalf("modified tracked file %q has empty patch", nonASCIITracked)
	}
	untracked, ok := byPath[nonASCIIUntracked]
	if !ok {
		t.Fatalf("workspace missing untracked file %q, got %+v", nonASCIIUntracked, files)
	}
	if untracked.Status != "untracked" || untracked.Patch == "" {
		t.Fatalf("untracked file %q: status=%q patchLen=%d", nonASCIIUntracked, untracked.Status, len(untracked.Patch))
	}
}

func TestGitStageNonASCIIUntrackedPath(t *testing.T) {
	dir := setupNonASCIIRepo(t)
	h := NewHandler()
	h.workDir = dir
	r, w := call("/api/git/stage", map[string]any{"paths": []string{nonASCIIUntracked}})
	h.HandleGitStage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 staging %q, got %d: %s", nonASCIIUntracked, w.Code, w.Body.String())
	}
	st, err := gitStatusForDir(dir)
	if err != nil {
		t.Fatalf("gitStatusForDir: %v", err)
	}
	if !slices.Contains(st.StagedFiles, nonASCIIUntracked) {
		t.Fatalf("expected %q staged, got %q", nonASCIIUntracked, st.StagedFiles)
	}
}

func TestGitStatusZUntrackedPaths(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want []string
	}{
		{
			name: "untracked and modified records",
			out:  "?? a b.txt\x00?? caf\xc3\xa9.md\x00 M tracked.go\x00",
			want: []string{"a b.txt", "caf\xc3\xa9.md"},
		},
		{
			// The original path of a rename is its own NUL record with no
			// status prefix; it must be skipped, not read as "??".
			name: "rename with an original path that looks untracked",
			out:  "R  new?x.txt\x00?? old\x00?? real.txt\x00",
			want: []string{"real.txt"},
		},
		{
			name: "worktree rename skips its original path",
			out:  " R new.txt\x00?? old.txt\x00?? real.txt\x00",
			want: []string{"real.txt"},
		},
		{
			name: "empty output",
			out:  "",
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := untrackedPathsFromStatusZ(tc.out)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("untrackedPathsFromStatusZ(%q) = %q, want %q", tc.out, got, tc.want)
			}
		})
	}
}

func TestParseUnifiedDiffDecodesQuotedHeader(t *testing.T) {
	diff := "diff --git \"a/James Tan \\342\\200\\224 CV.pdf\" \"b/James Tan \\342\\200\\224 CV.pdf\"\n" +
		"index 2e65efe..9ae9e86 100644\n" +
		"--- \"a/James Tan \\342\\200\\224 CV.pdf\"\n" +
		"+++ \"b/James Tan \\342\\200\\224 CV.pdf\"\n" +
		"@@ -1 +1 @@\n-v1\n+v2\n"
	files := parseUnifiedDiff(diff)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %+v", len(files), files)
	}
	if files[0].Path != nonASCIITracked {
		t.Fatalf("Path = %q, want %q", files[0].Path, nonASCIITracked)
	}
}
