// Package notebus implements the shared-agent notes bus: a per-group,
// append-only, goroutine-owned log of cross-agent findings (notes),
// write-touches, and resolves. It is agent-agnostic — no imports from
// internal/agent — so it stays unit-testable in isolation.
//
// Concurrency model: a single owner goroutine (started by Start) reads
// append requests on a channel, assigns monotonic sequence numbers, and
// appends to the in-memory log. Readers (Snapshot, Delta) route through
// the same owner via a request/response channel so they never touch
// mutable state held by a writer (share-by-communicating, no shared
// mutex on the log).
//
// Persistence: the owner also drives an append-only sidecar file (one
// JSON object per line) via a PersistSink interface. The in-memory
// append is synchronous and microsecond-cheap; disk flushes may be
// buffered/async and never block seq assignment.
package notebus

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Kind is the wire-format discriminator for an Entry. The string values
// ("note", "touch", "resolve") appear verbatim in the injected
// <oc-log>…</oc-log> block in the agent's prompt, and the parser in
// parse.go matches against these exact strings. Do not change them
// without auditing the rest of the package and the agent loop.
type Kind string

const (
	// KindNote is an intentional cross-agent finding authored by an agent.
	// The agent supplies At (anchor) and Body; the bus fills Seq and By.
	KindNote Kind = "note"
	// KindTouch is an automatic write-event derived by the bus from a
	// successful write/edit/apply_patch tool call. Agents never emit it.
	KindTouch Kind = "touch"
	// KindResolve marks a prior note (Ref) as superseded. Resolve entries
	// are never injected into Delta; they are kept in the log so the
	// reconciler can audit what was dropped and why.
	KindResolve Kind = "resolve"
	// KindUnknown is the zero value used by ParseKind for unknown strings.
	KindUnknown Kind = ""
)

// BriefAuthor is the reserved By value for orchestrator-seeded brief
// entries (change-set summary, partition assignments, rule digest).
// These are shared context, not agent findings, so the reconcile
// pre-pass excludes them from clustering.
const BriefAuthor = "main"

// ParseKind maps the on-wire string to its Kind constant. Unknown inputs
// (including the empty string) yield KindUnknown, which is not a valid
// entry kind and is rejected by Append.
func ParseKind(s string) Kind {
	switch Kind(s) {
	case KindNote, KindTouch, KindResolve:
		return Kind(s)
	default:
		return KindUnknown
	}
}

// Entry is one stamped line in the bus. Fields are populated by the
// per-kind constructors (Note, Touch, Resolve) so the bus cannot
// accidentally store a touch with a body or a note with a file. Seq is
// assigned by the owner goroutine — do not set it from the caller.
type Entry struct {
	Seq  int64
	By   string
	Kind Kind
	TS   int64 // unix seconds; injected (the bus never reads a wall clock)

	// Note fields
	At   string // anchor — symbol or snippet, never bare line
	Body string // caveman-concise plain text, < and > entity-encoded

	// Touch fields
	File string // file path touched
	Act  string // action — currently always "edit"

	// Resolve field
	Ref int64 // Seq of the note this entry supersedes
}

// Note constructs a note entry. Seq is assigned by the bus — pass 0; the
// owner will overwrite it.
func Note(seq int64, by, at, body string, ts int64) Entry {
	return Entry{Seq: seq, By: by, Kind: KindNote, At: at, Body: body, TS: ts}
}

// Touch constructs a touch entry. Touches are only emitted by the bus
// itself (from write-class tool results) — callers outside the package
// should never construct one.
func Touch(seq int64, by, file, act string, ts int64) Entry {
	return Entry{Seq: seq, By: by, Kind: KindTouch, File: file, Act: act, TS: ts}
}

// Resolve constructs a resolve entry that marks the note at Ref as
// superseded. Ref is the seq of the note being resolved.
func Resolve(seq int64, by string, ref int64, ts int64) Entry {
	return Entry{Seq: seq, By: by, Kind: KindResolve, Ref: ref, TS: ts}
}

// PersistSink writes a stamped entry to durable storage. The owner
// goroutine calls Write(entry) after assigning Seq and appending to the
// in-memory log. Implementations must not block the caller's critical
// path — the bus guarantees a fast no-op Write in the common case (the
// in-memory append has already happened). A nil sink means no
// persistence.
type PersistSink interface {
	Write(Entry) error
	Close() error
}

// CapError indicates the soft per-group entry cap was hit. The bus
// stops accepting appends and the caller is expected to surface this
// to the orchestrator so reconcile can flag the dropped tail.
type CapError struct {
	Cap int64
}

