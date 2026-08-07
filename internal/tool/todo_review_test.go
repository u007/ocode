package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRawTodoFile drops arbitrary bytes into a session's todo file, bypassing
// the store — the only way to simulate an externally corrupted file.
func writeRawTodoFile(t *testing.T, sessionID, content string) {
	t.Helper()
	if err := os.MkdirAll(todoDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(todoDir(), sessionID+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Regression tests for the review findings on the todo store. Each fails
// against the pre-fix code.

// Finding: assignTodoIDs stamped t1..tN on the incoming items BEFORE the
// destructive guard compared ids, so every old id appeared to survive and a
// same-length replacement silently destroyed completed work. The existing
// guard test passed only because its replacement list was shorter.
func TestTodoWriteSameLengthUnrelatedReplaceRejected(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-guard-samelen")
	writeTodoText(t, "- [✓] audit deps\n- [ ] write tests")

	// Two completely unrelated items — same count as the existing list.
	_, err := todoWrite(1, "- [ ] refactor parser\n- [ ] update docs", nil, "")
	if err == nil {
		t.Fatal("expected rejection: a same-length replace still drops both existing items")
	}
	if !strings.Contains(err.Error(), "audit deps") {
		t.Fatalf("rejection should name the dropped completed item: %v", err)
	}
}

// Finding: positional id assignment also broke id stability across a legal
// reorder, so a later todo_update citing "t1" mutated the wrong item.
func TestTodoWriteReorderKeepsIDsWithItems(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-reorder")
	if _, err := todoWrite(0, "- [ ] alpha\n- [ ] beta", nil, ""); err != nil {
		t.Fatalf("first write: %v", err)
	}
	_, before, err := readTodo()
	if err != nil {
		t.Fatalf("readTodo: %v", err)
	}
	alphaID := idOfText(t, before, "alpha")

	// Legal reorder: same items, swapped order.
	if _, err := todoWrite(1, "- [ ] beta\n- [ ] alpha", nil, ""); err != nil {
		t.Fatalf("reorder rejected: %v", err)
	}
	_, after, err := readTodo()
	if err != nil {
		t.Fatalf("readTodo after reorder: %v", err)
	}
	if got := idOfText(t, after, "alpha"); got != alphaID {
		t.Fatalf("id for %q changed across a reorder: %s -> %s", "alpha", alphaID, got)
	}
}

func idOfText(t *testing.T, content, text string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, text) {
			fields := strings.Fields(line)
			// "- [x] tN text"
			if len(fields) >= 3 {
				return fields[2]
			}
		}
	}
	t.Fatalf("no item with text %q in:\n%s", text, content)
	return ""
}

// Finding: stripTodoHeader's second TrimPrefix could never match, so the
// header residue ("3)") rendered above the sidebar list on every frame.
func TestStripTodoHeaderRemovesWholeHeaderLine(t *testing.T) {
	got := stripTodoHeader("# Todo (revision 1)\n\n- [ ] t1 ship task 4\n")
	if strings.Contains(got, "1)") || strings.HasPrefix(got, "#") {
		t.Fatalf("header residue left behind: %q", got)
	}
	if !strings.HasPrefix(got, "- [ ] t1 ship task 4") {
		t.Fatalf("item lines mangled: %q", got)
	}
}

// Finding: legacy headerless content (restoreTodoState feeds pre-store session
// metadata through SetTodoState) made todoread hard-fail on every call, and
// baseItems swallowed the same parse error so the first todowrite skipped the
// destructive guard and wiped the restored list.
func TestLegacyRestoredSessionReadsAndIsGuarded(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-legacy")
	SetTodoState("- [ ] first\n- [✓] second")

	if _, content, err := readTodo(); err != nil {
		t.Fatalf("todoread must tolerate legacy headerless content: %v", err)
	} else if !strings.Contains(content, "first") {
		t.Fatalf("legacy items missing from todoread: %q", content)
	}

	// The guard must still see the restored items.
	if _, err := todoWrite(0, "- [ ] something else", nil, ""); err == nil {
		t.Fatal("expected the destructive guard to protect legacy restored items")
	}
}

// Finding: a corrupt file rendered as "No todo list for this session yet" in
// the sidebar — the one surface where the failure was silent.
func TestTodoStateSurfacesParseError(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-corrupt")
	writeRawTodoFile(t, "ses-corrupt", "this is not a todo file")
	SetTodoSession("ses-corrupt") // reload from disk

	got := TodoState()
	if got == "" {
		t.Fatal("a corrupt todo file must not render as an empty list")
	}
	if !strings.Contains(got, "unreadable") {
		t.Fatalf("expected an explicit unreadable message, got %q", got)
	}
}
