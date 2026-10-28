package tool

import (
	"strings"
	"testing"
	"time"
)

const (
	t1Stamp = "2026-10-28T20:40:17+08:00"
	t2Stamp = "2026-10-28T20:44:10+08:00"
)

// fixedTodoClock freezes todoNow to the given RFC3339 stamps in turn; the last
// stamp repeats.
func fixedTodoClock(t *testing.T, stamps ...string) {
	t.Helper()
	orig := todoNow
	i := 0
	todoNow = func() time.Time {
		s := stamps[min(i, len(stamps)-1)]
		i++
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	t.Cleanup(func() { todoNow = orig })
}

func updateOp(t *testing.T, sid string, args todoOpArgs) (string, error) {
	t.Helper()
	rev, _, _, err := loadTodo(todoScopeFor(sid))
	if err != nil {
		t.Fatal(err)
	}
	return todoUpdate(todoScopeFor(sid), rev, args, nil, "")
}

func mustUpdate(t *testing.T, sid string, args todoOpArgs) {
	t.Helper()
	if _, err := updateOp(t, sid, args); err != nil {
		t.Fatalf("todo_update %+v: %v", args, err)
	}
}

func readItems(t *testing.T, sid string) []todoItem {
	t.Helper()
	_, items, _, err := loadTodo(todoScopeFor(sid))
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func seedPlan(t *testing.T, sid, text string) {
	t.Helper()
	writeTodoText(t, sid, text)
}

func TestTodoDetailParseRoundTrip(t *testing.T) {
	doc := "# Todo (revision 3)\n\n" +
		"- [ ] t1 First item\n" +
		"- [•] t2 Second item\n" +
		"  deps: t1\n" +
		"  started: " + t1Stamp + "\n" +
		"  run: agent-run-3\n" +
		"- [✓] t3 Third item\n" +
		"  deps: t1, t2\n" +
		"  started: " + t1Stamp + "\n" +
		"  finished: " + t2Stamp + "\n" +
		"  run: agent-run-4\n" +
		"  result: Added the parser and 6 tests; go test ./internal/tool passes.\n"
	rev, items, err := parseTodoDoc(doc)
	if err != nil {
		t.Fatal(err)
	}
	if rev != 3 || len(items) != 3 {
		t.Fatalf("rev=%d items=%d", rev, len(items))
	}
	it := items[2]
	if strings.Join(it.deps, ",") != "t1,t2" || it.started != t1Stamp || it.finished != t2Stamp || it.run != "agent-run-4" || !strings.HasPrefix(it.result, "Added the parser") {
		t.Fatalf("t3 parsed wrong: %+v", it)
	}
	if got := serializeTodo(rev, items); got != doc {
		t.Fatalf("round trip differs:\n%s\nvs\n%s", got, doc)
	}
}

func TestTodoDetailLegacyFileParses(t *testing.T) {
	_, items, err := parseTodoDoc("# Todo (revision 1)\n\n- [ ] t1 a\n- [✓] t2 b\n")
	if err != nil || len(items) != 2 || items[0].deps != nil || items[1].result != "" {
		t.Fatalf("legacy: %v %+v", err, items)
	}
}

func TestTodoDetailMalformedRejected(t *testing.T) {
	head := "# Todo (revision 1)\n\n- [ ] t1 a\n"
	cases := map[string]string{
		"unknown key":      head + "  owner: me\n",
		"bad timestamp":    head + "  started: yesterday\n",
		"bad dep id":       head + "  deps: first\n",
		"duplicate dep":    head + "  deps: t2, t2\n",
		"duplicate key":    head + "  run: a\n  run: b\n",
		"empty value":      head + "  result:\n",
		"one space indent": head + " run: a\n",
		"no colon":         head + "  run a\n",
	}
	for name, doc := range cases {
		if _, _, err := parseTodoDoc(doc); err == nil {
			t.Errorf("%s: parse succeeded, want error", name)
		}
	}
}

func TestTodoDetailBeforeFirstItemRejected(t *testing.T) {
	_, _, err := parseTodoDoc("# Todo (revision 1)\n\n  run: agent-run-1\n- [ ] t1 a\n")
	if err == nil || !strings.Contains(err.Error(), "before any item") {
		t.Fatalf("err = %v", err)
	}
}

func TestTodoDepsRejectedOnWrite(t *testing.T) {
	chdirTemp(t)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n- [ ] c\n")

	if _, err := updateOp(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t1", Deps: &[]string{"t9"}}); err == nil || !strings.Contains(err.Error(), "t9") {
		t.Errorf("missing id: %v", err)
	}
	if _, err := updateOp(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t1", Deps: &[]string{"t1"}}); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Errorf("self: %v", err)
	}
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t1", Deps: &[]string{"t2"}})
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t2", Deps: &[]string{"t3"}})
	_, err := updateOp(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t3", Deps: &[]string{"t1"}})
	if err == nil || !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), "t1") {
		t.Errorf("cycle: %v", err)
	}
	if _, err := updateOp(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t3"}); err == nil {
		t.Error("set_deps without deps must be an error")
	}
	// A rejected write leaves the file untouched.
	if got := readItems(t, "s"); len(got[2].deps) != 0 {
		t.Errorf("rejected write changed t3: %+v", got[2])
	}
}

func TestTodoBlockedStatusTransitions(t *testing.T) {
	chdirTemp(t)
	fixedTodoClock(t, t1Stamp, t2Stamp)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n- [ ] c\n")
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t3", Deps: &[]string{"t1", "t2"}})

	for _, st := range []string{"in_progress", "done"} {
		_, err := updateOp(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t3", Status: st})
		if err == nil || !strings.Contains(err.Error(), "t3 is blocked by t1, t2") {
			t.Errorf("%s: err = %v", st, err)
		}
	}
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t3", Status: "pending"})

	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t1", Status: "done"})
	if _, err := updateOp(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t3", Status: "in_progress"}); err == nil || !strings.Contains(err.Error(), "t3 is blocked by t2") {
		t.Errorf("partly unblocked: %v", err)
	}
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t2", Status: "done"})
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t3", Status: "in_progress"})
}

func TestTodoTimestampsStampedOnce(t *testing.T) {
	chdirTemp(t)
	fixedTodoClock(t, t1Stamp, t2Stamp, "2030-01-01T00:00:00Z")
	seedPlan(t, "s", "- [ ] a\n")

	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t1", Status: "in_progress"})
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t1", Status: "done"})
	// Back to pending and forward again: neither stamp moves or clears.
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t1", Status: "pending"})
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t1", Status: "in_progress"})
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t1", Status: "done"})

	it := readItems(t, "s")[0]
	if it.started != t1Stamp || it.finished != t2Stamp {
		t.Fatalf("started=%q finished=%q", it.started, it.finished)
	}
}

func TestTodoSetDepsSetResultAndStatusResult(t *testing.T) {
	chdirTemp(t)
	fixedTodoClock(t, t1Stamp)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n")

	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t2", Deps: &[]string{"t1"}})
	if got := readItems(t, "s")[1].deps; strings.Join(got, ",") != "t1" {
		t.Fatalf("deps = %v", got)
	}
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t2", Deps: &[]string{}})
	if got := readItems(t, "s")[1].deps; got != nil {
		t.Fatalf("empty deps must clear, got %v", got)
	}

	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetResult, ItemID: "t1", Text: "  parser added  "})
	if got := readItems(t, "s")[0].result; got != "parser added" {
		t.Fatalf("result = %q", got)
	}
	if _, err := updateOp(t, "s", todoOpArgs{Op: todoOpSetResult, ItemID: "t1", Text: "two\nlines"}); err == nil {
		t.Error("multi-line result must be rejected")
	}

	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t2", Status: "done", Result: "shipped"})
	it := readItems(t, "s")[1]
	if it.status != "✓" || it.result != "shipped" || it.finished != t1Stamp {
		t.Fatalf("t2 = %+v", it)
	}
}