func (e *CapError) Error() string {
	return "notebus: entry cap reached"
}

// Bus is the per-group notes bus. Construct with NewBus, then call
// Start(ctx) to launch the owner goroutine. The bus is safe for
// concurrent use by many producers (one per child agent) and readers
// (Delta, Snapshot) once Start has returned.
type Bus struct {
	groupID string
	cap     int64 // 0 = no cap

	// State owned by the owner goroutine only.
	log   []Entry
	seq   int64
	wm    map[string]int64
	reslv map[int64]bool // seqs of resolved notes

	// Channel-serialized inputs.
	reqs   chan busRequest
	stopCh chan struct{}
	done   chan struct{}

	// PersistSink (optional). nil disables persistence.
	persist PersistSink

	// onCompletion, when non-nil, is called once per agent run
	// when the agent's group run ends. Wired by the parent agent's
	// parallel block to surface per-agent completion to the
	// orchestrator's reconcile code. nil means "no callback
	// wired" (the common case for non-grouped or test runs).
	onCompletion func(agentID, status string, err error)
	onCompMu     sync.Mutex

	// now is the timestamp source. The bus must NOT read a wall clock
	// from inside the goroutine; tests inject a function so they can pin
	// TS deterministically.
	now func() int64

	// redactor, when non-nil, is invoked on every Note
	// body's text BEFORE the entry is appended to the log.
	// The redactor returns a scrubbed body (and any
	// metadata); the bus stores the scrubbed form, so a
	// secret never reaches the log, the sidecar, the
	// delta, or the reconcile hand-off. nil means "no
	// redaction" (the default; production callers wire
	// redact.Detect). Touches and resolves have no body
	// and pass through unchanged.
	redactor func(text string) string

	startOnce sync.Once
	stopOnce  sync.Once

	// started flips true the first time Start runs. Append and the
	// read paths consult it so a never-started bus fails fast instead
	// of parking forever on a request channel that has no reader.
	started atomic.Bool
}

// busRequest is the message routed through reqs. Either a Write (carries
// the freshly-constructed entry) or a Read (Snapshot/Delta query
// carrying a response channel so the owner can hand the result back).
type busRequest struct {
	kind    busReqKind
	write   Entry
	forceTS int64 // if > 0, use this TS instead of now()
	reply   chan busResponse
	seqCh   chan int64 // for writes: the owner hands the assigned seq back here
}

type busReqKind int

const (
	reqWrite busReqKind = iota
	reqReadSnapshot
	reqReadDelta
)

type busResponse struct {
	snap []Entry // for Snapshot
	delt []Entry // for Delta
	head int64   // for both: current head seq, useful for callers that want to advance manually
}

// NewBus constructs a Bus with the given group id. It does NOT start
// the owner goroutine — call Start(ctx) to begin accepting appends.
// Snapshot/Delta/HeadSeq return empty and Append returns
// ErrBusNotStarted until Start has been called, so a never-started bus
// fails fast rather than blocking on a request channel with no reader.
func NewBus(groupID string) *Bus {
	return &Bus{
		groupID: groupID,
		wm:      make(map[string]int64),
		reslv:   make(map[int64]bool),
		reqs:    make(chan busRequest, 256),
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
		// Default: a real wall clock. Tests override.
		now: func() int64 { return time.Now().Unix() },
	}
}

// SetCap sets a soft cap on the number of entries the bus will accept.
// Once the cap is hit, Append returns *CapError and the bus stops
// accepting new entries. 0 (default) means no cap. The cap is read by
// the owner on every Append, so changing it via a setter after Start
// is allowed (no synchronization is needed — the owner is the only
// reader).
func (b *Bus) SetCap(n int64) { b.cap = n }

// SetNow overrides the timestamp source. Intended for tests; production
// callers should leave the default in place. Must be called before
// Start, since once the goroutine is running the package has no
// synchronization against reads of now.
func (b *Bus) SetNow(now func() int64) { b.now = now }

// SetPersist wires a persistence sink. Must be called before Start.
// nil disables persistence.
func (b *Bus) SetPersist(p PersistSink) { b.persist = p }

// SetRedactor installs a body redactor. The redactor is
// called on every Note body BEFORE the entry is appended;
// the bus stores the redactor's return value. nil disables
// redaction. The redactor is called from the bus owner
// goroutine, so it must not block on network or other
// shared state — tier-1 (regex) detectors are fine. The
// caller is responsible for choosing a redaction policy
// that does not leak the original text via error messages
// or side channels; the bus only stores the return value.
func (b *Bus) SetRedactor(fn func(text string) string) {
	b.redactor = fn
}

