package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/snapshot"
)

// chdirTemp points the store at a throwaway directory (the store resolves the
// project root from os.Getwd()) and resets the in-memory store state.
func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	resetTodoStoreForTest()
}

func resetTodoStoreForTest() {
	todoStoreState.mu.Lock()
	defer todoStoreState.mu.Unlock()
	todoStoreState.current = ""
	todoStoreState.sessions = map[string]*todoSession{}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeTodoText(t *testing.T, text string) {
	t.Helper()
	args := mustJSON(t, map[string]any{"revision": 0, "todoText": text})
	if _, err := (TodoWriteTool{}).Execute(args); err != nil {
		t.Fatalf("todowrite: %v", err)
	}
}

func readTodoText(t *testing.T) string {
	t.Helper()
	got, err := (TodoReadTool{}).Execute(nil)
	if err != nil {
		t.Fatalf("todoread: %v", err)
	}
	return got
}

func todoFileExists(sessionID string) bool {
	_, err := os.Stat(filepath.Join(todoDir(), sessionID+".md"))
	return err == nil
}

func readTodoFileRaw(t *testing.T, sessionID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(todoDir(), sessionID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// --- Task 2: store round-trip, strict parse, atomic writes ---

func TestTodoStoreRoundTrip(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-roundtrip")
	writeTodoText(t, "- [ ] first\n- [•] second\n- [✓] third")

	got := readTodoText(t)
	if !strings.Contains(got, "revision: 1") {
		t.Fatalf("expected revision 1 in read, got %q", got)
	}
	if !strings.Contains(got, "- [ ] t1 first") ||
		!strings.Contains(got, "- [•] t2 second") ||
		!strings.Contains(got, "- [✓] t3 third") {
		t.Fatalf("expected canonical items with ids, got %q", got)
	}

	// The file on disk is the canonical serialized form (header + items).
	raw := readTodoFileRaw(t, "ses-roundtrip")
	if !strings.HasPrefix(raw, "# Todo (revision 1)\n") {
		t.Fatalf("expected header on disk, got %q", raw)
	}
	// Strict parse round-trips back to the same items.
	rev, items, err := parseTodoDoc(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if rev != 1 || len(items) != 3 {
		t.Fatalf("round-trip mismatch: rev=%d items=%d", rev, len(items))
	}
}

func TestTodoStoreMalformedFileRefusesWrites(t *testing.T) {
	chdirTemp(t)
	dir := todoDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := "# Todo (revision 1)\n\nthis is not a todo item line\n- [ ] t1 ok\n"
	path := filepath.Join(dir, "ses-bad.md")
	if err := os.WriteFile(path, []byte(bad), 0644); err != nil {
		t.Fatal(err)
	}

	// Resume the session: SetTodoSession loads the malformed file and records
	// the parse error.
	SetTodoSession("ses-bad")

	// todoread surfaces the parse error with the path.
	if _, err := (TodoReadTool{}).Execute(nil); err == nil || !strings.Contains(err.Error(), "failed to parse") {
		t.Fatalf("expected todoread to surface parse error, got %v", err)
	}

	// A mutation must be refused and the file left untouched.
	args := mustJSON(t, map[string]any{"revision": 2, "todoText": "- [ ] new"})
	_, writeErr := (TodoWriteTool{}).Execute(args)
	if writeErr == nil {
		t.Fatal("expected write to be refused on malformed file")
	}
	if !strings.Contains(writeErr.Error(), path) {
		t.Fatalf("expected write error to carry the file path, got %v", writeErr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != bad {
		t.Fatal("malformed file was modified despite refused write")
	}
}

func TestTodoStoreConcurrentWritesNoInterleave(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-concurrent")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			text := "- [ ] item " + string(rune('a'+n))
			args := mustJSON(t, map[string]any{"revision": 0, "todoText": text})
			_, _ = (TodoWriteTool{}).Execute(args) // most will fail stale-revision; that is fine
		}(i)
	}
	wg.Wait()

	// The file must parse cleanly (no interleaved/partial content) regardless
	// of how many writes landed.
	raw := readTodoFileRaw(t, "ses-concurrent")
	if _, _, err := parseTodoDoc(raw); err != nil {
		t.Fatalf("concurrent writes produced an unparseable file: %v\n%s", err, raw)
	}
}

// --- Task 3: session separation, no-disk-I/O read path, reset semantics ---

func TestTodoTwoSessionsKeepSeparateLists(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-a")
	writeTodoText(t, "- [ ] a-only")
	SetTodoSession("ses-b")
	if got := readTodoText(t); strings.Contains(got, "a-only") {
		t.Fatalf("session b saw session a's list: %q", got)
	}
	writeTodoText(t, "- [ ] b-only")

	// Switching back reloads session a's file.
	SetTodoSession("ses-a")
	if got := readTodoText(t); !strings.Contains(got, "a-only") {
		t.Fatalf("session a list lost after switching away: %q", got)
	}
}

func TestTodoStateNoDiskIO(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-nodisk")
	writeTodoText(t, "- [ ] something")

	// Point the store at a path that would fail loudly if touched, then
	// confirm TodoState still serves the in-memory copy.
	dir := todoDir()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if got := TodoState(); !strings.Contains(got, "something") {
		t.Fatalf("TodoState must serve memory without disk I/O, got %q", got)
	}
}

func TestTodoResetTodoStateKeepsFileOnDisk(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-reset")
	writeTodoText(t, "- [ ] durable")

	if !todoFileExists("ses-reset") {
		t.Fatal("expected file to exist after write")
	}
	ResetTodoState()

	// Memory cleared: todoread reports the empty state.
	if got := readTodoText(t); !strings.Contains(got, "No todo list found") {
		t.Fatalf("expected cleared read output, got %q", got)
	}
	// File survives and reloads intact on resume.
	if !todoFileExists("ses-reset") {
		t.Fatal("ResetTodoState must not delete the file")
	}
	SetTodoSession("ses-reset")
	if got := readTodoText(t); !strings.Contains(got, "durable") {
		t.Fatalf("expected list to reload from file on resume, got %q", got)
	}
}

// --- Task 4: revision protocol + todo_update ---

func TestTodoStaleRevisionRejected(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-stale")
	writeTodoText(t, "- [ ] first") // writes revision 1

	// Cite revision 0 against a file at revision 1 → rejected with content.
	args := mustJSON(t, map[string]any{"revision": 0, "todoText": "- [ ] replacement"})
	_, err := (TodoWriteTool{}).Execute(args)
	if err == nil {
		t.Fatal("expected stale revision to be rejected")
	}
	if !strings.Contains(err.Error(), "stale revision") || !strings.Contains(err.Error(), "first") {
		t.Fatalf("expected stale-revision error carrying current content, got %v", err)
	}
}

func TestTodoUpdateTargetedStatusChange(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-update")
	writeTodoText(t, "- [ ] first\n- [ ] second")
	before := readTodoFileRaw(t, "ses-update")

	args := mustJSON(t, map[string]any{
		"revision": 1, "op": "set_status", "item_id": "t1", "status": "done",
	})
	if _, err := (TodoUpdateTool{}).Execute(args); err != nil {
		t.Fatalf("todo_update: %v", err)
	}
	after := readTodoText(t)
	if !strings.Contains(after, "- [✓] t1 first") || !strings.Contains(after, "- [ ] t2 second") {
		t.Fatalf("targeted status change wrong: %q", after)
	}
	// All other items byte-identical.
	if strings.Count(after, "- [ ] t2 second") != 1 {
		t.Fatalf("non-targeted item changed: %q", after)
	}
	_ = before
}

// --- Task 5: destructive full-replace guard ---

func TestTodoWriteDroppingCompletedItemRejected(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-guard")
	writeTodoText(t, "- [ ] pending\n- [✓] done")

	args := mustJSON(t, map[string]any{"revision": 1, "todoText": "- [ ] pending"})
	_, err := (TodoWriteTool{}).Execute(args)
	if err == nil {
		t.Fatal("expected todowrite dropping a completed item to be rejected")
	}
	if !strings.Contains(err.Error(), "todo_update") {
		t.Fatalf("expected rejection to point at todo_update, got %v", err)
	}
}

func TestTodoWriteFirstWriteAndAppendAccepted(t *testing.T) {
	chdirTemp(t)
	SetTodoSession("ses-append")

	// Legitimate first write (no file yet).
	if _, err := todoWrite(0, "- [ ] one", nil, ""); err != nil {
		t.Fatalf("first write rejected: %v", err)
	}
	// Genuine append: same items plus a new one.
	if _, err := todoWrite(1, "- [ ] one\n- [•] two", nil, ""); err != nil {
		t.Fatalf("append rejected: %v", err)
	}
	got := readTodoText(t)
	if !strings.Contains(got, "- [ ] t1 one") || !strings.Contains(got, "- [•] t2 two") {
		t.Fatalf("append result wrong: %q", got)
	}
}

// --- Task 8: snapshot store captures todo writes ---

func TestTodoWriteCapturedBySnapshotStore(t *testing.T) {
	chdirTemp(t)

	store := snapshot.NewStore("test-agent", filepath.Join(t.TempDir(), "snapshots"))
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "call-todo-write-1")

	SetTodoSession("ses-snap")
	// First write — file is new (no backup bytes needed), but the snapshot
	// record is still created.
	args := mustJSON(t, map[string]any{"revision": 0, "todoText": "- [ ] one"})
	if _, err := (TodoWriteTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Second write — must produce a backup of the prior content.
	_ = store
	store.AdvanceStep()
	prior := readTodoFileRaw(t, "ses-snap")
	if !strings.Contains(prior, "t1 one") {
		t.Fatalf("first write did not land: %q", prior)
	}
	ctx2 := snapshot.WithToolCallID(ctx, "call-todo-write-2")
	args2 := mustJSON(t, map[string]any{"revision": 1, "todoText": "- [ ] one\n- [•] two"})
	if _, err := (TodoWriteTool{}).ExecuteCtx(ctx2, args2); err != nil {
		t.Fatalf("second write: %v", err)
	}

	restored, err := store.UndoByToolCallID("call-todo-write-2", 5)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	path := todoFilePath("ses-snap")
	var found bool
	for _, p := range restored {
		if p == path {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected undo to restore %s, got %v", path, restored)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "t1 one") || strings.Contains(string(after), "t2 two") {
		t.Fatalf("undo did not restore prior single-item state: %q", string(after))
	}
}
