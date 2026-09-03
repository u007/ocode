package session

// Live async session persistence.
//
// Previously every session write was a synchronous full-transcript save issued
// at turn boundaries: persisting per-message during a turn was deemed too
// expensive when sessions were single JSON blobs rewritten on every save.
// Now that sessions are SQLite files with an incremental-append path
// (appendSqliteSession writes only messages[existingCount:]), per-message
// writes are cheap — so streaming turns persist each completed message to the
// session history file live, in the background, instead of batching to the
// end of the turn. A crash mid-turn then loses at most the in-flight LLM
// round, not the whole turn.
//
// Design:
//   - SaveAsync / SaveAsyncForDir enqueue a snapshot for a session and return
//     immediately. One daemon worker per session storage-dir+id writes
//     snapshots in order, coalescing to the latest pending snapshot when the
//     worker falls behind (intermediate snapshots are superseded — they are
//     prefixes of the latest one). Workers retire after a few idle minutes;
//     the registry re-creates them on demand.
//   - Live writes pass live=true into saveToDir/appendSqliteSession: they
//     NEVER shrink or regress the transcript. A snapshot that adds no new
//     messages (e.g. an async write landing after the turn's final sync
//     save) is a complete no-op — title/metadata ride along only with
//     genuinely new messages. The synchronous Save/SaveForDir path stays
//     authoritative and keeps the existing shrink (compaction) semantics.
//   - All writers serialize in-process on a per-session stripe mutex (lockFor),
//     and cross-process serialization comes from a BEGIN IMMEDIATE
//     transaction: the second writer blocks until the first commits, then
//     re-reads the count inside its own transaction. Overlapping rows with
//     differing content are a conflict error, never a silent drop.
//   - Flush / FlushForDir / FlushAll register sequence barriers and block
//     until writes covering their seq complete, reporting the covering
//     write's outcome — never a false success, never before a pending write
//     they were meant to wait for. Uncovered barriers stay queued (the
//     worker never idles out while any remain). Exit paths drain via these;
//     turn-end code does not need to — the final sync save plus stale-skip
//     ordering makes every interleaving converge, and a successful sync
//     save marks the session covered via markLiveCovered.
//
// Locking: liveRegistryMu always precedes a worker's mu. SaveAsync and
// worker retirement take them in that order; the write/flush paths take each
// briefly and never nest in reverse.

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// liveIdleTimeout retires a session worker after this long without work.
// A variable (not a const) so tests can shrink it.
var liveIdleTimeout = 5 * time.Minute

// liveReq is one queued snapshot for a session's worker. seq orders the
// snapshot in the session's enqueue stream; gen is the history generation it
// was taken against (see readHistoryGen).
type liveReq struct {
	seq   uint64
	gen   int64
	title string
	msgs  []agent.Message
	meta  map[string]any
}

// flushBarrier is a drain request for everything enqueued up to seq: the
// worker reports once writes covering the barrier's seq have completed, so
// Flush reports real durability instead of always succeeding. Barriers whose
// seq is not yet covered stay queued across drain calls — they are never
// completed early and never discarded.
type flushBarrier struct {
	seq  uint64
	done chan error
}

// liveWriter is the per-session async persistence worker. pending holds the
// latest not-yet-written snapshot (coalescing slot); notify wakes the worker;
// barriers are drain requests closed once everything queued so far is
// written. seq numbers every enqueue; writtenSeq tracks the latest executed
// write; coveredOKSeq tracks the latest seq known durable (via a successful
// live write, whose snapshot covers everything before it, or via
// markLiveCovered after an authoritative sync save); lastFailErr carries the
// latest live-write failure for barriers covered only by failed writes.
type liveWriter struct {
	dir string
	id  string

	mu           sync.Mutex
	pending      *liveReq
	notify       chan struct{}
	barriers     []flushBarrier
	seq          uint64
	writtenSeq   uint64
	coveredOKSeq uint64
	lastFailErr  error
}

var liveRegistryMu sync.Mutex
var liveRegistry = map[string]*liveWriter{}

// sessionLocks serializes writers (sync Save/SaveForDir and async workers)
// for the same session file within this process. It is a fixed striped pool
// (not one mutex per session): hashing the session key into a small array
// bounds memory in long-lived servers no matter how many distinct sessions
// are ever saved. Unrelated sessions sharing a stripe merely contend briefly;
// correctness never depends on stripe exclusivity because the SQLite
// BEGIN IMMEDIATE transaction below provides the cross-process serialization
// and conflict detection. Cross-process races additionally rely on SQLite WAL
// + busy_timeout plus the overlap check in appendSqliteSession.
const sessionLockStripes = 64

