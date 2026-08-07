package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/snapshot"
)

// File-backed per-session todo store.
//
// Each session owns one markdown file at <project-root>/.ocode/todo/<session-id>.md
// — the durable, user-visible, git-diffable record of the plan. The in-memory
// copy is a cache of that file: it is only ever populated by (a) loading the
// file on session load/resume, or (b) a mutation that just succeeded in
// writing the file. There is no path that updates memory without updating disk;
// if a write fails, the memory copy is not advanced and the error is returned.
//
// The store keeps one in-memory copy per session id (not a single global
// cursor) so two sessions in one process cannot collide. The current cursor
// only selects which session's copy the render/read path serves.
//
// Concurrency model:
//   - In-process: the store mutex serializes all mutations.
//   - Cross-process (a second ocode instance on the same project, or a resumed
//     session): every mutation takes the advisory file lock, re-reads the file,
//     and verifies the caller's cited revision against the file's revision
//     before applying. A stale revision is rejected with the current content.
//
// Write path (every mutation): lock → re-read → verify revision → apply →
// write to a temp file in the same directory → fsync → atomic rename →
// release. A crash mid-write leaves either the old file or the new one, never
// a half-written one.
//
// Strict parse, never silent reset: if the file fails to parse, writes are
// refused and the parse error (with the file path) is surfaced. The last-good
// file stays on disk for the user to fix or revert.

// Canonical file format:
//
//	# Todo (revision 3)
//
//	- [ ] t1 First item
//	- [•] t2 Second item
//	- [✓] t3 Third item

type todoStore struct {
	mu       sync.RWMutex
	current  string // current session id ("" = none)
	sessions map[string]*todoSession
}

type todoSession struct {
	revision int    // last known revision; 0 = no file yet
	content  string // canonical serialized content (header + items) or legacy raw text
	parseErr error  // sticky parse error from the last file load
}

var todoStoreState = &todoStore{sessions: map[string]*todoSession{}}

// logTodoError reports a non-fatal todo-store failure: what was attempted and
// why it failed. Uses the stdlib logger, which tui.Run redirects into the debug
// panel — never raw stdout/stderr, which would paint over the alt-screen.
func logTodoError(attempted string, err error) {
	log.Printf("[TODO] failed to %s: %v", attempted, err)
}

// todo header regex: the first line of the file carries the revision.
var todoHeaderRe = regexp.MustCompile(`^# Todo \(revision (\d+)\)$`)

// todo item line regex: "- [ ] id text". The id token is parsed separately
// (only a tN token counts as an id — see parseTodoItems).
var todoItemLineRe = regexp.MustCompile(`^- \[(.+?)\]\s*(.*)$`)

// todo id regex: ids are short stable tokens like t1, t2.
var todoIDRe = regexp.MustCompile(`^t\d+$`)

// normalizeTodoMarker maps any accepted input marker to the canonical one.
func normalizeTodoMarker(m string) (string, error) {
	switch strings.TrimSpace(m) {
	case "", "pending", "○":
		return " ", nil
	case "•", "~", "-", "in_progress", "⟳":
		return "•", nil
	case "✓", "x", "X", "completed", "done":
		return "✓", nil
	}
	return "", fmt.Errorf("unknown todo status marker %q (want [ ], [•], [✓])", m)
}

// todoItem is one line of the todo file.
type todoItem struct {
	status string // " ", "•", "✓"
	id     string // stable id, e.g. "t1"; "" when not yet assigned
	text   string
}

// parseTodoItems parses the item body of a todo list (lines after the header).
// requireIDs (the file grammar) demands every item carry a tN id; tool input
// (todowrite) may omit ids, which get assigned by the caller. A line whose
// first token matches tN is treated as id + text; any other line is all text.
func parseTodoItems(body string, requireIDs bool) ([]todoItem, error) {
	var items []todoItem
	seen := map[string]bool{}
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := todoItemLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("line %q: expected \"- [ ] id text\"", line)
		}
		status, err := normalizeTodoMarker(m[1])
		if err != nil {
			return nil, fmt.Errorf("line %q: %v", line, err)
		}
		rest := strings.TrimSpace(m[2])
		var id, text string
		if rest != "" {
			if fields := strings.SplitN(rest, " ", 2); todoIDRe.MatchString(fields[0]) {
				id = fields[0]
				if len(fields) > 1 {
					text = strings.TrimSpace(fields[1])
				}
			} else {
				text = rest
			}
		}
		if requireIDs && !todoIDRe.MatchString(id) {
			return nil, fmt.Errorf("line %q: item is missing a stable id (want \"- [ ] t1 text\")", line)
		}
		if id != "" && seen[id] {
			return nil, fmt.Errorf("line %q: duplicate item id %q", line, id)
		}
		if id != "" {
			seen[id] = true
		}
		items = append(items, todoItem{status: status, id: id, text: text})
	}
	return items, nil
}

