package session

// Tests for the live async session writer (live.go): incremental
// persistence, stale-ordering, error-carrying flush, migration, worker
// retirement, and snapshot isolation.

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

func liveMsgs(contents ...string) []agent.Message {
	msgs := make([]agent.Message, len(contents))
	for i, c := range contents {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = agent.Message{Role: role, Content: c}
	}
	return msgs
}

func readLiveMessages(t *testing.T, dir, id string) *Session {
	t.Helper()
	s, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	return s
}

func TestLiveAsyncPersistsIncrementally(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-incr"

	if err := saveAsyncToDir(dir, id, "", liveMsgs("one"), nil); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	if got := readLiveMessages(t, dir, id); len(got.Messages) != 1 {
		t.Fatalf("expected 1 message after first live write, got %d", len(got.Messages))
	}

	if err := saveAsyncToDir(dir, id, "", liveMsgs("one", "two", "three"), nil); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	got := readLiveMessages(t, dir, id)
	if len(got.Messages) != 3 || got.Messages[2].Content != "three" {
		t.Fatalf("expected 3 ordered messages, got %+v", got.Messages)
	}
}

func TestLiveStaleSnapshotIsNoOp(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-stale"

	full := liveMsgs("one", "two", "three")
	if err := saveToDir(dir, id, "Real Title", full, map[string]any{"v": float64(2)}, false, 0); err != nil {
		t.Fatalf("saveToDir (sync seed): %v", err)
	}
	before := readLiveMessages(t, dir, id)
	gen, err := readHistoryGen(dir, id)
	if err != nil {
		t.Fatalf("readHistoryGen: %v", err)
	}

	// A stale queued snapshot (fewer messages, older title/metadata) landing
	// after the sync save must change nothing.
	changed, err := appendSqliteSession(dir, id, "Stale Title",
		liveMsgs("one"), map[string]any{"v": float64(1)}, true, gen)
	if err != nil {
		t.Fatalf("appendSqliteSession (live stale): %v", err)
	}
	if changed {
		t.Fatalf("expected stale live write to report unchanged")
	}
	after := readLiveMessages(t, dir, id)
	if len(after.Messages) != 3 {
		t.Fatalf("stale live write shrank transcript: %+v", after.Messages)
	}
	if after.Title != "Real Title" {
		t.Fatalf("stale live write regressed title: %q", after.Title)
	}
	if after.Metadata["v"] != float64(2) {
		t.Fatalf("stale live write regressed metadata: %+v", after.Metadata)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("stale live write touched updated_at: %v -> %v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestLiveDoesNotShrink(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-noshrink"

	if err := writeSqliteSessionFull(dir, Session{
		ID:        id,
		Messages:  liveMsgs("one", "two", "three"),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("writeSqliteSessionFull: %v", err)
	}
	// Compaction-style shrink via the live path must be refused; only the
	// synchronous path may replace the message set wholesale.
	changed, err := appendSqliteSession(dir, id, "", liveMsgs("compacted"), nil, true, 0)
	if err != nil {
		t.Fatalf("appendSqliteSession (live shrink): %v", err)
	}
	if changed {
		t.Fatalf("expected live shrink to report unchanged")
	}
	if got := readLiveMessages(t, dir, id); len(got.Messages) != 3 {
		t.Fatalf("live write shrank transcript, got %+v", got.Messages)
	}
}

func TestLiveConcurrentEnqueuesConverge(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-race"

	full := liveMsgs("one", "two", "three", "four", "five")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := saveAsyncToDir(dir, id, "", full, nil); err != nil {
				t.Errorf("saveAsyncToDir: %v", err)
			}
		}()
	}
	wg.Wait()
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	got := readLiveMessages(t, dir, id)
	if len(got.Messages) != 5 {
		t.Fatalf("expected 5 converged messages, got %d: %+v", len(got.Messages), got.Messages)
	}
	for i, m := range full {
		if got.Messages[i].Content != m.Content {
			t.Fatalf("message %d diverged: %q != %q", i, got.Messages[i].Content, m.Content)
		}
	}
}

func TestLiveSnapshotIsolatedFromCallerMutation(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-isolation"

	msgs := []agent.Message{{
		Role:    "assistant",
		Content: "original",
		ToolCalls: []agent.ToolCall{{
			ID:   "call-1",
			Type: "function",
		}},
	}}
	msgs[0].ToolCalls[0].Function.Name = "orig-tool"
	meta := map[string]any{"k": "v"}
	if err := saveAsyncToDir(dir, id, "", msgs, meta); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}
	// Mutate everything after enqueue: the background marshal must not see it.
	msgs[0].Content = "mutated"
	msgs[0].ToolCalls[0].Function.Name = "mutated-tool"
	meta["k"] = "mutated"
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	got := readLiveMessages(t, dir, id)
	if got.Messages[0].Content != "original" {
		t.Fatalf("caller mutation raced live write: %q", got.Messages[0].Content)
	}
	if got.Messages[0].ToolCalls[0].Function.Name != "orig-tool" {
		t.Fatalf("caller mutation raced live write (tool call): %q", got.Messages[0].ToolCalls[0].Function.Name)
	}
	if got.Metadata["k"] != "v" {
		t.Fatalf("caller mutation raced live write (metadata): %+v", got.Metadata)
	}
}

