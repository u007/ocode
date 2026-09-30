package reminders

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/scheduler"
)

// ErrNotFound is returned by every lookup that addresses a single item.
var ErrNotFound = errors.New("reminders: item not found")

// ErrNotRunning is returned by the one-shot FireNow entry point when the
// engine has no delivery wiring attached, so nothing could be delivered.
var ErrNotRunning = errors.New("reminders: service is not started")

// Service owns the persisted items for one project and the loop that fires
// them. The concurrency model mirrors internal/scheduler.Service: a single
// mutex guards both the slice and persistence, and every mutation ends with
// persistLocked + wake so the run loop recomputes the soonest due time at once.
type Service struct {
	mu        sync.Mutex
	items     []Item
	storePath string
	loadedAt  time.Time
	workDir   string

	now func() time.Time

	notifier Notifier
	runner   AgentRunner
	outbox   OutboxAppender
	runs     RunRecorder

	wakeCh chan struct{}
	stopCh chan struct{}
	done   chan struct{}
	start  sync.Once
	stop   sync.Once
	// running is true once the loop is live. FireNow refuses to deliver when
	// it is false, because a "run now" that cannot reach the outbox, the
	// notifier, or the agent is a silent no-op.
	running bool
	// loadErr records a failed load so Start's caller can see it: the loop
	// runs in a goroutine and cannot return an error.
	loadErr error
}

// OutboxAppender is the slice of scheduler.Outbox this package needs. It is an
// interface so the tests can capture deliveries without touching the disk, and
// so this package does not have to name scheduler.Outbox in its API.
type OutboxAppender interface {
	Append(d scheduler.Delivery) error
}

// RunRecorder is the slice of scheduler.RunHistory this package needs: a
// per-item run log, so a reminder or task shows a history panel exactly like a
// cron job does.
type RunRecorder interface {
	Append(rec scheduler.RunRecord) error
}

// NewService returns a Service reading and writing storePath. It does not
// touch the disk and does not start the loop — call Start, or use
// StartForHost, which also wires the delivery sinks.
func NewService(storePath string) *Service {
	return &Service{
		storePath: storePath,
		now:       time.Now,
		wakeCh:    make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		done:      make(chan struct{}),
		items:     []Item{},
	}
}

// SetClock overrides the clock. Tests only.
func (s *Service) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// SetNotifier attaches the plain-notification sink. A firing with
// ActionNotify calls it in addition to writing the delivery log.
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// SetAgentRunner attaches the agent-turn sink. A firing with ActionAgent
// delegates to it; without one, such an item records an error instead of
// silently doing nothing.
func (s *Service) SetAgentRunner(r AgentRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runner = r
}

// SetOutbox attaches the shared delivery log. Passing the SAME
// scheduler.Outbox the cron service uses is the point: one JSONL, one
// drainer, one Telegram/RC fan-out, one web Outbox panel.
func (s *Service) SetOutbox(o OutboxAppender) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outbox = o
}

// SetRunHistory attaches the shared per-job run history.
func (s *Service) SetRunHistory(r RunRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = r
}

// ListFilter narrows a List call. The zero value means "every item, first
// page".
type ListFilter struct {
	// Kind, when set, keeps only reminders or only tasks.
	Kind Kind
	// Status, when set, keeps only that status.
	Status Status
	// Limit is the page size. Zero uses DefaultPageSize; a negative value
	// means "no paging" and is clamped to MaxPageSize so a caller cannot pull
	// the whole store by accident.
	Limit int
	// Offset is the number of items to skip, applied AFTER sorting and
	// filtering, so paging is stable.
	Offset int
}

const (
	// DefaultPageSize is the page size when ListFilter.Limit is zero.
	DefaultPageSize = 50
	// MaxPageSize caps an explicit page size.
	MaxPageSize = 200
)

func (f ListFilter) pageSize() int {
	switch {
	case f.Limit == 0:
		return DefaultPageSize
	case f.Limit < 0:
		return MaxPageSize
	case f.Limit > MaxPageSize:
		return MaxPageSize
	default:
		return f.Limit
	}
}

