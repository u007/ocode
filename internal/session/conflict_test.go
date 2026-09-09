package session

// Regression tests for concurrent-writer divergence on one session file:
// the "conflicting message at seq N (concurrent writers diverged)" family.
//
// Cross-process writers share the same storage dir (TUI + desktop + web).
// In-process writers serialize on the per-session stripe mutex; across
// processes, serialization is the SQLite write lock (BEGIN IMMEDIATE via
// the _txlock=immediate DSN) plus the overlap check in
// appendSqliteSessionOnce. These tests pin the contract:
//
//   - identical overlap converges (idempotent retry),
//   - divergent overlap conflicts (never a silent drop),
//   - an ordinary sync save that is SHORTER than stored conflicts (the old
//     delete-all-and-rewrite silently destroyed another writer's appends),
//   - only the explicit replace path (Replace/ReplaceForDir) may shrink,
//   - AppendUserMessageForDir reloads and retries so a user message that
//     raced another writer's append still lands, and
//   - two independent writers appending under BEGIN IMMEDIATE serialize via
//     blocking, not the deferred-upgrade deadlock the old db.Begin() hit.

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

func mustSeedSession(t *testing.T, dir, id string, contents ...string) {
	t.Helper()
	if err := saveToDir(dir, id, "", liveMsgs(contents...), nil, false, 0); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func readStoredContents(t *testing.T, dir, id string) []agent.Message {
	t.Helper()
	s, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	return s.Messages
}

func contentsOf(msgs []agent.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

func assertContents(t *testing.T, got []agent.Message, want ...string) {
	t.Helper()
	if diff := fmt.Sprint(contentsOf(got)); diff != fmt.Sprint(want) {
		t.Fatalf("transcript mismatch:\n got: %v\nwant: %v", contentsOf(got), want)
	}
}

// TestSyncSaveDivergentOverlapConflicts: two writers append DIFFERENT
// messages at the same seq from a common base — the second sync save must
// error (typed conflict), never silently drop either branch.
func TestSyncSaveDivergentOverlapConflicts(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-overlap"
	mustSeedSession(t, dir, id, "m0", "m1", "m2")

	// Writer A appends its own continuation and lands first.
	if err := saveToDir(dir, id, "", liveMsgs("m0", "m1", "m2", "A3"), nil, false, 0); err != nil {
		t.Fatalf("writer A append: %v", err)
	}

	// Writer B (same base, different continuation) must conflict at seq 3.
	err := saveToDir(dir, id, "", liveMsgs("m0", "m1", "m2", "B3"), nil, false, 0)
	if !isConflictErr(err) {
		t.Fatalf("expected ErrTranscriptConflict, got: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "m0", "m1", "m2", "A3")

	// Writer B retrying with a stale snapshot keeps conflicting; only a
	// reload-then-append converges.
	err = saveToDir(dir, id, "", liveMsgs("m0", "m1", "m2", "B3"), nil, false, 0)
	if !isConflictErr(err) {
		t.Fatalf("stale retry must keep conflicting, got: %v", err)
	}
	if err := saveToDir(dir, id, "", liveMsgs("m0", "m1", "m2", "A3", "B4"), nil, false, 0); err != nil {
		t.Fatalf("reload-then-append: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "m0", "m1", "m2", "A3", "B4")
}

// TestSyncSaveShorterThanStoredConflicts pins the shrink hardening: an
// ordinary sync save with FEWER messages than stored (stale writer, or a
// failed load) must conflict instead of the old delete-all-and-rewrite that
// silently destroyed another writer's appended rows.
func TestSyncSaveShorterThanStoredConflicts(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-shrink"
	mustSeedSession(t, dir, id, "m0", "m1", "m2")

	// Another writer appended after this process's snapshot was taken.
	if err := saveToDir(dir, id, "", liveMsgs("m0", "m1", "m2", "m3", "m4"), nil, false, 0); err != nil {
		t.Fatalf("other writer append: %v", err)
	}

	// This process still holds the 3-message snapshot: shorter than stored.
	err := saveToDir(dir, id, "", liveMsgs("m0", "m1", "m2"), nil, false, 0)
	if !isConflictErr(err) {
		t.Fatalf("expected ErrTranscriptConflict for stale shorter snapshot, got: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "m0", "m1", "m2", "m3", "m4")
}

// TestReplaceShrinksAndDropsStaleLiveSnapshots: only the explicit replace
// path may shrink; it bumps history_gen so a queued pre-replacement live
// snapshot drops instead of resurrecting replaced history.
func TestReplaceShrinksAndDropsStaleLiveSnapshots(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-replace"
	mustSeedSession(t, dir, id, "m0", "m1", "m2", "m3")

	// Live write queued BEFORE the replacement, carrying its generation.
	gen, err := readHistoryGen(dir, id)
	if err != nil {
		t.Fatalf("readHistoryGen: %v", err)
	}
	if err := saveAsyncToDir(dir, id, "", liveMsgs("m0", "m1", "m2", "m3", "m4"), nil); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}

	// Ordinary sync save with a shorter snapshot: conflict, nothing lost.
	if err := persistToDir(dir, id, "", liveMsgs("m0", "m1"), nil, false, 0, false); !isConflictErr(err) {
		t.Fatalf("ordinary save must conflict on shrink, got: %v", err)
	}

	// Explicit replacement (compaction/truncation): allowed, bumps gen.
	if err := persistToDir(dir, id, "", liveMsgs("compacted"), nil, false, 0, true); err != nil {
		t.Fatalf("persistToDir (replace): %v", err)
	}
	newGen, err := readHistoryGen(dir, id)
	if err != nil || newGen != gen+1 {
		t.Fatalf("replace must bump history_gen: got %d want %d (%v)", newGen, gen+1, err)
	}

	// The queued pre-replacement live snapshot is superseded: dropped, so
	// replaced history is never resurrected.
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "compacted")
}

// TestLiveDivergentOverlapDropsSilently: live (never-regress) writes keep
// their by-design semantics — a snapshot that diverges from stored history
// is dropped without error, because the turn-end sync save stays
// authoritative.
func TestLiveDivergentOverlapDropsSilently(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-live"
	mustSeedSession(t, dir, id, "m0", "m1", "m2")

	if err := saveToDir(dir, id, "", liveMsgs("m0", "m1", "m2", "A3"), nil, false, 0); err != nil {
		t.Fatalf("writer A append: %v", err)
	}
	changed, err := appendSqliteSession(dir, id, "", liveMsgs("m0", "m1", "m2", "B3"), nil, true, 0, false)
	if err != nil {
		t.Fatalf("live divergent write must not error, got: %v", err)
	}
	if changed {
		t.Fatalf("live divergent write must be dropped, not applied")
	}
	assertContents(t, readStoredContents(t, dir, id), "m0", "m1", "m2", "A3")
}

// isolatedProjectRoot sets HOME to a temp dir (paths.GlobalDataDir derives
// from the user home on darwin) and returns a project root plus the
// sessions dir GetStorageDirForPath resolves for it — the real resolution
// path AppendUserMessageForDir uses in production.
func isolatedProjectRoot(t *testing.T) (projectRoot, sessionsDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot = filepath.Join(home, "work", "proj")
	var err error
	sessionsDir, err = GetStorageDirForPath(projectRoot)
	if err != nil {
		t.Fatalf("GetStorageDirForPath: %v", err)
	}
	return projectRoot, sessionsDir
}

// TestAppendUserMessageRetriesAfterConcurrentAppend reproduces the incident
// behind "conflicting message at seq 25": the server's persist-before-202
// load used to append the user message to a snapshot taken before another
// writer (a live turn in the TUI) appended, and the sync save conflicted —
// dropping the user's message. The helper must converge instead: the
// retried snapshot reloads the now-longer disk transcript and the user
// message lands after the other writer's messages.
func TestAppendUserMessageRetriesAfterConcurrentAppend(t *testing.T) {
	projectRoot, sessionsDir := isolatedProjectRoot(t)
	id := "ses_conflict-usermsg"
	base := []string{"q0", "a0", "q1", "a1"}
	mustSeedSession(t, sessionsDir, id, base...)

	// Simulate the stale server snapshot: loaded before writer B appended.
	stale, err := LoadForDir(projectRoot, id)
	if err != nil {
		t.Fatalf("LoadForDir (stale snapshot): %v", err)
	}

	// Writer B (a live turn elsewhere) appends while the server is between
	// load and save.
	if err := saveToDir(sessionsDir, id, "", liveMsgs("q0", "a0", "q1", "a1", "a2-live", "PERMISSION_ASK"), nil, false, 0); err != nil {
		t.Fatalf("writer B append: %v", err)
	}

	// The stale snapshot (disk base + user message) conflicts — the incident.
	staleMsgs := append(append([]agent.Message(nil), stale.Messages...), agent.Message{Role: "user", Content: "hello from user"})
	if err := persistToDir(sessionsDir, id, "", staleMsgs, nil, false, 0, false); !isConflictErr(err) {
		t.Fatalf("expected the stale persist to conflict (the incident), got: %v", err)
	}

	// The helper (load-fresh + append + bounded retry) converges: the user
	// message lands after the other writer's messages, nothing is lost.
	if err := AppendUserMessageForDir(projectRoot, id, "hello from user"); err != nil {
		t.Fatalf("AppendUserMessageForDir: %v", err)
	}
	assertContents(t, readStoredContents(t, sessionsDir, id),
		"q0", "a0", "q1", "a1", "a2-live", "PERMISSION_ASK", "hello from user")

	// A second append lands at the end too.
	if err := AppendUserMessageForDir(projectRoot, id, "second message"); err != nil {
		t.Fatalf("AppendUserMessageForDir (second): %v", err)
	}
	got := readStoredContents(t, sessionsDir, id)
	if got[len(got)-1].Content != "second message" {
		t.Fatalf("second user message did not land last: %+v", got)
	}
}

// TestAppendUserMessageCreatesMissingSession preserves the legacy
// persistUserMessage behavior for a session that does not exist on disk
// yet: the user message alone becomes the transcript.
func TestAppendUserMessageCreatesMissingSession(t *testing.T) {
	projectRoot, sessionsDir := isolatedProjectRoot(t)
	id := "ses_conflict-new"
	if err := AppendUserMessageForDir(projectRoot, id, "first message"); err != nil {
		t.Fatalf("AppendUserMessageForDir: %v", err)
	}
	assertContents(t, readStoredContents(t, sessionsDir, id), "first message")
}

// TestAppendUserMessageConcurrentWritersConverge: N independent writers
// append user messages concurrently to one session. Every append's
// snapshot is disk-derived, so races surface as conflicts that the
// helper's bounded retry must absorb — exercising the real retry loop.
func TestAppendUserMessageConcurrentWritersConverge(t *testing.T) {
	projectRoot, sessionsDir := isolatedProjectRoot(t)
	id := "ses_conflict-concurrent"
	mustSeedSession(t, sessionsDir, id, "seed")

	const writers = 4
	const perWriter = 5
	var wg sync.WaitGroup
	errs := make([][]error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				err := AppendUserMessageForDir(projectRoot, id, fmt.Sprintf("u%d-%d", w, i))
				if err != nil {
					errs[w] = append(errs[w], err)
				}
			}
		}(w)
	}
	wg.Wait()
	for w, list := range errs {
		if len(list) > 0 {
			t.Fatalf("writer %d failed: %v", w, list[0])
		}
	}
	got := readStoredContents(t, sessionsDir, id)
	if len(got) != 1+writers*perWriter {
		t.Fatalf("expected %d messages, got %d: %v", 1+writers*perWriter, len(got), contentsOf(got))
	}
	seen := map[string]bool{}
	for _, m := range got {
		seen[m.Content] = true
	}
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			if !seen[fmt.Sprintf("u%d-%d", w, i)] {
				t.Fatalf("writer %d message %d lost", w, i)
			}
		}
	}
}

// TestConcurrentAppendLoopsSerializeOnImmediateTx: two independent writers
// running load→append→save loops against one file must all succeed with
// every appended message present. Under the old deferred db.Begin() this
// pattern intermittently hit the SHARED→RESERVED upgrade deadlock that
// busy_timeout does not cover; with BEGIN IMMEDIATE the second writer
// blocks at BEGIN until the first commits.
func TestConcurrentAppendLoopsSerializeOnImmediateTx(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-immediate"
	mustSeedSession(t, dir, id, "seed")

	const loops = 6
	const writers = 2
	const attempts = 8
	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < loops; i++ {
				msg := fmt.Sprintf("w%d-%d", w, i)
				// A converging writer: reload and retry the whole cycle on
				// conflict — the same contract AppendUserMessageForDir
				// implements. A writer that saves a snapshot it loaded
				// before another writer committed is stale by definition;
				// the conflict is the signal to reload.
				var lastErr error
				ok := false
				for attempt := 0; attempt < attempts && !ok; attempt++ {
					if attempt > 0 {
						time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
					}
					// Fresh load from disk each attempt.
					s, err := readSqliteSession(sqliteSessionPath(dir, id))
					if err != nil {
						errCh <- fmt.Errorf("writer %d load: %w", w, err)
						return
					}
					msgs := append(append([]agent.Message(nil), s.Messages...), agent.Message{Role: "assistant", Content: msg})
					if err := saveToDir(dir, id, "", msgs, nil, false, 0); err != nil {
						if isConflictErr(err) {
							lastErr = err
							continue
						}
						errCh <- fmt.Errorf("writer %d save %s: %w", w, msg, err)
						return
					}
					ok = true
				}
				if !ok {
					errCh <- fmt.Errorf("writer %d save %s did not converge after %d attempts: %v", w, msg, attempts, lastErr)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent append loop failed: %v", err)
	}
	got := contentsOf(readStoredContents(t, dir, id))
	if len(got) != 1+writers*loops {
		t.Fatalf("expected %d messages, got %d: %v", 1+writers*loops, len(got), got)
	}
	for w := 0; w < writers; w++ {
		for i := 0; i < loops; i++ {
			want := fmt.Sprintf("w%d-%d", w, i)
			found := false
			for _, c := range got {
				if c == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("message %s lost", want)
			}
		}
	}
}

// TestHeldImmediateWriteLockBlocksNotFails: a writer holding a BEGIN
// IMMEDIATE transaction on a separate connection must BLOCK a concurrent
// append (up to busy_timeout) and then succeed — not fail-fast with the
// deferred-upgrade BUSY the old db.Begin() produced.
func TestHeldImmediateWriteLockBlocksNotFails(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-heldlock"
	mustSeedSession(t, dir, id, "m0")

	db, err := openSessionDB(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("openSessionDB: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatalf("BEGIN IMMEDIATE: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- saveToDir(dir, id, "", liveMsgs("m0", "m1"), nil, false, 0)
	}()
	time.Sleep(150 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("concurrent append did not block on the held write lock (should wait): %v", err)
	default:
	}
	if _, err := db.Exec(`COMMIT`); err != nil {
		t.Fatalf("COMMIT: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("append after lock release failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("append never completed after lock release")
	}
	assertContents(t, readStoredContents(t, dir, id), "m0", "m1")
}

// TestAppendUserMessageLandsAfterFilteredTail pins the tail-insert fix: a
// stored transcript whose tail holds a row the load path filters out
// (role tool with empty ToolID — e.g. the PERMISSION_ASK sentinel) made
// the old load→append→save persist conflict forever, because the loaded
// view is shorter than the stored rows and a "disk+1" snapshot compares a
// shifted sequence. The tail insert must land the user message after the
// filtered row, and a subsequent full-snapshot save of the same sequence
// (what the server does at turn end) must converge.
func TestAppendUserMessageLandsAfterFilteredTail(t *testing.T) {
	projectRoot, sessionsDir := isolatedProjectRoot(t)
	id := "ses_conflict-filteredtail"

	seed := []agent.Message{
		{Role: "user", Content: "list files"},
		{Role: "assistant", Content: "running ls"},
		{Role: "tool", Content: "PERMISSION_ASK:{\"tool_name\":\"bash\"}"}, // empty ToolID → filtered on load
	}
	if err := saveToDir(sessionsDir, id, "", seed, nil, false, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The user submits a second message while the first turn is paused on
	// the permission ask (server TestAsyncTurnRefusedWhilePermissionPending
	// regression): persist must succeed and land AFTER the sentinel.
	if err := AppendUserMessageForDir(projectRoot, id, "second message"); err != nil {
		t.Fatalf("AppendUserMessageForDir: %v", err)
	}
	got := readStoredContents(t, sessionsDir, id)
	if len(got) != 4 || got[2].Role != "tool" || got[3].Content != "second message" {
		t.Fatalf("user message did not land after the filtered tail: %+v", got)
	}

	// The server's in-memory transcript includes the sentinel, so its
	// turn-end full-snapshot save must converge against the stored rows.
	// runTurn stamps the appended user message with NextUserSeq over its
	// transcript — the tail insert above must have stamped the same seq.
	inMemory := append(append([]agent.Message(nil), seed...), agent.Message{Role: "user", Content: "second message", UserSeq: NextUserSeq(seed)})
	if err := persistToDir(sessionsDir, id, "", inMemory, nil, false, 0, false); err != nil {
		t.Fatalf("full-snapshot save after tail insert: %v", err)
	}
	assertContents(t, readStoredContents(t, sessionsDir, id),
		"list files", "running ls", "PERMISSION_ASK:{\"tool_name\":\"bash\"}", "second message")
}

// TestReconcileAppendMergesForeignAppends pins the turn-persistence
// reconcile: another writer's rows appended beyond the caller's base are
// kept, and the caller's unsaved suffix is appended after them — nothing
// is dropped, including rows of ours that our own live writes already
// stored interleaved with foreign ones (the greedy walk consumes those).
func TestReconcileAppendMergesForeignAppends(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-reconcile"

	base := liveMsgs("b0", "b1", "b2")
	mustSeedSession(t, dir, id, "b0", "b1", "b2")

	// The turn streamed: our live writes stored the first two response
	// messages, then a foreign writer appended two rows of its own, then a
	// third live row of ours landed. Disk = base + ours[0:2] + foreign(2) +
	// ours[2].
	ours := []agent.Message{
		{Role: "assistant", Content: "r0"},
		{Role: "assistant", Content: "r1"},
		{Role: "assistant", Content: "r2"},
	}
	disk := append(append([]agent.Message(nil), base...), ours[0], ours[1],
		agent.Message{Role: "user", Content: "foreign-0"},
		agent.Message{Role: "user", Content: "foreign-1"},
		ours[2])
	if err := saveToDir(dir, id, "", disk, nil, false, 0); err != nil {
		t.Fatalf("seed disk: %v", err)
	}

	// The caller's full view (base + all of ours) conflicts on a plain
	// save: ours[0..1] already in disk push our view's later rows into
	// positions the foreign rows occupy.
	if err := persistToDir(dir, id, "", append(append([]agent.Message(nil), base...), ours...), nil, false, 0, false); !isConflictErr(err) {
		t.Fatalf("expected plain save of the racing snapshot to conflict, got: %v", err)
	}

	// Reconcile keeps the foreign rows and appends nothing duplicated.
	if err := reconcileAppendToDir(dir, id, "", append(append([]agent.Message(nil), base...), ours...), len(base), nil); err != nil {
		t.Fatalf("reconcileAppendToDir: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id),
		"b0", "b1", "b2", "r0", "r1", "foreign-0", "foreign-1", "r2")

	// Idempotent: reconciling the same view again is a converged no-op save.
	if err := reconcileAppendToDir(dir, id, "", append(append([]agent.Message(nil), base...), ours...), len(base), nil); err != nil {
		t.Fatalf("reconcileAppendToDir (idempotent): %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id),
		"b0", "b1", "b2", "r0", "r1", "foreign-0", "foreign-1", "r2")
}

// TestReconcileAppendBaseDivergedReturnsConflict: when the divergence is
// inside the caller's base (two writers produced different content for the
// same position), no safe merge exists — the conflict is returned instead
// of silently dropping either side.
func TestReconcileAppendBaseDivergedReturnsConflict(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-rebase-diverged"
	mustSeedSession(t, dir, id, "b0", "b1", "b2")

	// Foreign writer replaced row 1 (same length, different content) via
	// its own authoritative replace.
	if err := persistToDir(dir, id, "", liveMsgs("b0", "FOREIGN-b1", "b2", "f3"), nil, false, 0, true); err != nil {
		t.Fatalf("foreign replace: %v", err)
	}

	ours := append(liveMsgs("b0", "b1", "b2"), agent.Message{Role: "assistant", Content: "r0"})
	err := reconcileAppendToDir(dir, id, "", ours, 3, nil)
	if !isConflictErr(err) {
		t.Fatalf("expected conflict for diverged base, got: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "b0", "FOREIGN-b1", "b2", "f3")
}

// TestReplaceSameLengthRewritesAndBumpsGen pins the replace semantics
// beyond shrinking: an authoritative replacement may also rewrite
// overlapping content at the same length (e.g. a re-compaction with a
// different summary). Diverging content rewrites wholesale and bumps
// history_gen; byte-identical content is not a rewrite (no bump).
func TestReplaceSameLengthRewritesAndBumpsGen(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-replace-eq"
	mustSeedSession(t, dir, id, "m0", "m1", "m2")
	gen0, err := readHistoryGen(dir, id)
	if err != nil {
		t.Fatalf("readHistoryGen: %v", err)
	}

	// Same length, different content: ordinary save conflicts...
	if err := persistToDir(dir, id, "", liveMsgs("m0", "CHANGED", "m2"), nil, false, 0, false); !isConflictErr(err) {
		t.Fatalf("ordinary same-length content change must conflict, got: %v", err)
	}
	// ...while the explicit replace rewrites and bumps the generation.
	if err := persistToDir(dir, id, "", liveMsgs("m0", "CHANGED", "m2"), nil, false, 0, true); err != nil {
		t.Fatalf("replace same-length rewrite: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "m0", "CHANGED", "m2")
	gen1, err := readHistoryGen(dir, id)
	if err != nil || gen1 != gen0+1 {
		t.Fatalf("rewrite must bump history_gen: got %d want %d (%v)", gen1, gen0+1, err)
	}

	// Prefix-converging replace (identical overlap + appended rows) is a
	// plain append: no generation bump.
	if err := persistToDir(dir, id, "", liveMsgs("m0", "CHANGED", "m2", "m3"), nil, false, 0, true); err != nil {
		t.Fatalf("replace with converging append: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "m0", "CHANGED", "m2", "m3")
	gen2, err := readHistoryGen(dir, id)
	if err != nil || gen2 != gen1 {
		t.Fatalf("converging replace-append must not bump history_gen: got %d want %d (%v)", gen2, gen1, err)
	}
}

// TestAppendUserMessageStampsUserSeq pins the seq contract between the async
// pre-persist and the turn: the tail insert stamps max stored user_seq + 1,
// exactly what server runTurn (NextUserSeq over the loaded transcript)
// assigns in memory, so the two copies serialize identically and the
// turn's live/turn-end saves never read as a diverged writer.
func TestAppendUserMessageStampsUserSeq(t *testing.T) {
	projectRoot, sessionsDir := isolatedProjectRoot(t)
	id := "ses_userseq-stamp"

	if err := AppendUserMessageForDir(projectRoot, id, "first"); err != nil {
		t.Fatalf("first append: %v", err)
	}
	got := readStoredContents(t, sessionsDir, id)
	if len(got) != 1 || got[0].UserSeq != 1 {
		t.Fatalf("first user message must carry user_seq 1, got %+v", got)
	}
	if want := NextUserSeq(nil); want != 1 {
		t.Fatalf("NextUserSeq(empty) = %d, want 1", want)
	}

	reply := []agent.Message{got[0], {Role: "assistant", Content: "ok"}}
	if err := persistToDir(sessionsDir, id, "", reply, nil, false, 0, false); err != nil {
		t.Fatalf("turn-end save: %v", err)
	}
	if err := AppendUserMessageForDir(projectRoot, id, "second"); err != nil {
		t.Fatalf("second append: %v", err)
	}
	got = readStoredContents(t, sessionsDir, id)
	if len(got) != 3 || got[2].UserSeq != 2 || got[2].UserSeq != NextUserSeq(reply) {
		t.Fatalf("second user message must carry user_seq 2 (= NextUserSeq(transcript)), got %+v", got)
	}
}

// TestReconcileAppendFromFilteredBase: the caller's base is the LOADER's
// view of disk (loadFromDir → removeIncompleteToolRequests dropped an
// unanswered question/permission round), so it is shorter and shifted
// relative to the stored rows. That is not a diverged base: the stored
// rows stay in place and the caller's new suffix lands after them, so the
// loader view of the result equals the caller's transcript. Before this,
// the merge reported base divergence, the server re-synced memory to the
// filtered disk copy, and the whole turn's streamed output vanished
// (desktop "streamed reply disappeared at turn end", 2026-09-09).
func TestReconcileAppendFromFilteredBase(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-filtered-base"

	askCall := agent.ToolCall{ID: "q-1", Type: "function"}
	askCall.Function.Name = "question"
	askCall.Function.Arguments = "{}"
	stored := []agent.Message{
		{Role: "user", Content: "u0", UserSeq: 1},
		{Role: "assistant", ToolCalls: []agent.ToolCall{askCall}},
		{Role: "tool", ToolID: "q-1", Content: tool.SentinelQuestionPrompt + "\n[]\n\n" + tool.SentinelWaitingForUser},
		{Role: "user", Content: "u1", UserSeq: 2},
	}
	if err := saveToDir(dir, id, "", stored, nil, false, 0); err != nil {
		t.Fatalf("seed disk: %v", err)
	}
	loaded, err := loadFromDir(dir, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertContents(t, loaded.Messages, "u0", "u1")

	// The turn ran from the loader view and produced one reply.
	ours := append(append([]agent.Message(nil), loaded.Messages...), agent.Message{Role: "assistant", Content: "r0"})
	if err := reconcileAppendToDir(dir, id, "", ours, len(loaded.Messages), nil); err != nil {
		t.Fatalf("reconcileAppendToDir: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "u0", "", stored[2].Content, "u1", "r0")

	// The loader view of the merged file is exactly the caller's transcript,
	// and a second turn from that view converges the same way.
	loaded, err = loadFromDir(dir, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	assertContents(t, loaded.Messages, "u0", "u1", "r0")
	ours = append(append([]agent.Message(nil), loaded.Messages...), agent.Message{Role: "assistant", Content: "r1"})
	if err := reconcileAppendToDir(dir, id, "", ours, len(loaded.Messages), nil); err != nil {
		t.Fatalf("reconcileAppendToDir (second turn): %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "u0", "", stored[2].Content, "u1", "r0", "r1")
}

// TestLiveSaveFromFilteredBaseAppends: live (mid-turn) snapshots from a
// loader-view base must land on disk, not drop — otherwise a crash
// mid-turn loses the whole turn on any session resumed past an
// unanswered ask round. Same shape as TestReconcileAppendFromFilteredBase
// but through the live path (no baseLen).
func TestLiveSaveFromFilteredBaseAppends(t *testing.T) {
	dir := t.TempDir()
	id := "ses_conflict-filtered-live"

	askCall := agent.ToolCall{ID: "q-1", Type: "function"}
	askCall.Function.Name = "question"
	askCall.Function.Arguments = "{}"
	stored := []agent.Message{
		{Role: "user", Content: "u0", UserSeq: 1},
		{Role: "assistant", ToolCalls: []agent.ToolCall{askCall}},
		{Role: "tool", ToolID: "q-1", Content: tool.SentinelQuestionPrompt + "\n[]\n\n" + tool.SentinelWaitingForUser},
		{Role: "user", Content: "u1", UserSeq: 2},
	}
	if err := saveToDir(dir, id, "", stored, nil, false, 0); err != nil {
		t.Fatalf("seed disk: %v", err)
	}
	loaded, err := loadFromDir(dir, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	gen, err := readHistoryGen(dir, id)
	if err != nil {
		t.Fatalf("readHistoryGen: %v", err)
	}

	// Base snapshot (shorter than disk): nothing new, must not error.
	live := append([]agent.Message(nil), loaded.Messages...)
	if changed, err := appendSqliteSession(dir, id, "", live, nil, true, gen, false); err != nil || changed {
		t.Fatalf("base live snapshot: changed=%v err=%v", changed, err)
	}
	// Two streamed messages land in order after the raw rows.
	live = append(live, agent.Message{Role: "assistant", Content: "r0"})
	if changed, err := appendSqliteSession(dir, id, "", live, nil, true, gen, false); err != nil || !changed {
		t.Fatalf("live snapshot r0: changed=%v err=%v", changed, err)
	}
	live = append(live, agent.Message{Role: "assistant", Content: "r1"})
	if changed, err := appendSqliteSession(dir, id, "", live, nil, true, gen, false); err != nil || !changed {
		t.Fatalf("live snapshot r1: changed=%v err=%v", changed, err)
	}
	assertContents(t, readStoredContents(t, dir, id), "u0", "", stored[2].Content, "u1", "r0", "r1")

	// A stale re-queued snapshot is a no-op, and the turn-end sync save
	// converges without conflict.
	if changed, err := appendSqliteSession(dir, id, "", live[:len(live)-1], nil, true, gen, false); err != nil || changed {
		t.Fatalf("stale live snapshot: changed=%v err=%v", changed, err)
	}
	if err := reconcileAppendToDir(dir, id, "", live, len(loaded.Messages), nil); err != nil {
		t.Fatalf("turn-end reconcile: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "u0", "", stored[2].Content, "u1", "r0", "r1")
	loaded, err = loadFromDir(dir, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	assertContents(t, loaded.Messages, "u0", "u1", "r0", "r1")
}
