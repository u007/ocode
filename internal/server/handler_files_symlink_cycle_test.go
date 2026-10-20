package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBuildFileTreeHandlesSymlinkCycle pins that the walk terminates when a
// symlink points at one of its own ancestors (a/loop -> a). A cycle would
// otherwise recurse until the node budget is exhausted — and because each
// symlinked subtree gets a FRESH budget, an unbounded recursion would never
// stop at all.
//
// The cycle must be detected and the walk must return.
func TestBuildFileTreeHandlesSymlinkCycle(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// a/loop -> a (the parent): a genuine cycle.
	if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "a", "loop")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// A self-referencing link too.
	if err := os.Symlink(root, filepath.Join(root, "a", "self")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// The walk MUST terminate. Run it with a watchdog so a hang fails the
	// test instead of blocking the suite forever.
	done := make(chan struct{})
	var w *fileTreeWalk
	var node FileNode
	var err error
	go func() {
		defer close(done)
		w = &fileTreeWalk{limit: maxTreeNodes}
		node, err = buildFileTree(root, root, 0, 0, w, map[string]bool{}, false)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("buildFileTree did not terminate on a symlink cycle")
	}
	if err != nil {
		t.Fatalf("buildFileTree: %v", err)
	}
	// The real file must still be indexed.
	paths := flattenTree(t, node.Children)
	if !contains(paths, "a/file.txt") {
		t.Errorf("a/file.txt missing from tree; got %v", paths)
	}
	// The walk must stay bounded.
	if w.visited > maxTreeNodes {
		t.Errorf("visited %d nodes, want <= %d", w.visited, maxTreeNodes)
	}
	// A cycle must NOT be walked: the same file must not be indexed under
	// ever-deeper paths (a/loop/loop/.../file.txt). Before the cycle check
	// the walk recursed to depth 25 and indexed the file 26 times.
	depth := treeDepth(node.Children)
	if depth > 4 {
		t.Errorf("tree depth %d, want <= 4: the symlink cycle was descended into", depth)
	}
	seen := map[string]int{}
	for _, p := range paths {
		seen[p]++
		if seen[p] > 1 {
			t.Errorf("path %q appears %d times; the cycle was walked", p, seen[p])
		}
	}
}

// treeDepth returns the maximum depth of the returned tree.
func treeDepth(nodes []FileNode) int {
	deepest := 0
	for _, n := range nodes {
		if d := treeDepth(n.Children) + 1; d > deepest {
			deepest = d
		}
	}
	return deepest
}

// TestBuildFileTreeContainsSymlinkedSubtree checks that a symlinked
// directory's contents are still addressed relative to the project root, so
// the frontend can open them (the Files tab expands such a folder with its
// own request whose root is the folder).
func TestBuildFileTreeContainsSymlinkedSubtree(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "inside.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	w := &fileTreeWalk{limit: maxTreeNodes}
	node, err := buildFileTree(root, root, 0, 0, w, map[string]bool{}, false)
	if err != nil {
		t.Fatal(err)
	}
	paths := flattenTree(t, node.Children)
	// The linked file's path must be root-relative (link/inside.txt), never
	// an absolute path into the external directory.
	if !contains(paths, filepath.Join("link", "inside.txt")) {
		t.Errorf("expected link/inside.txt in tree, got %v", paths)
	}
	for _, p := range paths {
		if filepath.IsAbs(p) {
			t.Errorf("tree path %q is absolute; paths must be root-relative", p)
		}
	}
}