// List returns a sorted, filtered page of items plus the TOTAL number matching
// the filter (not the page), so the UI can render "showing 50 of 137".
//
// Sort order is overdue/due-first by due time, with undated items last, then
// creation time, then id:
//
//  1. items WITH a due time, ascending (so the next thing to happen is first
//     and an overdue item sorts to the very top)
//  2. items WITHOUT a due time, ascending by created_at_ms
//  3. ties broken by id so the order is total and paging cannot repeat or skip
//     an item when two share a due time
func (s *Service) List(f ListFilter) ([]Item, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]Item, 0, len(s.items))
	for i := range s.items {
		it := s.items[i]
		if f.Kind != "" && it.Kind != f.Kind {
			continue
		}
		if f.Status != "" && it.Status != f.Status {
			continue
		}
		filtered = append(filtered, it)
	}
	sort.SliceStable(filtered, func(a, b int) bool {
		return lessItem(filtered[a], filtered[b])
	})

	total := len(filtered)
	start := f.Offset
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start + f.pageSize()
	if end > total {
		end = total
	}
	page := filtered[start:end]
	if page == nil {
		page = []Item{}
	}
	return page, total
}

// lessItem is the single total order used by List. Exported behaviour is
// pinned by TestListSortsDueFirstThenUndated.
func lessItem(a, b Item) bool {
	if (a.DueAtMs > 0) != (b.DueAtMs > 0) {
		return a.DueAtMs > 0
	}
	if a.DueAtMs > 0 && a.DueAtMs != b.DueAtMs {
		return a.DueAtMs < b.DueAtMs
	}
	if a.CreatedAtMs != b.CreatedAtMs {
		return a.CreatedAtMs < b.CreatedAtMs
	}
	return a.ID < b.ID
}

// Get returns a copy of one item, or ErrNotFound.
func (s *Service) Get(id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.indexLocked(id)
	if idx < 0 {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return s.items[idx], nil
}

// Add validates and stores a new item, returning its assigned id. The
// incoming item's Status is forced to StatusPending: a new item cannot be
// created already-completed, because "create it done" is a delete.
func (s *Service) Add(it Item) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) >= maxItems {
		return "", fmt.Errorf("reminder/task limit (%d) reached", maxItems)
	}
	it.ID = s.genIDLocked()
	it.Status = StatusPending
	it.CreatedAtMs = s.now().UnixMilli()
	it.UpdatedAtMs = it.CreatedAtMs
	if err := it.validate(); err != nil {
		return "", err
	}
	it.Owner = s.defaultOwner(it.Owner)
	s.items = append(s.items, it)
	if err := s.persistLocked(); err != nil {
		// Roll the append back so a failed write cannot leave an item that
		// only exists in memory and vanishes on the next load.
		s.items = s.items[:len(s.items)-1]
		return "", err
	}
	s.wake()
	return it.ID, nil
}

// ItemPatch is a partial update. Only non-nil fields are applied, so a PATCH
// that changes one field cannot silently reset the others. Status is NOT
// patchable here — it goes through SetStatus so the transition table applies.
type ItemPatch struct {
	Title        *string
	Message      *string
	Notes        *string
	Owner        *string
	Action       *Action
	AutoComplete *bool
	DueAtMs      *int64
	PermMode     *scheduler.PermissionMode
}

// Update applies a patch and returns the updated item. The item is left
// unmodified on error.
func (s *Service) Update(id string, patch ItemPatch) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.indexLocked(id)
	if idx < 0 {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	// Build and validate the CANDIDATE on a copy first, so a rejected patch
	// cannot leave a half-applied item behind.
	candidate := s.items[idx]
	if patch.Title != nil {
		candidate.Title = *patch.Title
	}
	if patch.Message != nil {
		candidate.Message = *patch.Message
	}
	if patch.Notes != nil {
		candidate.Notes = *patch.Notes
	}
	if patch.Owner != nil {
		candidate.Owner = *patch.Owner
	}
	if patch.Action != nil {
		candidate.Action = *patch.Action
	}
	if patch.AutoComplete != nil {
		candidate.AutoComplete = *patch.AutoComplete
	}
	if patch.DueAtMs != nil {
		candidate.DueAtMs = *patch.DueAtMs
	}
	if patch.PermMode != nil {
		candidate.PermMode = *patch.PermMode
	}
	candidate.UpdatedAtMs = s.now().UnixMilli()
	if err := candidate.validate(); err != nil {
		return Item{}, err
	}
	if patch.Owner == nil {
		candidate.Owner = s.defaultOwner(candidate.Owner)
	}

	prev := s.items[idx]
	s.items[idx] = candidate
	if err := s.persistLocked(); err != nil {
		s.items[idx] = prev
		return Item{}, err
	}
	// A due time can move into the past, so the loop must recompute.
	s.wake()
	return s.items[idx], nil
}

