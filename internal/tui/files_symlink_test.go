package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// A symlink to a directory must classify as a directory so linked folders
// expand like regular folders in the TUI files tab. DirEntry.IsDir reports
// the link itself (false); loadDirChildren follows the target via os.Stat.
func TestLoadDirChildrenSymlinkedDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "realdir")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "inside.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "linkdir")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aaa.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, err := loadDirChildren(dir, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]fileNode{}
	for _, n := range nodes {
		byName[n.name] = n
	}
	link, ok := byName["linkdir"]
	if !ok {
		t.Fatalf("expected linkdir in nodes, got %+v", nodes)
	}
	if !link.isDir {
		t.Fatalf("expected linkdir isDir=true, got %+v", link)
	}
	if _, ok := byName["aaa.txt"]; !ok {
		t.Fatalf("expected aaa.txt in nodes, got %+v", nodes)
	}
	// Expanding the link must yield the target's children.
	children, err := loadDirChildren(link.path, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range children {
		if c.name == "inside.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected inside.txt expanding linkdir, got %+v", children)
	}
}