func TestLiveFlushReportsWriteError(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-err"

	// Unmarshalable metadata fails fast at enqueue, like the sync path.
	badMeta := map[string]any{"fn": func() {}}
	if err := saveAsyncToDir(dir, id, "", liveMsgs("one"), badMeta); err == nil {
		t.Fatalf("expected enqueue to fail on unmarshalable metadata")
	}

	// A write failure inside the worker must reach Flush, not vanish. The
	// directory stays read-only through the flush so the failure is
	// deterministic.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0755) //nolint:errcheck
	if err := saveAsyncToDir(dir, id, "", liveMsgs("one"), nil); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}
	if err := flushDir(dir, id, 10*time.Second); err == nil {
		t.Fatalf("expected flush to report the worker's write error")
	}
}

func TestLiveMigratesLegacySession(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-migrate"

	if err := saveOjsonl(dir, id, "legacy title", liveMsgs("first"), nil); err != nil {
		t.Fatalf("saveOjsonl (seed): %v", err)
	}
	if err := saveAsyncToDir(dir, id, "", liveMsgs("first", "second"), nil); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	if fileExists(ojsonlSessionPath(dir, id)) {
		t.Fatalf("expected .ojsonl removed after live-write migration")
	}
	got := readLiveMessages(t, dir, id)
	if len(got.Messages) != 2 || got.Messages[1].Content != "second" {
		t.Fatalf("expected migrated transcript, got %+v", got.Messages)
	}
	if got.Title != "legacy title" {
		t.Fatalf("expected legacy title preserved, got %q", got.Title)
	}
}

func TestLiveFlushUnknownSessionIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := flushDir(dir, "ses_live-never-touched", 5*time.Second); err != nil {
		t.Fatalf("flush of unknown session should succeed, got %v", err)
	}
}

func TestLiveGenDropsPreCompactionSnapshot(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-gen"

	seed := liveMsgs("m0", "m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8", "m9")
	if err := saveToDir(dir, id, "", seed, nil, false, 0); err != nil {
		t.Fatalf("saveToDir (seed): %v", err)
	}
	// Queue a live snapshot with one more message, then compact
	// authoritatively before the worker necessarily writes. Whichever lands
	// first, the converged state must be the compacted transcript: either
	// the shrink overwrites the live write, or the live write mismatches
	// the bumped generation and drops.
	grown := append(append([]agent.Message(nil), seed...), agent.Message{Role: "user", Content: "m10"})
	if err := saveAsyncToDir(dir, id, "", grown, nil); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}
	compacted := liveMsgs("summary")
	if err := saveToDir(dir, id, "", compacted, nil, false, 0); err != nil {
		t.Fatalf("saveToDir (compact): %v", err)
	}
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	got := readLiveMessages(t, dir, id)
	if len(got.Messages) != 1 || got.Messages[0].Content != "summary" {
		t.Fatalf("pre-compaction live write resurrected history: %+v", got.Messages)
	}
	if gen, err := readHistoryGen(dir, id); err != nil || gen != 1 {
		t.Fatalf("expected history_gen 1 after one shrink, got %d, %v", gen, err)
	}

	// Direct unit: a live write carrying a superseded generation drops even
	// when it is longer than what is stored.
	id2 := "ses_live-gen-direct"
	if err := saveToDir(dir, id2, "", liveMsgs("a", "b", "c"), nil, false, 0); err != nil {
		t.Fatalf("saveToDir (seed2): %v", err)
	}
	if err := saveToDir(dir, id2, "", liveMsgs("compacted"), nil, false, 0); err != nil {
		t.Fatalf("saveToDir (compact2): %v", err)
	}
	changed, err := appendSqliteSession(dir, id2, "", liveMsgs("a", "b", "c", "d"), nil, true, 0)
	if err != nil {
		t.Fatalf("appendSqliteSession (superseded gen): %v", err)
	}
	if changed {
		t.Fatalf("expected superseded-generation live write to report unchanged")
	}
	if got := readLiveMessages(t, dir, id2); len(got.Messages) != 1 {
		t.Fatalf("superseded live write resurrected history: %+v", got.Messages)
	}
}

// liveTestWriter builds a worker detached from the registry so drain() can
// be driven synchronously — fully deterministic, no goroutine timing.
func liveTestWriter(dir, id string) *liveWriter {
	return &liveWriter{dir: dir, id: id, notify: make(chan struct{}, 1)}
}

func liveBarrierDone(b flushBarrier) (error, bool) {
	select {
	case err := <-b.done:
		return err, true
	default:
		return nil, false
	}
}