// serializeTodo renders the canonical file content for a revision + items.
func serializeTodo(revision int, items []todoItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Todo (revision %d)\n\n", revision)
	for _, it := range items {
		fmt.Fprintf(&b, "- [%s] %s %s\n", it.status, it.id, it.text)
	}
	return b.String()
}

// serializeItems renders just the item lines (no header) — the todoread view.
func serializeItems(items []todoItem) string {
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "- [%s] %s %s\n", it.status, it.id, it.text)
	}
	return b.String()
}

// parseTodoDoc parses full file content (header + items) with the strict
// grammar: the header must be the first non-blank line, every item must carry
// an id.
func parseTodoDoc(content string) (revision int, items []todoItem, err error) {
	lines := strings.Split(content, "\n")
	idx := 0
	for idx < len(lines) && strings.TrimSpace(lines[idx]) == "" {
		idx++
	}
	if idx >= len(lines) {
		return 0, nil, fmt.Errorf("empty todo file: missing \"# Todo (revision N)\" header")
	}
	hm := todoHeaderRe.FindStringSubmatch(strings.TrimSpace(lines[idx]))
	if hm == nil {
		return 0, nil, fmt.Errorf("first line %q is not the \"# Todo (revision N)\" header", strings.TrimSpace(lines[idx]))
	}
	revision, err = strconv.Atoi(hm[1])
	if err != nil || revision < 0 {
		return 0, nil, fmt.Errorf("invalid revision %q in header", hm[1])
	}
	items, err = parseTodoItems(strings.Join(lines[idx+1:], "\n"), true)
	if err != nil {
		return 0, nil, err
	}
	return revision, items, nil
}

// stripTodoHeader removes the "# Todo (revision N)" header line (and the blank
// line after it) for display. Content without a header (legacy metadata
// restore) passes through unchanged.
func stripTodoHeader(content string) string {
	if !strings.HasPrefix(content, "# Todo (revision ") {
		return content
	}
	// Drop everything through the first newline — that is the whole header
	// line. (Trimming the "# Todo (revision " prefix first and then trying to
	// trim the full first line does nothing: after the first trim the string
	// no longer starts with that line, so the second trim never matches and
	// the residue — "3)" — renders above the list on every frame.)
	nl := strings.Index(content, "\n")
	if nl < 0 {
		return ""
	}
	return strings.TrimPrefix(content[nl+1:], "\n")
}

// parseTodoContent parses either canonical file content (header + ids) or
// legacy headerless raw text.
//
// The legacy shape is real and reachable: restoreTodoState in the TUI feeds
// pre-store session metadata (e.g. "- [ ] first") into SetTodoState. Demanding
// the header there would make todoread hard-fail on every call for any resumed
// pre-change session, with no recovery path. Legacy content parses at revision
// 0 with ids assigned on the spot, so the first mutation writes a canonical
// file.
func parseTodoContent(content string) (revision int, items []todoItem, err error) {
	if strings.HasPrefix(strings.TrimSpace(content), "# Todo (revision ") {
		return parseTodoDoc(content)
	}
	items, err = parseTodoItems(content, false)
	if err != nil {
		return 0, nil, err
	}
	assignTodoIDs(items)
	return 0, items, nil
}

// --- path resolution ---

// todoProjectRoot returns the project root that owns .ocode/todo/. It follows
// the TUI's workDir source of truth: config.FindProjectRoot walks up from
// os.Getwd(), which the TUI keeps in sync with m.workDir (the /cd handler
// calls os.Chdir). Falls back to os.Getwd() for non-project (test/temp) dirs.
func todoProjectRoot() string {
	if root := config.FindProjectRoot(); root != "" {
		return root
	}
	d, _ := os.Getwd()
	return d
}

func todoDir() string { return filepath.Join(todoProjectRoot(), ".ocode", "todo") }

func todoLockPath() string { return filepath.Join(todoDir(), ".todo.lock") }