func TestTodoRewriteKeepsDetailOfEchoedItems(t *testing.T) {
	chdirTemp(t)
	fixedTodoClock(t, t1Stamp)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n")
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t2", Deps: &[]string{"t1"}})
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetResult, ItemID: "t2", Text: "kept"})

	// Rewrite by id (reworded) and by text (reordered, id-less): both keep detail.
	rewrite := "- [ ] t2 b reworded\n- [ ] a\n"
	rev, _, _, _ := loadTodo(todoScopeFor("s"))
	if _, err := todoWrite(todoScopeFor("s"), rev, rewrite, nil, ""); err != nil {
		t.Fatal(err)
	}
	items := readItems(t, "s")
	if items[0].id != "t2" || strings.Join(items[0].deps, ",") != "t1" || items[0].result != "kept" {
		t.Fatalf("detail lost: %+v", items[0])
	}

	// An explicit field in the rewrite wins over the stored one.
	rev, _, _, _ = loadTodo(todoScopeFor("s"))
	if _, err := todoWrite(todoScopeFor("s"), rev, "- [ ] t2 b reworded\n  result: replaced\n- [ ] a\n", nil, ""); err != nil {
		t.Fatal(err)
	}
	if got := readItems(t, "s")[0].result; got != "replaced" {
		t.Fatalf("result = %q", got)
	}
}