func TestLiveBarrierWaitsForQueuedWrite(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-handoff"

	// Reenact the handoff race deterministically: a barrier registered with
	// no pending write must not complete until the write lands.
	w := liveTestWriter(dir, id)
	b := flushBarrier{seq: 1, done: make(chan error, 1)}
	w.barriers = append(w.barriers, b)
	w.drain()
	if _, fired := liveBarrierDone(b); fired {
		t.Fatalf("barrier completed before its pending write")
	}
	if len(w.barriers) != 1 {
		t.Fatalf("uncovered barrier must stay queued, got %d", len(w.barriers))
	}
	w.pending = &liveReq{seq: 1, gen: 0, msgs: liveMsgs("one")}
	w.drain()
	if err, fired := liveBarrierDone(b); !fired || err != nil {
		t.Fatalf("barrier should complete with the covering write (fired=%v err=%v)", fired, err)
	}
	if got := readLiveMessages(t, dir, id); len(got.Messages) != 1 {
		t.Fatalf("expected written message, got %+v", got.Messages)
	}
}

func TestLiveBarrierReportsCoveringWriteError(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-barrier-err"

	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0755) //nolint:errcheck

	// A barrier live during a failed write reports the failure, even though
	// a later write succeeds: the failed write at seq 1 is the covering
	// write for a barrier targeting seq 1.
	w := liveTestWriter(dir, id)
	w.pending = &liveReq{seq: 1, gen: 0, msgs: liveMsgs("one")}
	b1 := flushBarrier{seq: 1, done: make(chan error, 1)}
	w.barriers = append(w.barriers, b1)
	w.drain()
	if err, fired := liveBarrierDone(b1); !fired || err == nil {
		t.Fatalf("barrier should report its covering write's error (fired=%v err=%v)", fired, err)
	}

	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	w.pending = &liveReq{seq: 2, gen: 0, msgs: liveMsgs("one", "two")}
	b2 := flushBarrier{seq: 2, done: make(chan error, 1)}
	w.barriers = append(w.barriers, b2)
	w.drain()
	if err, fired := liveBarrierDone(b2); !fired || err != nil {
		t.Fatalf("barrier should succeed with its covering write (fired=%v err=%v)", fired, err)
	}
	if got := readLiveMessages(t, dir, id); len(got.Messages) != 2 {
		t.Fatalf("expected recovered transcript, got %+v", got.Messages)
	}
}

func TestLiveCoalescedWriteCoversSupersededBarrier(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-coalesce"

	// The seq-1 snapshot was superseded before the worker ran; the seq-2
	// write covers it, so a barrier targeting seq 1 succeeds with nil.
	w := liveTestWriter(dir, id)
	w.pending = &liveReq{seq: 2, gen: 0, msgs: liveMsgs("one", "two")}
	b := flushBarrier{seq: 1, done: make(chan error, 1)}
	w.barriers = append(w.barriers, b)
	w.drain()
	if err, fired := liveBarrierDone(b); !fired || err != nil {
		t.Fatalf("superseded barrier should succeed with covering write (fired=%v err=%v)", fired, err)
	}
}

func TestLiveSyncSaveSatisfiesQueuedBarriers(t *testing.T) {
	dir := t.TempDir()
	id := "ses_live-markcovered"

	// Mirror Save's two steps without coupling to the process workdir:
	// authoritative sync write, then markLiveCovered. A shutdown flush with
	// no further live writes must complete instead of timing out.
	if err := saveAsyncToDir(dir, id, "", liveMsgs("one"), nil); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}
	if err := saveToDir(dir, id, "", liveMsgs("one", "two"), nil, false, 0); err != nil {
		t.Fatalf("saveToDir (sync): %v", err)
	}
	markLiveCovered(dir, id)
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flush after authoritative sync save should succeed, got %v", err)
	}
}

func TestLiveWorkerRetiresWhenIdle(t *testing.T) {
	old := liveIdleTimeout
	liveIdleTimeout = 20 * time.Millisecond
	defer func() { liveIdleTimeout = old }()

	dir := t.TempDir()
	id := "ses_live-retire"
	if err := saveAsyncToDir(dir, id, "", liveMsgs("one"), nil); err != nil {
		t.Fatalf("saveAsyncToDir: %v", err)
	}
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		liveRegistryMu.Lock()
		_, ok := liveRegistry[liveKey(dir, id)]
		liveRegistryMu.Unlock()
		if !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("idle live worker never retired")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLiveFlushAllDrainsEverySession(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("ses_live-all-%d", i)
		if err := saveAsyncToDir(dir, id, "", liveMsgs("one"), nil); err != nil {
			t.Fatalf("saveAsyncToDir: %v", err)
		}
	}
	if err := FlushAll(10 * time.Second); err != nil {
		t.Fatalf("FlushAll: %v", err)
	}
	for i := 0; i < 3; i++ {
		if got := readLiveMessages(t, dir, fmt.Sprintf("ses_live-all-%d", i)); len(got.Messages) != 1 {
			t.Fatalf("session %d missing live write: %+v", i, got.Messages)
		}
	}
}
