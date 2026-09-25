package server

import (
	"strings"
	"testing"
)

// Phase 07 characterization test.
//
// For an unmerged path, `git diff` emits a COMBINED diff, not an ordinary one:
//
//	diff --cc f.txt
//	@@@ -1,1 -1,1 +1,5 @@@
//	++<<<<<<< HEAD
//	 +ours
//	++=======
//	+ theirs
//	++>>>>>>> other
//
// parseUnifiedDiff keys off `diff --git`, so a combined section never opens a
// file entry and every one of its lines is dropped. The consequence is that a
// conflicted file produces NO GitDiffFile, so the web editor falls back to an
// empty git patch and shows no decorations for it. That is currently accepted
// behavior (the Git tab owns conflict resolution and the file tree badges the
// file), but it was discovered rather than designed, so it is pinned here.
//
// If someone teaches the parser combined diffs, THIS TEST SHOULD FAIL — that is
// the signal to update the web-side test in web/src/lib/editorDiffSource.test.ts
// and the TODO entry together, not to delete this one.
func TestParseUnifiedDiffDropsCombinedConflictDiffs(t *testing.T) {
	combined := "diff --cc f.txt\n" +
		"index b19a1e9,950b81e..0000000\n" +
		"--- a/f.txt\n" +
		"+++ b/f.txt\n" +
		"@@@ -1,1 -1,1 +1,5 @@@\n" +
		"++<<<<<<< HEAD\n" +
		" +ours\n" +
		"++=======\n" +
		"+ theirs\n" +
		"++>>>>>>> other\n"

	if got := parseUnifiedDiff(combined); len(got) != 0 {
		t.Fatalf("parseUnifiedDiff(combined) = %+v, want no entries; a combined "+
			"`diff --cc` section is not recognized, so the conflicted file is dropped", got)
	}
}

// The ordinary two-tree diff must still parse, so the test above cannot pass
// merely because the parser is broken.
func TestParseUnifiedDiffStillParsesOrdinaryDiffs(t *testing.T) {
	ordinary := "diff --git a/f.txt b/f.txt\n" +
		"index 1234567..89abcde 100644\n" +
		"--- a/f.txt\n" +
		"+++ b/f.txt\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"+new\n"

	got := parseUnifiedDiff(ordinary)
	if len(got) != 1 {
		t.Fatalf("parseUnifiedDiff(ordinary) returned %d entries, want 1", len(got))
	}
	if got[0].Path != "f.txt" {
		t.Errorf("Path = %q, want %q", got[0].Path, "f.txt")
	}
	if !strings.Contains(got[0].Patch, "-old") || !strings.Contains(got[0].Patch, "+new") {
		t.Errorf("Patch = %q, want it to carry the -old/+new lines", got[0].Patch)
	}
}