// SetOnCompletion registers a per-agent completion callback. The
// callback fires once per agent when the agent's group run ends.
// Passing nil clears the callback. Safe to call before or after
// Start. Used by the agent parallel block to surface per-agent
// status to the orchestrator's reconcile code.
func (b *Bus) SetOnCompletion(cb func(agentID, status string, err error)) {
	b.onCompMu.Lock()
	b.onCompletion = cb
	b.onCompMu.Unlock()
}

// fireCompletion invokes the completion callback, if any, outside
// any bus-internal lock so a slow or panicking callback cannot
// deadlock the owner goroutine.
func (b *Bus) fireCompletion(agentID, status string, err error) {
	b.onCompMu.Lock()
	cb := b.onCompletion
	b.onCompMu.Unlock()
	if cb == nil {
		return
	}
	// The callback is expected to be quick (it just records status
	// into a tracker). We do not recover from panics here — a
	// panicking callback indicates a bug in the caller, and the
	// panic should propagate so tests can catch it.
	cb(agentID, status, err)
}

// Start launches the owner goroutine. It is safe to call Start exactly
// once. Subsequent calls are no-ops. The goroutine exits when ctx is
// cancelled and all queued requests have drained, or when Stop is
// called.
func (b *Bus) Start(ctx context.Context) {
	b.startOnce.Do(func() {
		b.started.Store(true)
		go b.run(ctx)
	})
}

// Stop signals the owner goroutine to exit. The call returns
// immediately; callers that need to wait for the goroutine to drain
// should wait on Done(). Stop is idempotent.
func (b *Bus) Stop() {
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
}

// Done returns a channel closed when the owner goroutine has fully
// exited and any final persist sink has been closed.
func (b *Bus) Done() <-chan struct{} { return b.done }

// GroupID returns the immutable group id the bus was constructed with.
func (b *Bus) GroupID() string { return b.groupID }

// HeadSeq returns the seq that the next Append would receive. The
// answer is from a snapshot read, so a concurrent Append may have
// already moved it. Callers that need a tight bound should call Delta
// and inspect the entries directly.
func (b *Bus) HeadSeq() int64 {
	if !b.started.Load() {
		return 0
	}
	respCh := make(chan busResponse, 1)
	select {
	case b.reqs <- busRequest{kind: reqReadSnapshot, reply: respCh}:
	case <-b.stopCh:
		return 0
	}
	r := <-respCh
	return r.head
}

// Snapshot returns a copy of the entire log in seq order. The returned
// slice is owned by the caller and never observes later appends. Safe
// to call concurrently with Append.
func (b *Bus) Snapshot() []Entry {
	if !b.started.Load() {
		return nil
	}
	respCh := make(chan busResponse, 1)
	select {
	case b.reqs <- busRequest{kind: reqReadSnapshot, reply: respCh}:
	case <-b.stopCh:
		return nil
	}
	r := <-respCh
	if r.snap == nil {
		return nil
	}
	out := make([]Entry, len(r.snap))
	copy(out, r.snap)
	return out
}

// Delta returns the entries that the given agent has not yet seen
// (seq > lastSeen[agent]), excluding entries it authored itself
// (by != agent), and excluding resolved notes. The agent's lastSeen
// watermark is advanced to the current head (so its own entries are
// never re-considered).
//
// If the delta is empty, the returned slice has length 0 (may be nil).
// Callers must check length, not nil-ness.
func (b *Bus) Delta(agent string) []Entry {
	if !b.started.Load() {
		return nil
	}
	respCh := make(chan busResponse, 1)
	select {
	case b.reqs <- busRequest{kind: reqReadDelta, reply: respCh, write: Entry{By: agent}}:
	case <-b.stopCh:
		return nil
	}
	r := <-respCh
	if r.delt == nil {
		return nil
	}
	out := make([]Entry, len(r.delt))
	copy(out, r.delt)
	return out
}

// ReportCompletion is called by the parent (TaskTool) when an
// agent's group run ends. The bus forwards the event to the
// completion callback registered via SetOnCompletion. The bus
// itself stores no per-agent state — completion is purely a
// notification path for the orchestrator's reconcile code.
func (b *Bus) ReportCompletion(agentID, status string, err error) {
	b.fireCompletion(agentID, status, err)
}

// Append appends an entry to the log. Seq is overwritten by the bus
// (caller-supplied values are ignored). Returns the assigned seq, or
// an error if the bus is closed / ctx is cancelled / the cap is hit.
//
// The caller must NOT have set Seq — the owner is the single source
// of truth for ordering. The caller supplies the data fields (At,
// Body, File, Act, Ref) according to the entry's Kind.
func (b *Bus) Append(e Entry) (int64, error) {
	return b.appendInternal(e, 0)
}

