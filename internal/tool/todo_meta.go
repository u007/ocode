package tool

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/snapshot"
)

// Optional per-item detail ("continuation lines") for the todo file grammar.
// Each line is two spaces + "key: value", directly under its item, every key
// at most once, serialized in this fixed order:
//
//	- [✓] t3 Third item
//	  deps: t1, t2
//	  started: 2026-10-28T20:41:02+08:00
//	  finished: 2026-10-28T20:44:10+08:00
//	  run: agent-run-4
//	  result: Added the parser and 6 tests.
//
// Unknown keys and malformed values are parse errors (strict parse, never
// silent reset). Files with no continuation lines are the legacy grammar and
// parse unchanged.

var todoWrittenHooks struct {
	mu  sync.Mutex
	fns []func(TodoScope)
}

// OnTodoWritten registers fn to run after every successful todo file write
// (todowrite, todo_update and the task link write-back alike). fn runs on the
// writer's goroutine after every todo lock is released, so it must be cheap and
// must not block; hand slow work to another goroutine.
func OnTodoWritten(fn func(scope TodoScope)) {
	todoWrittenHooks.mu.Lock()
	defer todoWrittenHooks.mu.Unlock()
	todoWrittenHooks.fns = append(todoWrittenHooks.fns, fn)
}

func notifyTodoWritten(scope TodoScope) {
	todoWrittenHooks.mu.Lock()
	fns := slices.Clone(todoWrittenHooks.fns)
	todoWrittenHooks.mu.Unlock()
	for _, fn := range fns {
		fn(scope)
	}
}

// todoNow is the clock used to stamp started/finished; tests replace it.
var todoNow = time.Now

// todoResultMax caps a result line, in runes including the trailing "…".
const todoResultMax = 300

var todoDetailRe = regexp.MustCompile(`^  ([a-z]+): (.+)$`)

func isTodoDetailLine(line string) bool { return strings.HasPrefix(line, " ") }

// applyTodoDetail parses one continuation line into it.
func applyTodoDetail(it *todoItem, line string) error {
	m := todoDetailRe.FindStringSubmatch(line)
	if m == nil {
		return fmt.Errorf("line %q: malformed detail line (want two spaces + \"key: value\" directly under an item)", line)
	}
	key, val := m[1], strings.TrimSpace(m[2])
	dup := func() error { return fmt.Errorf("line %q: duplicate %q for item %s", line, key, it.id) }
	switch key {
	case "deps":
		if it.deps != nil {
			return dup()
		}
		seen := map[string]bool{}
		for _, d := range strings.Split(val, ",") {
			d = strings.TrimSpace(d)
			if !todoIDRe.MatchString(d) {
				return fmt.Errorf("line %q: deps want comma-separated item ids like t1, got %q", line, d)
			}
			if seen[d] {
				return fmt.Errorf("line %q: dep %s listed twice", line, d)
			}
			seen[d] = true
			it.deps = append(it.deps, d)
		}
	case "started", "finished":
		if _, err := time.Parse(time.RFC3339, val); err != nil {
			return fmt.Errorf("line %q: %s wants an RFC3339 timestamp: %v", line, key, err)
		}
		dst := &it.started
		if key == "finished" {
			dst = &it.finished
		}
		if *dst != "" {
			return dup()
		}
		*dst = val
	case "run":
		if it.run != "" {
			return dup()
		}
		it.run = val
	case "result":
		if it.result != "" {
			return dup()
		}
		it.result = val
	default:
		return fmt.Errorf("line %q: unknown detail key %q (want deps, started, finished, run, result)", line, key)
	}
	return nil
}

// writeTodoItem renders one item. detail adds the continuation lines.
func writeTodoItem(b *strings.Builder, it todoItem, detail bool) {
	fmt.Fprintf(b, "- [%s] %s %s\n", it.status, it.id, it.text)
	if !detail {
		return
	}
	if len(it.deps) > 0 {
		fmt.Fprintf(b, "  deps: %s\n", strings.Join(it.deps, ", "))
	}
	if it.started != "" {
		fmt.Fprintf(b, "  started: %s\n", it.started)
	}
	if it.finished != "" {
		fmt.Fprintf(b, "  finished: %s\n", it.finished)
	}
	if it.run != "" {
		fmt.Fprintf(b, "  run: %s\n", it.run)
	}
	if it.result != "" {
		fmt.Fprintf(b, "  result: %s\n", it.result)
	}
}