func todoFilePath(sessionID string) string {
	return filepath.Join(todoDir(), sessionID+".md")
}

// readTodoFile loads and strictly parses the file for sessionID. Returns
// (revision, items, false, nil) when the file does not exist yet.
func readTodoFile(sessionID string) (int, []todoItem, bool, error) {
	path := todoFilePath(sessionID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, err
	}
	rev, items, err := parseTodoDoc(string(data))
	if err != nil {
		return 0, nil, true, fmt.Errorf("todo file %s failed to parse: %w", path, err)
	}
	return rev, items, true, nil
}

// writeTodoFile atomically writes the canonical content for sessionID: temp
// file in the same directory, fsync, atomic rename. The snapshot store captures
// the write so undo_file_change can restore it.
func writeTodoFile(sessionID string, revision int, items []todoItem, store *snapshot.Store, tcID string) error {
	dir := todoDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := todoFilePath(sessionID)

	// Snapshot before the write so undo_file_change can restore the prior
	// content. A backup failure does not block the write, but it does mean
	// undo coverage is silently lost for this revision — so it is logged.
	if store != nil {
		if err := store.Backup(path, tcID); err != nil {
			logTodoError("snapshot the todo file before writing (undo_file_change will not cover this revision)", err)
		}
	}

	tmp, err := os.CreateTemp(dir, ".todo-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.WriteString(serializeTodo(revision, items)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// Register the successful write in the snapshot store so undo_file_change
	// can resolve it by tool call id.
	if store != nil {
		store.RegisterWrite(path, tcID)
	}
	return nil
}

// --- public API (signatures preserved) ---

// SetTodoSession switches the current session and loads its file into the
// in-memory copy. The signature is unchanged; the file path is resolved from
// the project root.
func SetTodoSession(sessionID string) {
	todoStoreState.mu.Lock()
	defer todoStoreState.mu.Unlock()
	todoStoreState.current = sessionID
	ses, ok := todoStoreState.sessions[sessionID]
	if !ok {
		ses = &todoSession{}
		todoStoreState.sessions[sessionID] = ses
	}
	// Reload from disk: the file is the durable record. A legacy memory-only
	// restore (SetTodoState) is preserved only when no file exists yet.
	rev, items, exists, err := readTodoFile(sessionID)
	if err != nil {
		ses.parseErr = err
		return
	}
	ses.parseErr = nil
	if exists {
		ses.revision = rev
		ses.content = serializeTodo(rev, items)
	} else if ses.content == "" {
		ses.revision = 0
		ses.content = ""
	}
}

// ResetTodoState clears the in-memory copy for the current session only. It
// must NOT delete the file: the call sites move to a different session id, and
// deleting the outgoing session's todo would destroy exactly the state the
// file exists to preserve.
func ResetTodoState() {
	todoStoreState.mu.Lock()
	defer todoStoreState.mu.Unlock()
	if todoStoreState.current == "" {
		return
	}
	delete(todoStoreState.sessions, todoStoreState.current)
}

// TodoState serves the current session's in-memory copy. This is the TUI
// sidebar render path — it runs on every frame and must never touch the disk.
//
// A parse error is rendered, not swallowed: returning "" would print "No todo
// list for this session yet" over a file that exists and is corrupt, making
// the sidebar the one surface where the failure is silent.
func TodoState() string {
	todoStoreState.mu.RLock()
	defer todoStoreState.mu.RUnlock()
	if todoStoreState.current == "" {
		return ""
	}
	ses, ok := todoStoreState.sessions[todoStoreState.current]
	if !ok {
		return ""
	}
	if ses.parseErr != nil {
		return fmt.Sprintf("! todo file unreadable: %v", ses.parseErr)
	}
	return stripTodoHeader(ses.content)
}

// SetTodoState sets the current session's in-memory copy without touching
// disk. This is the legacy session-metadata restore path (restoreTodoState in
// the TUI reads todo_text out of a session file for sessions that predate the
// file-backed store). The first todowrite/todo_update in the session creates
// the file from this content.
func SetTodoState(state string) {
	todoStoreState.mu.Lock()
	defer todoStoreState.mu.Unlock()
	if todoStoreState.current == "" {
		return
	}
	ses, ok := todoStoreState.sessions[todoStoreState.current]
	if !ok {
		ses = &todoSession{}
		todoStoreState.sessions[todoStoreState.current] = ses
	}
	ses.content = state
	ses.parseErr = nil
}

// --- read/mutation helpers (used by the tools) ---

// currentTodo returns the current session id and its in-memory copy.
func currentTodo() (string, *todoSession) {
	todoStoreState.mu.RLock()
	defer todoStoreState.mu.RUnlock()
	return todoStoreState.current, todoStoreState.sessions[todoStoreState.current]
}

// readTodo returns the current session's revision + items for todoread.
func readTodo() (revision int, content string, err error) {
	cur, ses := currentTodo()
	if cur == "" {
		return 0, "", fmt.Errorf("todo session not set")
	}
	if ses != nil && ses.parseErr != nil {
		return 0, "", ses.parseErr
	}
	if ses == nil || ses.content == "" {
		return 0, "No todo list found", nil
	}
	rev, items, err := parseTodoContent(ses.content)
	if err != nil {
		return 0, "", err
	}
	return rev, serializeItems(items), nil
}

// baseItems returns the current session's base items for a mutation: the file
// content when a file exists, else the legacy in-memory copy, else empty.
// Caller must hold todoStoreState.mu.
func baseItems(fileExists bool, fileItems []todoItem) []todoItem {
	if fileExists {
		out := make([]todoItem, len(fileItems))
		copy(out, fileItems)
		return out
	}
	ses := todoStoreState.sessions[todoStoreState.current]
	if ses != nil && ses.content != "" {
		// parseTodoContent, not parseTodoDoc: legacy headerless content must
		// parse here too. Swallowing the error and returning nil would skip
		// the destructive guard entirely, so the first todowrite of a resumed
		// pre-store session would wipe the restored list.
		if _, items, err := parseTodoContent(ses.content); err == nil {
			return items
		} else {
			logTodoError("parse in-memory todo content for the destructive guard", err)
		}
	}
	return nil
}

// assignTodoIDs fills in stable ids for items missing one, using the next free
// tN id. Mutates the slice in place.
func assignTodoIDs(items []todoItem) {
	maxN := 0
	for _, it := range items {
		if it.id != "" && todoIDRe.MatchString(it.id) {
			if n, err := strconv.Atoi(it.id[1:]); err == nil && n > maxN {
				maxN = n
			}
		}
	}
	next := func() string {
		maxN++
		return "t" + strconv.Itoa(maxN)
	}
	for i := range items {
		if items[i].id == "" {
			items[i].id = next()
		}
	}
}

// staleTodoError builds the rejection for a stale revision. The current
// content rides in the error so the model re-reads and retries against fresh
// state.
func staleTodoError(sessionID string, cited, current int, currentItems []todoItem) error {
	return fmt.Errorf("todo write rejected: stale revision (you cited %d, current is %d). Re-read with todoread and retry.\nCurrent content:\n%s", cited, current, serializeItems(currentItems))
}

// todoWrite applies a full-replace todowrite. citedRevision must equal the
// file's current revision (0 when no file exists yet); a stale citation is
// rejected with the current content. Destructive full replacements — a rewrite
// that drops an existing item, particularly a completed one — are rejected
// unconditionally: there is no override flag, the rejection text is the fix
// (use todo_update for status changes).
func todoWrite(citedRevision int, todoText string, store *snapshot.Store, tcID string) (string, error) {
	todoStoreState.mu.Lock()
	defer todoStoreState.mu.Unlock()
	if todoStoreState.current == "" {
		return "", fmt.Errorf("todo session not set")
	}
	cur := todoStoreState.current

	newItems, err := parseTodoItems(todoText, false)
	if err != nil {
		return "", err
	}
	// NOTE: ids are deliberately NOT assigned here. Stamping t1..tN on the
	// incoming items before the destructive guard runs would make the guard
	// vacuous — every old id would appear to "survive" because the new items
	// were just handed those same positional ids. Assignment happens after
	// the guard, below.

	// Ensure the lock's directory exists before opening the lock file.
	if err := os.MkdirAll(todoDir(), 0o755); err != nil {
		return "", err
	}

	// The ENTIRE read → verify revision → guard → write sequence runs inside
	// one lock hold. Releasing between the read and the write would let two
	// processes both observe revision N, both pass the staleness check, and
	// both write N+1 — the second rename silently discarding the first's
	// items. The in-process mutex above does not help here: cross-process is
	// the only case this lock exists for.
	var out string
	err = filelock.WithFileLock(todoLockPath(), func() error {
		fileRev, fileItems, fileExists, rerr := readTodoFile(cur)
		if rerr != nil {
			return rerr
		}
		if fileExists && fileRev != citedRevision {
			return staleTodoError(cur, citedRevision, fileRev, fileItems)
		}
		if !fileExists && citedRevision != 0 {
			return fmt.Errorf("todo write rejected: no todo file exists for this session yet (cited revision %d); run todoread to get the current state", citedRevision)
		}

		// Destructive guard: every existing item must survive the rewrite.
		existing := fileItems
		if !fileExists {
			existing = baseItems(false, nil)
		}
		if gerr := guardNoDroppedItems(existing, newItems); gerr != nil {
			return gerr
		}
		// Only now assign ids: first let each new item inherit the id of the
		// existing item it matches by text (so a reorder keeps t1 pointing at
		// the same item), then fill the genuinely new ones.
		inheritTodoIDs(existing, newItems)
		assignTodoIDs(newItems)

		newRev := fileRev + 1
		if !fileExists {
			newRev = 1
		}
		if werr := writeTodoFile(cur, newRev, newItems, store, tcID); werr != nil {
			return werr
		}
		// Update the in-memory copy only after the file write succeeded.
		todoStoreState.sessions[cur] = &todoSession{
			revision: newRev,
			content:  serializeTodo(newRev, newItems),
		}
		out = fmt.Sprintf("Successfully updated todo list (revision %d)", newRev)
		return nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// guardNoDroppedItems rejects a full replace that would remove an existing
// item. Matching is by id when the incoming item carries one (the model may
// echo ids back to reword an item in place) and by exact text otherwise.
//
// It must run BEFORE any id assignment on newItems: positional ids stamped in
// advance would match every old id and the guard would never fire.
func guardNoDroppedItems(existing, newItems []todoItem) error {
	if len(existing) == 0 {
		return nil
	}
	newIDs := map[string]bool{}
	newTexts := map[string]bool{}
	for _, it := range newItems {
		if it.id != "" {
			newIDs[it.id] = true
		}
		if it.text != "" {
			newTexts[it.text] = true
		}
	}
	var dropped []string
	for _, old := range existing {
		kept := (old.id != "" && newIDs[old.id]) || (old.text != "" && newTexts[old.text])
		if !kept {
			dropped = append(dropped, fmt.Sprintf("%s %q", old.id, old.text))
		}
	}
	if len(dropped) > 0 {
		return fmt.Errorf("todowrite rejected: full replace would drop existing item(s) — %s. Use todo_update to change statuses or text instead of rewriting the whole list; a deliberate rewrite may reorder items, or reword one by echoing its id, but may never remove them.", strings.Join(dropped, ", "))
	}
	return nil
}

// inheritTodoIDs gives each id-less incoming item the id of the existing item
// with the same text. Without this, a legal reorder (`[a, b]` rewritten as
// `[b, a]`) would reassign ids positionally, so a later todo_update citing
// "t1" would mutate the wrong item.
func inheritTodoIDs(existing, newItems []todoItem) {
	byText := make(map[string]string, len(existing))
	for _, old := range existing {
		if old.text != "" && old.id != "" {
			if _, dup := byText[old.text]; !dup {
				byText[old.text] = old.id
			}
		}
	}
	taken := map[string]bool{}
	for _, it := range newItems {
		if it.id != "" {
			taken[it.id] = true
		}
	}
	for i := range newItems {
		if newItems[i].id != "" {
			continue
		}
		if id, ok := byText[newItems[i].text]; ok && !taken[id] {
			newItems[i].id = id
			taken[id] = true
		}
	}
}

// todoUpdateOps are the targeted mutations supported by todo_update.
const (
	todoOpSetStatus   = "set_status"
	todoOpEditText    = "edit_text"
	todoOpAppend      = "append"
	todoOpInsertAfter = "insert_after"
)

// todoUpdate applies a targeted mutation by item id. Every mutation must cite
// the revision it was based on; a stale citation is rejected with the current
// content.
func todoUpdate(citedRevision int, op, itemID, status, text string, store *snapshot.Store, tcID string) (string, error) {
	todoStoreState.mu.Lock()
	defer todoStoreState.mu.Unlock()
	if todoStoreState.current == "" {
		return "", fmt.Errorf("todo session not set")
	}
	cur := todoStoreState.current

	// Ensure the lock's directory exists before opening the lock file.
	if err := os.MkdirAll(todoDir(), 0o755); err != nil {
		return "", err
	}

	// One lock hold for read → verify revision → apply → write, for the same
	// reason as todoWrite: a lock released between the read and the write
	// does not prevent two processes from both writing revision N+1.
	var out string
	err := filelock.WithFileLock(todoLockPath(), func() error {
		fileRev, fileItems, fileExists, rerr := readTodoFile(cur)
		if rerr != nil {
			return rerr
		}
		if fileExists && fileRev != citedRevision {
			return staleTodoError(cur, citedRevision, fileRev, fileItems)
		}
		if !fileExists && citedRevision != 0 {
			return fmt.Errorf("todo update rejected: no todo file exists for this session yet (cited revision %d); run todoread to get the current state", citedRevision)
		}

		newItems := baseItems(fileExists, fileItems)
		assignTodoIDs(newItems)

		res, aerr := applyTodoOp(newItems, op, itemID, status, text)
		if aerr != nil {
			return aerr
		}
		newItems = res

		newRev := fileRev + 1
		if !fileExists {
			newRev = 1
		}
		if werr := writeTodoFile(cur, newRev, newItems, store, tcID); werr != nil {
			return werr
		}
		todoStoreState.sessions[cur] = &todoSession{
			revision: newRev,
			content:  serializeTodo(newRev, newItems),
		}
		out = fmt.Sprintf("Successfully updated todo list (revision %d)", newRev)
		return nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// applyTodoOp applies one targeted mutation to items and returns the new
// slice. Split out of todoUpdate so the whole read-verify-apply-write
// sequence can sit inside a single file-lock hold.
func applyTodoOp(newItems []todoItem, op, itemID, status, text string) ([]todoItem, error) {
	findIdx := func(id string) int {
		for i, it := range newItems {
			if it.id == id {
				return i
			}
		}
		return -1
	}

	switch op {
	case todoOpSetStatus:
		canon, err := normalizeTodoMarker(status)
		if err != nil {
			return nil, err
		}
		idx := findIdx(itemID)
		if idx < 0 {
			return nil, fmt.Errorf("todo_update: no item with id %q", itemID)
		}
		newItems[idx].status = canon
	case todoOpEditText:
		idx := findIdx(itemID)
		if idx < 0 {
			return nil, fmt.Errorf("todo_update: no item with id %q", itemID)
		}
		newItems[idx].text = text
	case todoOpAppend:
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("todo_update: append requires non-empty text")
		}
		canon := " "
		if status != "" {
			c, err := normalizeTodoMarker(status)
			if err != nil {
				return nil, err
			}
			canon = c
		}
		newItems = append(newItems, todoItem{status: canon, text: text})
		assignTodoIDs(newItems)
	case todoOpInsertAfter:
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("todo_update: insert_after requires non-empty text")
		}
		idx := findIdx(itemID)
		if idx < 0 {
			return nil, fmt.Errorf("todo_update: no item with id %q to insert after", itemID)
		}
		canon := " "
		if status != "" {
			c, err := normalizeTodoMarker(status)
			if err != nil {
				return nil, err
			}
			canon = c
		}
		rest := append([]todoItem{{status: canon, text: text}}, newItems[idx+1:]...)
		newItems = append(newItems[:idx+1], rest...)
		assignTodoIDs(newItems)
	default:
		return nil, fmt.Errorf("todo_update: unknown op %q (want set_status, edit_text, append, insert_after)", op)
	}

	return newItems, nil
}

// --- tools ---

// TodoWriteTool replaces the whole list. It survives only for creating or
// deliberately rewriting the list (see the destructive guard in todoWrite).
type TodoWriteTool struct{}

func (t TodoWriteTool) Name() string { return "todowrite" }
func (t TodoWriteTool) Description() string {
	return "Create or rewrite the current session todo list. Replaces the entire list, so use todo_update for targeted changes. Include the revision returned by todoread; a stale revision is rejected. Item lines: \"- [ ] text\", \"- [•] text\", or \"- [✓] text\". Completed items may never be dropped by a rewrite."
}
func (t TodoWriteTool) Parallel() bool { return false }
func (t TodoWriteTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "todowrite",
		"description": "Create or rewrite the current session todo list. Replaces the entire list, so use todo_update for targeted changes. Include the revision returned by todoread; a stale revision is rejected. Item lines: \"- [ ] text\", \"- [•] text\", or \"- [✓] text\". Completed items may never be dropped by a rewrite.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"revision": map[string]interface{}{
					"type":        "integer",
					"description": "The revision from the most recent todoread (0 when the list is empty).",
				},
				"todoText": map[string]interface{}{
					"type":        "string",
					"description": "The todo list content. Each line is a bullet starting with a status marker: [✓], [•], [ ]",
				},
			},
			"required": []string{"todoText"},
		},
	}
}

func (t TodoWriteTool) Execute(args json.RawMessage) (string, error) {
	return t.execute(context.Background(), args)
}

func (t TodoWriteTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	return t.execute(ctx, args)
}

func (t TodoWriteTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Revision int    `json:"revision"`
		TodoText string `json:"todoText"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	store := snapshot.FromContext(ctx)
	tcID := snapshot.ToolCallIDFromContext(ctx)
	return todoWrite(params.Revision, params.TodoText, store, tcID)
}

// TodoReadTool returns the current revision along with the content.
type TodoReadTool struct{}

func (t TodoReadTool) Name() string { return "todoread" }
func (t TodoReadTool) Description() string {
	return "Read the current session todo list, including its revision."
}
func (t TodoReadTool) Parallel() bool { return true }
func (t TodoReadTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "todoread",
		"description": "Read the current session todo list, including its revision.",
		"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}

func (t TodoReadTool) Execute(args json.RawMessage) (string, error) {
	rev, content, err := readTodo()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("revision: %d\n\n%s", rev, content), nil
}

// TodoUpdateTool applies a targeted mutation by item id.
type TodoUpdateTool struct{}

func (t TodoUpdateTool) Name() string { return "todo_update" }
func (t TodoUpdateTool) Description() string {
	return "Apply a targeted update to one todo item by its id."
}
func (t TodoUpdateTool) Parallel() bool { return false }
func (t TodoUpdateTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "todo_update",
		"description": "Apply a targeted update to one todo item by its id. Ops: set_status (status: pending|in_progress|done), edit_text (text), append (text, new item at end), insert_after (item_id + text, new item after the anchor). Every call must cite the revision from the most recent todoread; a stale revision is rejected.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"revision": map[string]interface{}{
					"type":        "integer",
					"description": "The revision from the most recent todoread.",
				},
				"op": map[string]interface{}{
					"type":        "string",
					"description": "set_status | edit_text | append | insert_after",
				},
				"item_id": map[string]interface{}{
					"type":        "string",
					"description": "The stable item id from todoread (required for set_status, edit_text, insert_after).",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "pending | in_progress | done (for set_status, or the new item's status for append/insert_after).",
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "New text for edit_text, or the new item's text for append/insert_after.",
				},
			},
			"required": []string{"revision", "op"},
		},
	}
}

func (t TodoUpdateTool) Execute(args json.RawMessage) (string, error) {
	return t.execute(context.Background(), args)
}

func (t TodoUpdateTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	return t.execute(ctx, args)
}

func (t TodoUpdateTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Revision int    `json:"revision"`
		Op       string `json:"op"`
		ItemID   string `json:"item_id"`
		Status   string `json:"status"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	store := snapshot.FromContext(ctx)
	tcID := snapshot.ToolCallIDFromContext(ctx)
	return todoUpdate(params.Revision, params.Op, params.ItemID, params.Status, params.Text, store, tcID)
}

// TodoSnapshot describes the current session's plan for the re-anchor path.
// HasOpenItems is true when at least one item is pending or in-progress, which
// is the gate for injection: a finished or absent list injects nothing.
type TodoSnapshot struct {
	Revision     int
	Content      string // canonical serialized items (no header), or ""
	HasOpenItems bool
}

// ReadTodoSnapshot returns the current session's plan for the re-anchor path.
func ReadTodoSnapshot() TodoSnapshot {
	cur, ses := currentTodo()
	if cur == "" {
		return TodoSnapshot{}
	}
	if ses == nil || ses.parseErr != nil || ses.content == "" {
		return TodoSnapshot{}
	}
	rev, items, err := parseTodoContent(ses.content)
	if err != nil {
		logTodoError("parse todo content for the context re-anchor block", err)
		return TodoSnapshot{}
	}
	open := false
	for _, it := range items {
		if it.status == " " || it.status == "•" {
			open = true
			break
		}
	}
	return TodoSnapshot{Revision: rev, Content: serializeItems(items), HasOpenItems: open}
}