func TestTodoRewriteDepsOnUnassignedIDRejected(t *testing.T) {
	chdirTemp(t)
	_, err := todoWrite(todoScopeFor("s"), 0, "- [ ] a\n- [ ] b\n  deps: t1\n", nil, "")
	if err == nil || !strings.Contains(err.Error(), "assigns ids") {
		t.Fatalf("err = %v", err)
	}
	// With explicit ids both sides are known.
	if _, err := todoWrite(todoScopeFor("s"), 0, "- [ ] t1 a\n- [ ] t2 b\n  deps: t1\n", nil, ""); err != nil {
		t.Fatalf("explicit ids: %v", err)
	}
}

func TestTodoRewriteAppliesStatusRules(t *testing.T) {
	chdirTemp(t)
	fixedTodoClock(t, t1Stamp)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n")
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t2", Deps: &[]string{"t1"}})

	rev, _, _, _ := loadTodo(todoScopeFor("s"))
	if _, err := todoWrite(todoScopeFor("s"), rev, "- [ ] t1 a\n- [✓] t2 b\n", nil, ""); err == nil || !strings.Contains(err.Error(), "t2 is blocked by t1") {
		t.Fatalf("blocked done via rewrite: %v", err)
	}
	if _, err := todoWrite(todoScopeFor("s"), rev, "- [✓] t1 a\n- [✓] t2 b\n", nil, ""); err != nil {
		t.Fatal(err)
	}
	items := readItems(t, "s")
	if items[0].finished != t1Stamp || items[1].finished != t1Stamp {
		t.Fatalf("finished not stamped: %+v", items)
	}
}

func TestTodoSnapshotAnchorShowsBlockedBy(t *testing.T) {
	chdirTemp(t)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n")
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t2", Deps: &[]string{"t1"}})
	snap := ReadTodoSnapshot(todoScopeFor("s"))
	if !snap.HasOpenItems || len(snap.Blocked) != 1 || snap.Blocked[0] != "t2 by t1" {
		t.Fatalf("blocked = %v", snap.Blocked)
	}
	if strings.Contains(snap.Content, "blocked by") || strings.Contains(snap.Content, "deps:") {
		t.Fatalf("anchor must not carry raw detail lines:\n%s", snap.Content)
	}
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t1", Status: "done"})
	if snap = ReadTodoSnapshot(todoScopeFor("s")); len(snap.Blocked) != 0 {
		t.Fatalf("unblocked item still noted: %v", snap.Blocked)
	}
}

func TestTodoSidebarStateDropsDetailLines(t *testing.T) {
	chdirTemp(t)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n")
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t2", Deps: &[]string{"t1"}})
	SetTodoSession("s")
	if got := TodoState(); strings.Contains(got, "deps:") || !strings.Contains(got, "t2 (blocked) b") || strings.Contains(got, "t1 (blocked)") {
		t.Fatalf("TodoState:\n%s", got)
	}
}