// dropTodoDetail removes continuation lines from rendered todo text. The TUI
// sidebar shows item lines only; the file keeps the detail. An open item whose
// dependencies are not done keeps its line and gains a "(blocked) " marker right after its id, where a clamped row keeps it.
func dropTodoDetail(s string) string {
	lines := strings.Split(s, "\n")
	done := map[string]bool{}
	for _, l := range lines {
		if m := todoSidebarItemRe.FindStringSubmatch(l); m != nil {
			done[m[2]] = m[1] == "✓"
		}
	}
	out := make([]string, 0, len(lines))
	item := -1 // index in out of the item line the detail lines belong to
	for _, l := range lines {
		switch {
		case !isTodoDetailLine(l):
			out = append(out, l)
			item = -1
			if todoSidebarItemRe.MatchString(l) && !strings.HasPrefix(l, "- [✓]") {
				item = len(out) - 1
			}
		case item >= 0 && strings.HasPrefix(l, "  deps: "):
			for _, d := range strings.Split(strings.TrimPrefix(l, "  deps: "), ",") {
				if id := strings.TrimSpace(d); !done[id] {
					out[item] = todoSidebarItemRe.ReplaceAllString(out[item], "${0}(blocked) ")
					break
				}
			}
		}
	}
	return strings.Join(out, "\n")
}

var todoSidebarItemRe = regexp.MustCompile(`^- \[(.)\] (t\d+) `)

// serializeItemsForAnchor renders the re-anchor view: the item lines only, so
// the text stays valid todowrite grammar if a model echoes it back.
func serializeItemsForAnchor(items []todoItem) string {
	var b strings.Builder
	for _, it := range items {
		writeTodoItem(&b, it, false)
	}
	return b.String()
}

// blockedTodoNotes lists, as "t3 by t1, t2", every open item whose
// dependencies are not all done, and returns the blocked ids separately so
// callers that need structured state never parse the display string. They
// travel beside the item lines, never in them, because a "blocked by" line is
// not part of the plan grammar.
func blockedTodoNotes(items []todoItem) (notes []string, ids []string) {
	for i, it := range items {
		if it.status == "✓" {
			continue
		}
		if open := openTodoDeps(items, i); len(open) > 0 {
			notes = append(notes, fmt.Sprintf("%s by %s", it.id, strings.Join(open, ", ")))
			ids = append(ids, it.id)
		}
	}
	return notes, ids
}

// openTodoDeps lists the dependencies of items[idx] that are not done.
func openTodoDeps(items []todoItem, idx int) []string {
	var open []string
	for _, d := range items[idx].deps {
		for _, o := range items {
			if o.id == d && o.status != "✓" {
				open = append(open, d)
			}
		}
	}
	return open
}

// validateTodoDeps rejects dependencies on missing items, self-references and
// cycles. Every id involved is named in the error.
func validateTodoDeps(items []todoItem) error {
	byID := make(map[string]int, len(items))
	for i, it := range items {
		byID[it.id] = i
	}
	for _, it := range items {
		var missing []string
		for _, d := range it.deps {
			if d == it.id {
				return fmt.Errorf("todo deps rejected: %s cannot depend on itself", it.id)
			}
			if _, ok := byID[d]; !ok {
				missing = append(missing, d)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("todo deps rejected: %s depends on unknown item(s) %s", it.id, strings.Join(missing, ", "))
		}
	}
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(items))
	var path []string
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case done:
			return nil
		case visiting:
			cycle := append(append([]string(nil), path...), id)
			for i, p := range cycle {
				if p == id {
					cycle = cycle[i:]
					break
				}
			}
			return fmt.Errorf("todo deps rejected: dependency cycle %s", strings.Join(cycle, " -> "))
		}
		state[id] = visiting
		path = append(path, id)
		for _, d := range items[byID[id]].deps {
			if err := visit(d); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[id] = done
		return nil
	}
	for _, it := range items {
		if err := visit(it.id); err != nil {
			return err
		}
	}
	return nil
}

// checkTodoNotBlocked rejects moving items[idx] to in_progress or done while a
// dependency is not done.
func checkTodoNotBlocked(items []todoItem, idx int) error {
	if open := openTodoDeps(items, idx); len(open) > 0 {
		return fmt.Errorf("%s is blocked by %s", items[idx].id, strings.Join(open, ", "))
	}
	return nil
}

// stampTodoStatus stamps started/finished for the status items[idx] now has.
// Stamps are written once and never cleared.
func stampTodoStatus(it *todoItem) {
	switch it.status {
	case "•":
		if it.started == "" {
			it.started = todoNow().Format(time.RFC3339)
		}
	case "✓":
		if it.finished == "" {
			it.finished = todoNow().Format(time.RFC3339)
		}
	}
}

// setTodoStatus is the one status-change path for targeted updates: it refuses
// a blocked in_progress/done transition and stamps the timestamps.
func setTodoStatus(items []todoItem, idx int, canonStatus string) error {
	if canonStatus != " " {
		if err := checkTodoNotBlocked(items, idx); err != nil {
			return err
		}
	}
	items[idx].status = canonStatus
	stampTodoStatus(&items[idx])
	return nil
}