// SetStatus moves an item to to, enforcing the transition table.
//
// Three things happen here that a naive assignment would get wrong:
//
//  1. Same-status is an IDEMPOTENT NO-OP, not an error. A retried PATCH, or a
//     double-clicked button, must not surface a failure. The strict
//     Transition() check is reserved for real moves.
//  2. Moving to pending — including already being pending — clears FiredAtMs
//     and the last run's outcome. A one-shot item is gated on FiredAtMs == 0,
//     so without this an unmarked reminder could never ring again, and a
//     reopened task would keep showing a stale error next to a fresh
//     "pending". This also makes the UI's "Reopen" button work on a task that
//     fired and stayed pending: the status is unchanged, but the item is
//     re-armed and genuinely pending again.
//  3. A cancelled or completed item never fires again because Item.Due()
//     refuses those statuses, and the auto-complete path re-checks the status
//     under the same mutex before writing completed — so a cancel that lands
//     while an agent turn is in flight is never overwritten.
func (s *Service) SetStatus(id string, to Status) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.indexLocked(id)
	if idx < 0 {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	cur := &s.items[idx]
	sameState := cur.Status == to
	if !sameState {
		if err := Transition(cur.Status, to); err != nil {
			return Item{}, err
		}
	}
	if sameState && to != StatusPending {
		// Nothing to re-arm and nothing to change.
		return *cur, nil
	}
	prev := *cur
	cur.Status = to
	cur.UpdatedAtMs = s.now().UnixMilli()
	if to == StatusPending {
		cur.FiredAtMs = 0
		cur.LastStatus = ""
		cur.LastError = ""
	}
	if err := s.persistLocked(); err != nil {
		s.items[idx] = prev
		return Item{}, err
	}
	s.wake()
	return s.items[idx], nil
}

// Remove deletes an item. Firing and a delete can race — the run loop holds a
// pointer into the slice — so Remove re-checks the index under the mutex and
// is the only place the slice shrinks.
func (s *Service) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.indexLocked(id)
	if idx < 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	prev := s.items
	s.items = append(s.items[:idx], s.items[idx+1:]...)
	if err := s.persistLocked(); err != nil {
		s.items = prev
		return err
	}
	s.wake()
	return nil
}

// indexLocked returns the slice index of id, or -1. Caller must hold mu.
func (s *Service) indexLocked(id string) int {
	if id == "" {
		return -1
	}
	for i := range s.items {
		if s.items[i].ID == id {
			return i
		}
	}
	return -1
}

// Start loads the store and launches the run loop. It is safe to call more
// than once; the second call is a no-op that returns the same result.
//
// A load failure is returned, not swallowed, and the loop does NOT start: a
// corrupt or unreadable store is the one thing a reminder file must never
// recover from by resetting itself, because that would destroy the user's
// list. The host logs the error and runs without reminders, exactly as it
// does for the cron scheduler.
func (s *Service) Start() error {
	s.start.Do(func() {
		if err := s.load(); err != nil {
			// No loop will run, so close done immediately: Stop must not hang
			// waiting for a goroutine that was never launched.
			close(s.done)
			return
		}
		s.mu.Lock()
		s.running = true
		s.mu.Unlock()
		go s.run()
	})
	return s.startErr()
}

// startErr reports a load failure recorded by Start. A goroutine cannot
// return an error, so the failure is stashed and re-read here.
func (s *Service) startErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