var sessionLocks [sessionLockStripes]sync.Mutex

func liveKey(dir, id string) string { return dir + "\x00" + id }

// lockFor returns the in-process stripe mutex for a session file.
func lockFor(dir, id string) *sync.Mutex {
	k := liveKey(dir, id)
	h := fnvHash(k)
	return &sessionLocks[h%sessionLockStripes]
}

// fnvHash is FNV-1a 32-bit over the session key for stripe selection.
func fnvHash(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}

// liveWriterForLocked returns the worker for a session, starting it lazily.
// Callers hold liveRegistryMu.
func liveWriterForLocked(dir, id string) *liveWriter {
	k := liveKey(dir, id)
	if w, ok := liveRegistry[k]; ok {
		return w
	}
	w := &liveWriter{dir: dir, id: id, notify: make(chan struct{}, 1)}
	liveRegistry[k] = w
	go w.run()
	return w
}

// SaveAsync enqueues a live snapshot of id for background persistence and
// returns immediately. It resolves the process-default storage dir (the TUI
// path); multi-project servers use SaveAsyncForDir. Snapshots use live
// (never-regress) semantics: title "" keeps the stored title, metadata nil
// keeps the stored metadata, and a snapshot older than what is already on
// disk is a no-op. The synchronous Save at turn end remains authoritative.
//
// The snapshot is deep-copied before returning, so later agent mutations of
// the caller's slice cannot race the background SQLite marshal.
func SaveAsync(id string, title string, messages []agent.Message, metadata map[string]any) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}
	return saveAsyncToDir(dir, id, title, messages, metadata)
}

// SaveAsyncForDir enqueues a live snapshot for the session owned by
// projectRoot and returns immediately. projectRoot is a project root (not a
// storage dir): it is resolved via GetStorageDirForPath exactly like
// SaveForDir, so the two always address the same file.
func SaveAsyncForDir(projectRoot, id string, title string, messages []agent.Message, metadata map[string]any) error {
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return err
	}
	return saveAsyncToDir(dir, id, title, messages, metadata)
}

// saveAsyncToDir is the shared enqueue core once the storage dir is known.
func saveAsyncToDir(dir, id string, title string, messages []agent.Message, metadata map[string]any) error {
	if id == "" || len(messages) == 0 {
		return nil
	}
	msgs := cloneMessages(messages)
	meta, err := cloneMetadata(metadata)
	if err != nil {
		return fmt.Errorf("session: clone metadata for live write %s: %w", id, err)
	}
	// Capture the history generation the snapshot is taken against. A
	// synchronous shrink landing between here and the worker's transaction
	// bumps the stored generation, so the write mismatches and drops
	// instead of resurrecting compacted history.
	gen, err := readHistoryGen(dir, id)
	if err != nil {
		return fmt.Errorf("session: read history gen for live write %s: %w", id, err)
	}
	liveRegistryMu.Lock()
	w := liveWriterForLocked(dir, id)
	w.mu.Lock()
	w.seq++
	w.pending = &liveReq{seq: w.seq, gen: gen, title: title, msgs: msgs, meta: meta}
	w.mu.Unlock()
	liveRegistryMu.Unlock()
	select {
	case w.notify <- struct{}{}:
	default:
	}
	return nil
}

// saveToDirLive performs one live (never-regress) write to an already-
// resolved storage dir. It funnels into the unified saveToDir path, which
// serializes in-process writers itself.
func saveToDirLive(dir, id string, title string, messages []agent.Message, metadata map[string]any, gen int64) error {
	return saveToDir(dir, id, title, messages, metadata, true, gen)
}

// markLiveCovered records that an authoritative synchronous save made the
// session's state durable through the latest enqueue, satisfying queued
// barriers with success. It runs after Save/SaveForDir succeed: the sync
// transcript supersedes every queued live snapshot, so even a shutdown
// FlushAll with no further live writes completes instead of timing out.
func markLiveCovered(dir, id string) {
	liveRegistryMu.Lock()
	w, ok := liveRegistry[liveKey(dir, id)]
	if !ok {
		liveRegistryMu.Unlock()
		return
	}
	w.mu.Lock()
	if w.seq > w.coveredOKSeq {
		w.coveredOKSeq = w.seq
	}
	fire := w.collectFiredLocked()
	w.mu.Unlock()
	liveRegistryMu.Unlock()
	for _, f := range fire {
		f.done <- f.err
	}
}