// cleanTodoResult reduces text to one line of at most todoResultMax runes
// (the last being "…" when truncated).
func cleanTodoResult(text string) string {
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	r := []rune(line)
	if len(r) > todoResultMax {
		return string(r[:todoResultMax-1]) + "…"
	}
	return line
}

// modelTodoResult validates result text written by the model: single line.
func modelTodoResult(text string) (string, error) {
	text = strings.TrimSpace(text)
	if strings.ContainsAny(text, "\r\n") {
		return "", fmt.Errorf("todo result must be a single line")
	}
	return text, nil
}

// findTodoItem returns the index of id in items, or -1.
func findTodoItem(items []todoItem, id string) int {
	for i, it := range items {
		if it.id == id {
			return i
		}
	}
	return -1
}

// checkRewriteDeps runs before ids are assigned in a todowrite: deps may only
// name ids the list already has (echoed or inherited). Ids for new items are
// assigned afterwards, so a dep on one would silently bind to whichever item
// happens to receive that id.
func checkRewriteDeps(items []todoItem) error {
	have := map[string]bool{}
	for _, it := range items {
		if it.id != "" {
			have[it.id] = true
		}
	}
	for _, it := range items {
		for _, d := range it.deps {
			if !have[d] {
				return fmt.Errorf("todowrite rejected: item %q depends on %s, which is not an id in the list. todowrite assigns ids to new items itself, so a dep cannot name an item that does not exist yet; write the list first, then use todo_update set_deps", it.text, d)
			}
		}
	}
	return nil
}

// mergeTodoDetail copies the continuation fields of existing items onto the
// incoming items that echo their id and leave the field unset, so a rewrite
// that only rewords or reorders does not erase deps, timestamps, run or result.
// Must run after inheritTodoIDs.
func mergeTodoDetail(existing, newItems []todoItem) {
	for i := range newItems {
		j := findTodoItem(existing, newItems[i].id)
		if newItems[i].id == "" || j < 0 {
			continue
		}
		old, it := existing[j], &newItems[i]
		if it.deps == nil {
			it.deps = old.deps
		}
		if it.started == "" {
			it.started = old.started
		}
		if it.finished == "" {
			it.finished = old.finished
		}
		if it.run == "" {
			it.run = old.run
		}
		if it.result == "" {
			it.result = old.result
		}
	}
}

// applyRewriteTransitions applies the status-transition rules to a rewrite:
// every item whose status differs from its existing one (a new item counts as
// pending before) is checked for blocking and stamped. Unchanged items are left
// alone so an unrelated rewrite of an already-inconsistent item still succeeds.
func applyRewriteTransitions(existing, newItems []todoItem) error {
	for i := range newItems {
		prev := " "
		if j := findTodoItem(existing, newItems[i].id); j >= 0 {
			prev = existing[j].status
		}
		if newItems[i].status == prev {
			continue
		}
		if newItems[i].status != " " {
			if err := checkTodoNotBlocked(newItems, i); err != nil {
				return err
			}
		}
		stampTodoStatus(&newItems[i])
	}
	return nil
}

// --- system-initiated mutations (used by the task tool, not by the model) ---

// TodoRunOutcome is how a linked sub-agent run ended.
type TodoRunOutcome int

const (
	TodoRunDone TodoRunOutcome = iota
	TodoRunFailed
	TodoRunCancelled
)

// systemMutateTodo records what the harness did to the plan. It takes the same
// flock as the model-facing mutations and re-reads, applies, writes and bumps
// the revision, but cites no revision: the caller is not acting on a read of
// the plan. It is not a second writer: the task tool calls it from the main
// agent's own tool call (TaskTool refuses todo_item inside a sub-agent), so
// writes stay serialized with the model's by the lock, and a model that read
// the plan earlier gets a stale-revision rejection on its next write, as for
// any other change.
func systemMutateTodo(scope TodoScope, apply func(items []todoItem) error) (int, error) {
	return mutateTodo(scope, nil, "todo update", nil, "", func(items []todoItem) ([]todoItem, error) {
		return items, apply(items)
	})
}

// CheckTodoItemDispatchable reports whether itemID exists and is not blocked.
// Read-only: the task tool calls it before spawning anything. A done item is
// refused too.
func CheckTodoItemDispatchable(scope TodoScope, itemID string) error {
	_, items, exists, err := loadTodo(scope)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("todo_item %s: this session has no todo plan", itemID)
	}
	idx := findTodoItem(items, itemID)
	if idx < 0 {
		return fmt.Errorf("todo_item %s: no such item in the plan", itemID)
	}
	// A fresh dispatch must not silently re-open finished work. Resume does not
	// come through here: it links through LinkTodoItemToRun directly, and a
	// resume may legitimately reopen the item its own run completed.
	if items[idx].status == "✓" {
		return fmt.Errorf("todo_item %s is already done; pick an open item or append a new one", itemID)
	}
	return checkTodoNotBlocked(items, idx)
}

