package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodoSnapshotOpenItems(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".ocode", "todo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Todo (revision 1)\n\n- [ ] t1 pending item\n- [•] t2 in progress item\n- [✓] t3 done item\n"
	if err := os.WriteFile(filepath.Join(dir, "ses-x.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := ReadTodoSnapshot(TodoScope{ProjectRoot: root, SessionID: "ses-x"})
	if !snap.HasOpenItems {
		t.Fatal("expected open items")
	}
	if len(snap.OpenItems) != 2 {
		t.Fatalf("OpenItems len = %d, want 2", len(snap.OpenItems))
	}
	if snap.OpenItems[0].ID != "t1" || snap.OpenItems[0].Status != "pending" {
		t.Fatalf("item0 = %+v", snap.OpenItems[0])
	}
	if snap.OpenItems[1].ID != "t2" || snap.OpenItems[1].Status != "in_progress" {
		t.Fatalf("item1 = %+v", snap.OpenItems[1])
	}
}

func TestTodoSnapshotBlockedIDs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".ocode", "todo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Todo (revision 1)\n\n- [ ] t1 ready\n- [ ] t2 waiting\n  deps: t1\n- [•] t3 running blocked\n  deps: t2\n"
	if err := os.WriteFile(filepath.Join(dir, "ses-b.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := ReadTodoSnapshot(TodoScope{ProjectRoot: root, SessionID: "ses-b"})
	if got := strings.Join(snap.BlockedIDs, ","); got != "t2,t3" {
		t.Fatalf("BlockedIDs = %q, want t2,t3", got)
	}
	if !strings.Contains(strings.Join(snap.Blocked, ";"), "t2 by t1") {
		t.Fatalf("Blocked display lines = %v", snap.Blocked)
	}
	if !snap.HasOpenItems || len(snap.OpenItems) != 3 {
		t.Fatalf("snapshot open items = %+v", snap.OpenItems)
	}
}