// cloneMessages deep-copies a transcript snapshot so the background writer's
// JSON marshal cannot race later agent mutations of the caller's slices,
// maps, or pointers.
func cloneMessages(messages []agent.Message) []agent.Message {
	cp := make([]agent.Message, len(messages))
	for i, m := range messages {
		cp[i] = cloneMessage(m)
	}
	return cp
}

func cloneMessage(m agent.Message) agent.Message {
	if m.Images != nil {
		images := make([]agent.Image, len(m.Images))
		copy(images, m.Images)
		m.Images = images
	}
	if m.ToolCalls != nil {
		calls := make([]agent.ToolCall, len(m.ToolCalls))
		copy(calls, m.ToolCalls)
		m.ToolCalls = calls
	}
	if m.OpenAIResponseItems != nil {
		items := make([]map[string]interface{}, len(m.OpenAIResponseItems))
		for i, item := range m.OpenAIResponseItems {
			if item == nil {
				continue
			}
			dup := make(map[string]interface{}, len(item))
			for k, v := range item {
				dup[k] = v
			}
			items[i] = dup
		}
		m.OpenAIResponseItems = items
	}
	if m.Usage != nil {
		usage := *m.Usage
		m.Usage = &usage
	}
	if m.Spend != nil {
		spend := *m.Spend
		m.Spend = &spend
	}
	return m
}

// cloneMetadata deep-copies session metadata via a JSON round-trip — the same
// encoding the write itself uses, so the copy is lossless with respect to
// what reaches disk. A nil input stays nil (keep-existing semantics).
func cloneMetadata(metadata map[string]any) (map[string]any, error) {
	if metadata == nil {
		return nil, nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	var cp map[string]any
	if err := json.Unmarshal(raw, &cp); err != nil {
		return nil, err
	}
	return cp, nil
}

// Flush blocks until id's queued live snapshots are durable. It returns the
// latest write's error, or a timeout error — never a false success.
func Flush(id string, timeout time.Duration) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}
	return flushDir(dir, id, timeout)
}

// FlushForDir blocks until the session owned by projectRoot has its queued
// live snapshots durable. It returns the latest write's error, or a timeout
// error — never a false success.
func FlushForDir(projectRoot, id string, timeout time.Duration) error {
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return err
	}
	return flushDir(dir, id, timeout)
}

// flushDir is the shared drain core once the storage dir is known.
func flushDir(dir, id string, timeout time.Duration) error {
	// The barrier is appended while holding liveRegistryMu (the same order
	// tryRetire uses), so a worker cannot retire between the lookup and the
	// append and strand the barrier on a dead worker. The barrier records
	// the current enqueue seq, so it completes only once writes covering
	// everything queued so far have finished — never before a pending write
	// it was meant to wait for.
	liveRegistryMu.Lock()
	w, ok := liveRegistry[liveKey(dir, id)]
	var done chan error
	if ok {
		w.mu.Lock()
		done = make(chan error, 1)
		w.barriers = append(w.barriers, flushBarrier{seq: w.seq, done: done})
		w.mu.Unlock()
	}
	liveRegistryMu.Unlock()
	if !ok {
		return nil
	}
	select {
	case w.notify <- struct{}{}:
	default:
	}
	if timeout <= 0 {
		return <-done
	}
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("session: flush %s timed out after %v", id, timeout)
	}
}

// FlushAll blocks until every known session worker is drained. It returns the
// first write or timeout error encountered, if any. Used by exit paths so a
// quit immediately after a turn never drops the turn's live writes. It loops
// to quiescence: a turn still unwinding during shutdown may enqueue after a
// pass snapshots, so passes repeat while any worker still holds pending
// work (bounded by timeout).
func FlushAll(timeout time.Duration) error {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	remaining := func() time.Duration {
		if timeout <= 0 {
			return 0
		}
		return time.Until(deadline)
	}
	for {
		// Snapshot only writers holding pending work; idle registered
		// workers need no barrier.
		liveRegistryMu.Lock()
		var active []*liveWriter
		for _, w := range liveRegistry {
			w.mu.Lock()
			if w.pending != nil {
				active = append(active, w)
			}
			w.mu.Unlock()
		}
		liveRegistryMu.Unlock()
		if len(active) == 0 {
			return nil
		}
		if timeout > 0 && remaining() <= 0 {
			return fmt.Errorf("session: flush-all timed out after %v", timeout)
		}
		for _, w := range active {
			// Re-resolve under the registry lock: a worker may have retired
			// since the snapshot above — appending to a dead worker would
			// strand the barrier. tryRetire cannot delete a worker that has a
			// queued barrier, so a registered worker stays alive past this.
			// The barrier records the worker's current enqueue seq (see
			// flushDir) so it waits for everything queued so far.
			liveRegistryMu.Lock()
			current, ok := liveRegistry[liveKey(w.dir, w.id)]
			var done chan error
			if ok && current == w {
				w.mu.Lock()
				done = make(chan error, 1)
				w.barriers = append(w.barriers, flushBarrier{seq: w.seq, done: done})
				w.mu.Unlock()
			}
			liveRegistryMu.Unlock()
			if !ok || current != w {
				continue
			}
			select {
			case w.notify <- struct{}{}:
			default:
			}
			if timeout <= 0 {
				if err := <-done; err != nil {
					return err
				}
				continue
			}
			if remaining() <= 0 {
				return fmt.Errorf("session: flush-all timed out after %v", timeout)
			}
			select {
			case err := <-done:
				if err != nil {
					return err
				}
			case <-time.After(remaining()):
				return fmt.Errorf("session: flush-all timed out after %v", timeout)
			}
		}
	}
}