// LinkTodoItemToRun marks itemID in_progress, records runID and stamps
// started. It returns the plan's new revision.
func LinkTodoItemToRun(scope TodoScope, itemID, runID string) (int, error) {
	return systemMutateTodo(scope, func(items []todoItem) error {
		idx := findTodoItem(items, itemID)
		if idx < 0 {
			return fmt.Errorf("todo_item %s: no such item in the plan", itemID)
		}
		if err := setTodoStatus(items, idx, "•"); err != nil {
			return err
		}
		items[idx].run = runID
		items[idx].result = ""
		return nil
	})
}

// FinishTodoItemRun writes the first line of text back as the item's result.
// A completed run also marks the item done and stamps finished; a failed or
// cancelled one leaves it in_progress with a FAILED:/CANCELLED: prefix so the
// main agent decides what to do. It refuses when the item has since been
// linked to a different run.
func FinishTodoItemRun(scope TodoScope, itemID, runID string, outcome TodoRunOutcome, text string) (int, error) {
	line := cleanTodoResult(text)
	switch outcome {
	case TodoRunFailed:
		line = cleanTodoResult("FAILED: " + line)
	case TodoRunCancelled:
		line = cleanTodoResult("CANCELLED: " + line)
	}
	return systemMutateTodo(scope, func(items []todoItem) error {
		idx := findTodoItem(items, itemID)
		if idx < 0 {
			return fmt.Errorf("todo_item %s: no such item in the plan", itemID)
		}
		if items[idx].run != runID {
			return fmt.Errorf("todo_item %s is now linked to run %q, not %s", itemID, items[idx].run, runID)
		}
		items[idx].result = line
		if outcome == TodoRunDone {
			return setTodoStatus(items, idx, "✓")
		}
		return nil
	})
}

// mutateTodo is the shared lock → re-read → verify → apply → validate → write
// sequence of the targeted mutations. cited == nil skips the revision check
// (system mutations). It returns the new revision.
func mutateTodo(scope TodoScope, cited *int, what string, store *snapshot.Store, tcID string, apply func(items []todoItem) ([]todoItem, error)) (int, error) {
	wrote := false
	// Registered before the lock so it runs after the unlock.
	defer func() {
		if wrote {
			notifyTodoWritten(scope)
		}
	}()
	todoStoreState.mu.Lock()
	defer todoStoreState.mu.Unlock()
	root, cur := scope.ProjectRoot, scope.SessionID

	// Ensure the lock's directory exists before opening the lock file.
	if err := os.MkdirAll(todoDir(root), 0o755); err != nil {
		return 0, err
	}

	// One lock hold for read → verify revision → apply → write: a lock released
	// between the read and the write does not prevent two processes from both
	// writing revision N+1.
	var newRev int
	err := filelock.WithFileLock(todoLockPath(root), func() error {
		fileRev, fileItems, fileExists, rerr := readTodoFile(root, cur)
		if rerr != nil {
			return rerr
		}
		if cited != nil {
			if fileExists && fileRev != *cited {
				return staleTodoError(cur, *cited, fileRev, fileItems)
			}
			if !fileExists && *cited != 0 {
				return fmt.Errorf("%s rejected: no todo file exists for this session yet (cited revision %d); run todoread to get the current state", what, *cited)
			}
		}

		items := baseItems(cur, fileExists, fileItems)
		assignTodoIDs(items)
		items, aerr := apply(items)
		if aerr != nil {
			return aerr
		}
		if verr := validateTodoDeps(items); verr != nil {
			return verr
		}

		newRev = fileRev + 1
		if !fileExists {
			newRev = 1
		}
		if werr := writeTodoFile(root, cur, newRev, items, store, tcID); werr != nil {
			return werr
		}
		todoStoreState.sessions[cur] = &todoSession{
			revision: newRev,
			content:  serializeTodo(newRev, items),
		}
		wrote = true
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newRev, nil
}

// parseTodoDepsInput validates a deps list given to todo_update: each entry
// must be an item id and none may repeat. Existence, self-reference and cycles
// are checked by validateTodoDeps on the resulting list.
func parseTodoDepsInput(deps []string) ([]string, error) {
	seen := map[string]bool{}
	for _, d := range deps {
		if !todoIDRe.MatchString(d) {
			return nil, fmt.Errorf("todo_update: dep %q is not an item id like t1", d)
		}
		if seen[d] {
			return nil, fmt.Errorf("todo_update: dep %s listed twice", d)
		}
		seen[d] = true
	}
	if len(deps) == 0 {
		return nil, nil
	}
	return deps, nil
}