func (s *Service) run() {
	defer close(s.done)
	// Safety ticker: a throttled VM or a wall-clock change must not let the
	// timer sleep through a due time. Matches scheduler.maxPollLeg.
	safety := time.NewTicker(maxPollLeg)
	defer safety.Stop()
	for {
		s.mu.Lock()
		delay := s.nextDelayLocked()
		s.mu.Unlock()

		if delay <= 0 {
			select {
			case <-s.stopCh:
				return
			default:
			}
			s.tick()
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-s.stopCh:
			timer.Stop()
			return
		case <-s.wakeCh:
			timer.Stop()
		case <-timer.C:
			s.tick()
		case <-safety.C:
			s.tick()
		}
	}
}

// nextDelayLocked returns how long until the soonest armed item, or idleDelay
// when nothing is armed. Caller must hold mu.
//
// It uses Item.Active(), the SAME predicate Item.Due uses. That is not tidiness,
// it is a liveness requirement: this function returning 0 tells run() to tick
// immediately, and tick can only make progress by claiming something Due().
// If the two ever disagreed about which statuses are live, an item could be
// permanently "due" to the delay computation and permanently refused by the
// firing gate — and run() would then loop tick/continue at full CPU forever.
// TestNextDelayAgreesWithDue pins it.
//
// An item whose due time is far in the future is not "soon", so the returned
// delay is capped at maxPollLeg and the safety ticker re-checks. Without the
// cap a 2030 reminder would park the loop for years, and a suspend/resume or
// clock change would miss the fire entirely.
func (s *Service) nextDelayLocked() time.Duration {
	now := s.now().UnixMilli()
	soonest := int64(0)
	for i := range s.items {
		it := &s.items[i]
		// An unfired item only: one that already fired is not due again, and
		// one that never will be (no due date) is not a candidate at all.
		if !it.Active() || it.DueAtMs == 0 || it.FiredAtMs != 0 {
			continue
		}
		if soonest == 0 || it.DueAtMs < soonest {
			soonest = it.DueAtMs
		}
	}
	if soonest == 0 {
		return idleDelay
	}
	d := time.Duration(soonest-now) * time.Millisecond
	if d < 0 {
		return 0
	}
	if d > maxPollLeg {
		return maxPollLeg
	}
	return d
}

// tick finds every armed, due item and fires it. Firing happens OUTSIDE the
// lock (an agent turn can take minutes) against a snapshot, and fireOnce
// re-checks the item's state under the lock before it records anything.
func (s *Service) tick() {
	s.syncFromDisk()
	now := s.now().UnixMilli()

	s.mu.Lock()
	snapshots := make([]Item, 0, len(s.items))
	for i := range s.items {
		if s.items[i].Due(now) {
			snapshots = append(snapshots, s.items[i])
		}
	}
	s.mu.Unlock()

	for i := range snapshots {
		s.fireOnce(&snapshots[i])
	}
}

// FireNow fires one item immediately, bypassing the due-time gate, and is what
// the "Remind me now" / "Run now" button calls. The item must still exist;
// a cancelled or completed item is refused, because firing it would deliver a
// notification the user explicitly turned off.
func (s *Service) FireNow(id string) (Item, error) {
	s.mu.Lock()
	idx := s.indexLocked(id)
	if idx < 0 {
		s.mu.Unlock()
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if s.items[idx].Status == StatusCompleted || s.items[idx].Status == StatusCancelled {
		st := s.items[idx].Status
		s.mu.Unlock()
		return Item{}, fmt.Errorf("cannot run a %s item", st)
	}
	snapshot := s.items[idx]
	runner, notifier, outbox, runs := s.runner, s.notifier, s.outbox, s.runs
	running := s.running
	s.mu.Unlock()

	if !running {
		return Item{}, ErrNotRunning
	}
	s.fireWith(&snapshot, runner, notifier, outbox, runs)
	return s.Get(id)
}

// Stop signals the loop to exit and waits for it.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stop.Do(func() { close(s.stopCh) })
	<-s.done
}

// wake non-blockingly signals the loop to recompute the soonest due time.
func (s *Service) wake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

// genIDLocked returns a new unique 8-char hex id. Caller must hold mu.
func (s *Service) genIDLocked() string {
	seen := make(map[string]bool, len(s.items))
	for i := range s.items {
		seen[s.items[i].ID] = true
	}
	for {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			// Fallback to a time-derived id if the system RNG is unavailable;
			// the uniqueness check below still applies.
			return fmt.Sprintf("%08x", s.now().UnixNano()&0xFFFFFFFF)
		}
		id := hex.EncodeToString(b)[:idLen]
		if !seen[id] {
			return id
		}
	}
}