// AppendPinned is like Append but pins the timestamp to the supplied
// value (no wall-clock read). Intended for tests; production code
// should call Append. Pass ts=0 to fall through to Append.
func (b *Bus) AppendPinned(e Entry, ts int64) (int64, error) {
	if ts == 0 {
		return b.appendInternal(e, 0)
	}
	return b.appendInternal(e, ts)
}

func (b *Bus) appendInternal(e Entry, forceTS int64) (int64, error) {
	if e.Kind == KindUnknown {
		return 0, ErrUnknownKind
	}
	if !b.started.Load() {
		// No owner goroutine — the request would never be read and the
		// caller would block on seqCh forever. Fail fast instead.
		return 0, ErrBusNotStarted
	}
	// Redact the body BEFORE sending to the owner. The
	// bus only ever stores the redacted form; the raw
	// text never reaches the log, the sidecar, or any
	// reader. Touches and resolves have no body and
	// pass through. We do this on the caller's
	// goroutine so the redactor is not blocking the
	// owner — but we then send the redacted entry in.
	if e.Kind == KindNote && b.redactor != nil {
		e.Body = b.redactor(e.Body)
	}
	seqCh := make(chan int64, 1)
	req := busRequest{kind: reqWrite, write: e, forceTS: forceTS, seqCh: seqCh}
	select {
	case b.reqs <- req:
	case <-b.stopCh:
		return 0, ErrBusClosed
	}
	select {
	case seq := <-seqCh:
		return seq, nil
	case <-b.stopCh:
		return 0, ErrBusClosed
	}
}

// run is the owner goroutine. It serializes writes (assigning seq),
// reads (snapshot/delta), and resolves. The persist sink is called
// after the in-memory append — disk IO never gates seq assignment
// for an in-memory slice that is the source of truth at read time.
// A slow persist sink will, however, slow subsequent appends in this
// implementation; production deployments can wrap the sink in an
// async shim if disk latency becomes a problem.
func (b *Bus) run(ctx context.Context) {
	defer close(b.done)
	for {
		select {
		case <-ctx.Done():
			b.shutdown()
			return
		case <-b.stopCh:
			b.shutdown()
			return
		case req := <-b.reqs:
			b.handle(req)
		}
	}
}

func (b *Bus) shutdown() {
	if b.persist != nil {
		if err := b.persist.Close(); err != nil {
			log.Printf("notebus: persist close: %v", err)
		}
	}
}

// handle dispatches a single request. This is the entire serialization
// point of the bus: every mutation and every read happens here, in
// order, on the owner goroutine.
func (b *Bus) handle(req busRequest) {
	switch req.kind {
	case reqWrite:
		// Cap check first so we never assign a seq to a dropped entry.
		if b.cap > 0 && b.seq >= b.cap {
			// Reply with a sentinel seq=0 and a non-nil error. The
			// caller can detect via ErrCapReached; we use seqCh=-1
			// as the "failed" signal so the caller's select can pick
			// it up.
			if req.seqCh != nil {
				req.seqCh <- -1
			}
			return
		}
		b.seq++
		e := req.write
		e.Seq = b.seq
		if req.forceTS > 0 {
			e.TS = req.forceTS
		} else if b.now != nil {
			e.TS = b.now()
		}
		b.log = append(b.log, e)
		if e.Kind == KindResolve && e.Ref > 0 {
			b.reslv[e.Ref] = true
		}
		if b.persist != nil {
			if err := b.persist.Write(e); err != nil {
				log.Printf("notebus: persist write seq=%d: %v", e.Seq, err)
			}
		}
		if req.seqCh != nil {
			req.seqCh <- e.Seq
		}
	case reqReadSnapshot:
		snap := make([]Entry, len(b.log))
		copy(snap, b.log)
		req.reply <- busResponse{snap: snap, head: b.seq}
	case reqReadDelta:
		agent := req.write.By
		cut := b.wm[agent]
		var delta []Entry
		for _, e := range b.log {
			if e.Seq <= cut {
				continue
			}
			if e.By == agent {
				// Skip own notes from injection but still advance the
				// watermark so the agent never re-evaluates them.
				continue
			}
			if e.Kind == KindResolve {
				// Resolves are bookkeeping; never injected.
				continue
			}
			if e.Kind == KindNote && b.reslv[e.Seq] {
				// Note was resolved by a later entry; do not inject.
				continue
			}
			delta = append(delta, e)
		}
		b.wm[agent] = b.seq
		req.reply <- busResponse{delt: delta, head: b.seq}
	}
}
