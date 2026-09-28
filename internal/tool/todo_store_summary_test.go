package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTodoFileAt mirrors the on-disk layout (.ocode/todo/<id>.md) for a
// specific project root, which the process-global todoDir() cannot express.
func writeTodoFileAt(t *testing.T, root, sessionID, content string) {
	t.Helper()
	dir := filepath.Join(root, ".ocode", "todo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write todo file: %v", err)
	}
}

func TestReadTodoSummaryCountsAndOrder(t *testing.T) {
	root := t.TempDir()
	writeTodoFileAt(t, root, "ses_1", "# Todo (revision 3)\n- [✓] t1 read the spec\n- [•] t2 wire the store\n- [ ] t3 add the test\n")

	sum, ok, err := ReadTodoSummary(root, "ses_1")
	if err != nil {
		t.Fatalf("ReadTodoSummary: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true for an existing todo file")
	}
	if sum.Done != 1 || sum.Total != 3 {
		t.Errorf("Done/Total = %d/%d, want 1/3", sum.Done, sum.Total)
	}
	if sum.Current != "wire the store" {
		t.Errorf("Current = %q, want %q", sum.Current, "wire the store")
	}
	want := []TodoSummaryItem{
		{Text: "read the spec", State: "done"},
		{Text: "wire the store", State: "in_progress"},
		{Text: "add the test", State: "pending"},
	}
	if len(sum.Items) != len(want) {
		t.Fatalf("Items = %d, want %d (%+v)", len(sum.Items), len(want), sum.Items)
	}
	for i, w := range want {
		if sum.Items[i] != w {
			t.Errorf("Items[%d] = %+v, want %+v", i, sum.Items[i], w)
		}
	}
}

func TestReadTodoSummaryAbsentFileIsNotAnError(t *testing.T) {
	sum, ok, err := ReadTodoSummary(t.TempDir(), "ses_missing")
	if err != nil {
		t.Fatalf("absent file must not be an error, got %v", err)
	}
	if ok {
		t.Error("ok = true, want false for a missing todo file")
	}
	if sum.Total != 0 || sum.Current != "" {
		t.Errorf("summary = %+v, want zero value", sum)
	}
}

func TestReadTodoSummaryParseErrorNamesPath(t *testing.T) {
	root := t.TempDir()
	// No header, and every line lacks the "- [ ]" marker: the strict parser
	// rejects it, so the reader must surface the offending path.
	writeTodoFileAt(t, root, "ses_bad", "just some prose\nwith no list items\n")

	_, ok, err := ReadTodoSummary(root, "ses_bad")
	if err == nil {
		t.Fatal("err = nil, want a parse error for unparsable content")
	}
	if !ok {
		t.Error("ok = false, want true — the file exists, only its content is bad")
	}
	if !strings.Contains(err.Error(), "ses_bad.md") {
		t.Errorf("error %q does not name the todo path", err)
	}
}

func TestReadTodoSummaryEmptyListHasNoTotal(t *testing.T) {
	root := t.TempDir()
	writeTodoFileAt(t, root, "ses_empty", "# Todo (revision 0)\n")

	sum, ok, err := ReadTodoSummary(root, "ses_empty")
	if err != nil {
		t.Fatalf("ReadTodoSummary: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true for an existing (empty) todo file")
	}
	if sum.Total != 0 || sum.Done != 0 || sum.Current != "" {
		t.Errorf("summary = %+v, want zero counts", sum)
	}
}

func TestReadTodoSummaryIsScopedToGivenRoot(t *testing.T) {
	// The process-global todoDir() resolves from the cwd, so a summary for a
	// project must never leak another project's todo file.
	rootA, rootB := t.TempDir(), t.TempDir()
	writeTodoFileAt(t, rootA, "ses_same", "# Todo (revision 1)\n- [ ] t1 alpha work\n")
	writeTodoFileAt(t, rootB, "ses_same", "# Todo (revision 1)\n- [ ] t1 beta work\n- [ ] t2 more beta\n")

	sumA, _, err := ReadTodoSummary(rootA, "ses_same")
	if err != nil {
		t.Fatalf("ReadTodoSummary(rootA): %v", err)
	}
	if sumA.Total != 1 {
		t.Errorf("rootA total = %d, want 1 (read the wrong project's file)", sumA.Total)
	}
	sumB, _, err := ReadTodoSummary(rootB, "ses_same")
	if err != nil {
		t.Fatalf("ReadTodoSummary(rootB): %v", err)
	}
	if sumB.Total != 2 {
		t.Errorf("rootB total = %d, want 2", sumB.Total)
	}
}