func TestTodoSystemLinkAndFinish(t *testing.T) {
	chdirTemp(t)
	fixedTodoClock(t, t1Stamp, t2Stamp)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n")
	scope := todoScopeFor("s")
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetDeps, ItemID: "t2", Deps: &[]string{"t1"}})

	if err := CheckTodoItemDispatchable(scope, "t2"); err == nil || !strings.Contains(err.Error(), "blocked by t1") {
		t.Fatalf("blocked item dispatchable: %v", err)
	}
	if err := CheckTodoItemDispatchable(scope, "t9"); err == nil {
		t.Fatal("unknown item dispatchable")
	}

	rev, err := LinkTodoItemToRun(scope, "t1", "agent-run-1")
	if err != nil {
		t.Fatal(err)
	}
	it := readItems(t, "s")[0]
	if it.status != "•" || it.run != "agent-run-1" || it.started != t1Stamp {
		t.Fatalf("after link: %+v", it)
	}

	long := "first line " + strings.Repeat("é", 400) + "\nsecond line"
	rev2, err := FinishTodoItemRun(scope, "t1", "agent-run-1", TodoRunDone, "\n"+long)
	if err != nil || rev2 != rev+1 {
		t.Fatalf("finish: rev=%d err=%v", rev2, err)
	}
	it = readItems(t, "s")[0]
	if it.status != "✓" || it.finished != t2Stamp || len([]rune(it.result)) != todoResultMax || !strings.HasSuffix(it.result, "…") || strings.Contains(it.result, "second") {
		t.Fatalf("after finish: %+v", it)
	}
}

func TestTodoSystemFinishFailedAndCancelledLeaveInProgress(t *testing.T) {
	chdirTemp(t)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n")
	scope := todoScopeFor("s")
	for _, tc := range []struct {
		id      string
		outcome TodoRunOutcome
		prefix  string
	}{{"t1", TodoRunFailed, "FAILED: boom"}, {"t2", TodoRunCancelled, "CANCELLED: stopped"}} {
		if _, err := LinkTodoItemToRun(scope, tc.id, "agent-run-"+tc.id); err != nil {
			t.Fatal(err)
		}
		text := strings.TrimPrefix(strings.TrimPrefix(tc.prefix, "FAILED: "), "CANCELLED: ")
		if _, err := FinishTodoItemRun(scope, tc.id, "agent-run-"+tc.id, tc.outcome, text); err != nil {
			t.Fatal(err)
		}
		it := readItems(t, "s")[findTodoItem(readItems(t, "s"), tc.id)]
		if it.status != "•" || it.result != tc.prefix || it.finished != "" {
			t.Fatalf("%s: %+v", tc.id, it)
		}
	}
}

func TestTodoSystemFinishRefusesRelinkedItem(t *testing.T) {
	chdirTemp(t)
	seedPlan(t, "s", "- [ ] a\n")
	scope := todoScopeFor("s")
	if _, err := LinkTodoItemToRun(scope, "t1", "agent-run-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := LinkTodoItemToRun(scope, "t1", "agent-run-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := FinishTodoItemRun(scope, "t1", "agent-run-1", TodoRunDone, "late"); err == nil {
		t.Fatal("finish for a superseded run must be refused")
	}
	if it := readItems(t, "s")[0]; it.status != "•" || it.result != "" {
		t.Fatalf("superseded finish changed the item: %+v", it)
	}
}

func TestTodoSystemMutationNeedsNoCitedRevisionButBumpsIt(t *testing.T) {
	chdirTemp(t)
	seedPlan(t, "s", "- [ ] a\n")
	before, _, _, _ := loadTodo(todoScopeFor("s"))
	if _, err := LinkTodoItemToRun(todoScopeFor("s"), "t1", "agent-run-1"); err != nil {
		t.Fatal(err)
	}
	after, _, _, _ := loadTodo(todoScopeFor("s"))
	if after != before+1 {
		t.Fatalf("revision %d -> %d", before, after)
	}
	// The model's stale citation is rejected as for any other change.
	if _, err := todoUpdate(todoScopeFor("s"), before, todoOpArgs{Op: todoOpSetStatus, ItemID: "t1", Status: "done"}, nil, ""); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("err = %v", err)
	}
}

func TestTodoDoneItemNotDispatchable(t *testing.T) {
	chdirTemp(t)
	seedPlan(t, "s", "- [ ] a\n- [ ] b\n")
	mustUpdate(t, "s", todoOpArgs{Op: todoOpSetStatus, ItemID: "t2", Status: "done"})
	err := CheckTodoItemDispatchable(todoScopeFor("s"), "t2")
	if err == nil || err.Error() != "todo_item t2 is already done; pick an open item or append a new one" {
		t.Fatalf("err = %v", err)
	}
	if err := CheckTodoItemDispatchable(todoScopeFor("s"), "t1"); err != nil {
		t.Fatalf("open item refused: %v", err)
	}
}