// load reads the store from disk. A missing file is an empty list, not an
// error. A stored item the current build cannot interpret — an unknown status,
// for instance — is a hard error naming the item, never a silent reset.
func (s *Service) load() error {
	store, err := readStore(s.storePath)
	if err != nil {
		s.setLoadErr(err)
		return err
	}
	for i := range store.Items {
		it := &store.Items[i]
		if !ValidStatus(it.Status) {
			err := fmt.Errorf("reminders: item %s has unknown status %q in %s", it.ID, it.Status, s.storePath)
			s.setLoadErr(err)
			return err
		}
	}
	now := s.now()
	s.mu.Lock()
	s.items = store.Items
	s.loadErr = nil
	if info, err := os.Stat(s.storePath); err == nil {
		s.loadedAt = info.ModTime()
	} else {
		s.loadedAt = now
	}
	s.mu.Unlock()
	return nil
}

// setLoadErr records a load failure under the mutex.
func (s *Service) setLoadErr(err error) {
	s.mu.Lock()
	s.loadErr = err
	s.mu.Unlock()
}

// syncFromDisk reloads the store when another process (the TUI, or a second
// ocode instance on the same project) has rewritten it. In-memory runtime
// state for items that still exist is preserved; items the other process added
// are adopted; items it removed are dropped. Mirrors scheduler.syncFromDisk.
func (s *Service) syncFromDisk() {
	s.mu.Lock()
	if s.loadErr != nil {
		s.mu.Unlock()
		return
	}
	info, err := os.Stat(s.storePath)
	if err != nil || !info.ModTime().After(s.loadedAt) {
		s.mu.Unlock()
		return
	}
	store, rerr := readStore(s.storePath)
	if rerr != nil {
		s.mu.Unlock()
		return
	}
	prev := make(map[string]Item, len(s.items))
	for _, it := range s.items {
		prev[it.ID] = it
	}
	merged := make([]Item, 0, len(store.Items))
	for i := range store.Items {
		disk := store.Items[i]
		if mine, ok := prev[disk.ID]; ok {
			// Keep the in-memory run bookkeeping (which the other process has
			// no way to know about) while adopting every authored field.
			disk.Runs = mine.Runs
			disk.FiredAtMs = mine.FiredAtMs
			disk.LastStatus = mine.LastStatus
			disk.LastError = mine.LastError
		}
		merged = append(merged, disk)
	}
	s.items = merged
	s.loadedAt = info.ModTime()
	s.mu.Unlock()
}

func readStore(path string) (Store, error) {
	var store Store
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{Version: 1, Items: []Item{}}, nil
		}
		return store, err
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return store, fmt.Errorf("corrupt reminders store %s: %w", path, err)
	}
	if store.Items == nil {
		store.Items = []Item{}
	}
	return store, nil
}

// persistLocked writes the store atomically (temp file + rename) and records
// the new mtime so syncFromDisk does not reload our own write. Caller must
// hold mu.
func (s *Service) persistLocked() error {
	store := Store{Version: 1, Items: s.items}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.storePath), 0o755); err != nil {
		return err
	}
	tmp := s.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.storePath); err != nil {
		return err
	}
	if info, err := os.Stat(s.storePath); err == nil {
		s.loadedAt = info.ModTime()
	}
	return nil
}

// siblingPath returns a filename in the same directory as path.
func siblingPath(path, name string) string {
	return filepath.Join(filepath.Dir(path), name)
}

// prefixID namespaces an item id for the SHARED delivery log and run history.
// The cron store uses bare 8-hex ids, so a reminder id alone could collide
// with a cron job id in the same outbox and make the Telegram sink resolve the
// wrong job. The prefix is part of the on-the-wire id, never the stored one.
func prefixID(k Kind, id string) string {
	return string(k) + ":" + id
}

// trimForLog keeps a log line bounded. Returns a single line, so a
// multi-line agent result cannot forge extra log records.
func trimForLog(s string) string {
	const max = 200
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