// run is the worker loop: take the latest pending snapshot, write it with
// live semantics, repeat while snapshots keep arriving, then deliver the
// write result to any drain barriers queued so far. Idle workers retire
// themselves (see tryRetire) so a long-lived process does not accumulate one
// goroutine per session ever touched.
func (w *liveWriter) run() {
	idle := time.NewTimer(liveIdleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-w.notify:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(liveIdleTimeout)
			w.drain()
		case <-idle.C:
			if w.tryRetire() {
				return
			}
			idle.Reset(liveIdleTimeout)
		}
	}
}

// firedBarrier is a barrier due for delivery with its decided outcome.
type firedBarrier struct {
	done chan error
	err  error
}

// drain writes pending snapshots until none remain, delivering queued
// barriers as their seqs become covered. The pending take and the barrier
// partition happen atomically, so a barrier registered just before the take
// is never completed before the pending write it was meant to wait for —
// the race that sequence numbers close.
func (w *liveWriter) drain() {
	for {
		w.mu.Lock()
		req := w.pending
		w.pending = nil
		fire := w.collectFiredLocked()
		w.mu.Unlock()
		for _, f := range fire {
			f.done <- f.err
		}
		if req == nil {
			// Either everything is delivered, or the leftovers wait for
			// future writes — which always arrive with a notify wake, so
			// returning cannot strand them.
			return
		}
		// A newer snapshot may have arrived while a previous write was in
		// flight; the loop picks it up next iteration instead of letting a
		// superseded intermediate snapshot hit the disk.
		werr := saveToDirLive(w.dir, w.id, req.title, req.msgs, req.meta, req.gen)
		if werr != nil {
			log.Printf("session: live write %s: %v", w.id, werr)
		}
		w.mu.Lock()
		if req.seq > w.writtenSeq {
			w.writtenSeq = req.seq
		}
		if werr == nil {
			// The snapshot covers everything enqueued up to its seq.
			if req.seq > w.coveredOKSeq {
				w.coveredOKSeq = req.seq
			}
		} else {
			w.lastFailErr = werr
		}
		fire = w.collectFiredLocked()
		w.mu.Unlock()
		for _, f := range fire {
			f.done <- f.err
		}
	}
}

// collectFiredLocked splits queued barriers into those whose seq is covered
// now versus those still waiting, snapshotting each fired barrier's outcome.
// A barrier covered by a successful write (or an authoritative sync save via
// markLiveCovered) succeeds; one covered only by failed live writes reports
// the latest failure. Call with w.mu held.
func (w *liveWriter) collectFiredLocked() []firedBarrier {
	var fire []firedBarrier
	rest := w.barriers[:0]
	for _, b := range w.barriers {
		switch {
		case b.seq <= w.coveredOKSeq:
			fire = append(fire, firedBarrier{done: b.done})
		case b.seq <= w.writtenSeq:
			fire = append(fire, firedBarrier{done: b.done, err: w.lastFailErr})
		default:
			rest = append(rest, b)
		}
	}
	w.barriers = rest
	return fire
}

// tryRetire removes an idle worker from the registry. It reports whether the
// caller should exit. The registry lock precedes the worker lock (the same
// order SaveAsync uses), so a snapshot enqueued concurrently is always
// observed: either retirement sees it and aborts, or the enqueue lands on a
// worker that is still registered.
func (w *liveWriter) tryRetire() bool {
	liveRegistryMu.Lock()
	defer liveRegistryMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending != nil || len(w.barriers) != 0 {
		return false
	}
	if liveRegistry[liveKey(w.dir, w.id)] != w {
		return true
	}
	delete(liveRegistry, liveKey(w.dir, w.id))
	return true
}
