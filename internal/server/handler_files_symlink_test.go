package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestBuildFileTreeSymlinkedDirIsBounded pins the root cause of the
// Ctrl/Cmd+P picker losing files: a symlinked directory used to be walked
// with the tree's whole node budget, so one link (e.g. a project symlinking
// a shared asset dir holding 40731 files) could spend ALL of maxTreeNodes
// inside the link and silently drop every not-yet-visited sibling subtree —
// including the project root's own files.
//
// The symlinked directory must still be followed (the Files tab and the
// picker both let the user open one), but it gets its own node budget so it
// cannot starve the rest of the tree.
func TestBuildFileTreeSymlinkedDirIsBounded(t *testing.T) {
	root := t.TempDir()

	// An "external" tree LARGER than the whole tree's node budget: with the
	// symlink unbounded it consumes maxTreeNodes on its own and the
	// project's own files are never reached. With the symlink bounded to
	// maxSymlinkTreeNodes the walk costs a fraction of that.
	external := t.TempDir()
	for i := 0; i < maxTreeNodes+200; i++ {
		dir := filepath.Join(external, "d"+pad3(i/50))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "f"+pad3(i%50)+".txt"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// Real files in the project root — they must survive the walk.
	if err := os.WriteFile(filepath.Join(root, "root.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "nested.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	w := &fileTreeWalk{limit: maxTreeNodes}
	node, err := buildFileTree(root, root, 0, 0, w, map[string]bool{}, false)
	if err != nil {
		t.Fatal(err)
	}

	paths := flattenTree(t, node.Children)
	for _, want := range []string{"root.txt", "sub/nested.txt"} {
		if !contains(paths, want) {
			t.Errorf("%s missing from tree; the symlinked subtree starved the walk", want)
		}
	}
	// The symlink is still followed, so its contents are listed.
	if !contains(paths, "linked") {
		t.Errorf("symlinked dir should still be listed; got %v", paths)
	}
	linked := 0
	for _, p := range paths {
		if len(p) > 7 && p[:7] == "linked/" {
			linked++
		}
	}
	if linked == 0 {
		t.Error("symlinked dir was not followed at all; its contents should be listed")
	}
	if linked > maxSymlinkTreeNodes {
		t.Errorf("symlinked subtree contributed %d nodes, want <= %d", linked, maxSymlinkTreeNodes)
	}
	// The whole tree must stay well under maxTreeNodes: the link was bounded.
	if w.visited > maxTreeNodes {
		t.Errorf("tree visited %d nodes, want <= %d", w.visited, maxTreeNodes)
	}
	// THE USER-VISIBLE SYMPTOM: with the link unbounded, the external tree
	// eats the whole budget and the project's own files vanish from the
	// index. Assert the tree's own files are still indexed even though the
	// link holds far more files than the entire tree budget.
	if linked >= maxTreeNodes {
		t.Errorf("symlinked subtree contributed %d nodes (>= maxTreeNodes %d); it must not be able to consume the tree's whole budget", linked, maxTreeNodes)
	}
}

// TestBuildFileTreeSymlinkBudgetIsCollective pins that MANY symlinks still
// cannot starve the tree: the symlink budget is shared across every symlink
// in the walk, so a project with several linked asset directories still
// leaves the bulk of the node budget for its own files.
func TestBuildFileTreeSymlinkBudgetIsCollective(t *testing.T) {
	root := t.TempDir()

	// Three symlinks, each pointing at a large external tree.
	for i := 0; i < 3; i++ {
		external := t.TempDir()
		for j := 0; j < maxSymlinkTreeNodes; j++ {
			dir := filepath.Join(external, "d"+pad3(j/50))
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "f"+pad3(j%50)+".txt"), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		link := filepath.Join(root, "link"+pad3(i))
		if err := os.Symlink(external, link); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
	}
	// The project's own files must survive all three symlinks.
	for _, name := range []string{"own1.txt", "own2.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	w := &fileTreeWalk{limit: maxTreeNodes}
	node, err := buildFileTree(root, root, 0, 0, w, map[string]bool{}, false)
	if err != nil {
		t.Fatal(err)
	}
	paths := flattenTree(t, node.Children)
	for _, want := range []string{"own1.txt", "own2.txt"} {
		if !contains(paths, want) {
			t.Errorf("%s missing from tree; the symlinks took the whole budget", want)
		}
	}
	if w.symlinkVisited > maxSymlinkTreeNodes {
		t.Errorf("symlinked subtrees contributed %d nodes, want <= %d", w.symlinkVisited, maxSymlinkTreeNodes)
	}
}

// TestBuildFileTreeIndexesRootFilesBeforeSubtrees pins the other half of the
// truncation bug: the node budget stops the walk DEAD, so a directory visited
// first can swallow the remaining budget and starve the files that come after
// it. Every file directly in a directory must be indexed before any subtree
// competes for the budget.
//
// The subtree below is deliberately larger than maxTreeNodes, so the visit
// order decides whether the root's files are ever reached.
func TestBuildFileTreeIndexesRootFilesBeforeSubtrees(t *testing.T) {
	root := t.TempDir()
	// "aaa-big" sorts BEFORE the root files, so a dirs-first visit order
	// enters its subtree first and exhausts the budget there.
	big := filepath.Join(root, "aaa-big")
	if err := os.Mkdir(big, 0755); err != nil {
		t.Fatal(err)
	}
	for d := 0; d < 700; d++ {
		dir := filepath.Join(big, "d"+pad3(d))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < 30; f++ {
			if err := os.WriteFile(filepath.Join(dir, "f"+pad3(f)+".txt"), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Root files sort AFTER "aaa-big" alphabetically, so they are only
	// reached when files are visited before directories.
	for _, name := range []string{"zzz1.txt", "zzz2.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	w := &fileTreeWalk{limit: maxTreeNodes}
	node, err := buildFileTree(root, root, 0, 0, w, map[string]bool{}, false)
	if err != nil {
		t.Fatal(err)
	}
	paths := flattenTree(t, node.Children)
	for _, want := range []string{"zzz1.txt", "zzz2.txt"} {
		if !contains(paths, want) {
			t.Errorf("%s missing from tree (visited %d nodes); the walk visited a subtree before the directory's own files", want, w.visited)
		}
	}
	if !w.truncated {
		t.Error("truncated = false, want true: the subtree exceeded the node budget")
	}
}

// TestHandleFileTreeReportsTruncation checks the truncation signal still
// reaches the client when the node cap really is hit, so the UI can say the
// result set is incomplete.
func TestHandleFileTreeReportsTruncation(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	// Enough nodes to exceed maxTreeNodes.
	for d := 0; d < 40; d++ {
		dir := filepath.Join(tmpDir, "d"+string(rune('a'+d/26))+string(rune('a'+d%26)))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < 600; f++ {
			name := filepath.Join(dir, "f"+string(rune('a'+f/26))+string(rune('a'+f%26))+".txt")
			if err := os.WriteFile(name, []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/tree?depth=0", nil)
	h.HandleFileTree(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp FileTreeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated {
		t.Error("Truncated = false, want true for a tree over the node cap")
	}
}

// pad3 renders n as a 3-digit string so generated names sort predictably.
func pad3(n int) string {
	s := "000" + strconv.Itoa(n)
	return s[len(s)-3:]
}
