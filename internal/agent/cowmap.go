package agent

import (
	"maps"
	"sync"
	"sync/atomic"
)

// cowMap is a copy-on-write map used for every rule table on
// PermissionManager.
//
// The permission state is mutated from goroutines that are NOT the agent's turn
// goroutine: `POST /api/permissions` (SetRule), the permission/question
// continuation handlers (SetUserConfirmedRule, SetWebfetchDomain) and the TUI
// `/permissions` command handlers (SetBashAutoAllowPrefix, SetBashPrefixMode)
// all write while turns are running, and canAutoAllowWithMode persists an
// in-root allow from *inside* Decide. A plain map made that a concurrent
// read/write — a Go runtime FATAL ("concurrent map read and map write"), not
// merely a race-detector warning, and it takes the whole process down.
//
// The contract, identical for every table:
//
//   - Writers call mutate: clone the current map, apply fn to the CLONE, publish
//     it. fn must therefore never mutate a value it read out of a snapshot — a
//     map-of-slices has to copy the inner slice too, because the clone shares
//     the previous version's backing array (see SetPathRule).
//   - mutate serialises writers under mu so two of them cannot lose each
//     other's change. Readers never take mu, so a fn that itself reads a
//     snapshot cannot deadlock.
//   - Readers call load (whole snapshot, safe to hold or range over for as long
//     as it likes, since the map behind the pointer is never mutated in place)
//     or get (one key). Callers that make several decisions should take ONE
//     snapshot and reuse it, so their lookups cannot disagree with each other
//     if a save lands mid-decision.
type cowMap[K comparable, V any] struct {
	mu sync.Mutex
	p  atomic.Pointer[map[K]V]
}

// init seeds the table. It is a method on the pointer receiver rather than a
// `newCowMap` constructor returning a value, because returning a struct that
// contains a mutex copies the lock (go vet's "return copies lock value"), and a
// copied mutex would be a lock nobody else can unlock.
//
// Every table must be initialised before a reader or writer runs. A zero-value
// cowMap still answers reads safely (nil map), so a partially initialised
// PermissionManager degrades to "no rules" rather than panicking.
func (c *cowMap[K, V]) init(m map[K]V) {
	next := maps.Clone(m)
	if next == nil {
		next = make(map[K]V)
	}
	c.p.Store(&next)
}

// load returns the current snapshot. Never nil after construction; a zero-value
// cowMap (a PermissionManager built without the constructor) returns nil, which
// is safe to index, range and take the length of.
func (c *cowMap[K, V]) load() map[K]V {
	if p := c.p.Load(); p != nil {
		return *p
	}
	return nil
}

// get reads one key. The two-value form matches the map idiom it replaces.
func (c *cowMap[K, V]) get(k K) (V, bool) {
	m := c.load()
	v, ok := m[k]
	return v, ok
}

// mutate clones, applies fn and publishes. The cost is one map copy per write,
// which is irrelevant next to an LLM round trip and buys lock-free reads.
func (c *cowMap[K, V]) mutate(fn func(m map[K]V)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := c.load()
	next := make(map[K]V, len(cur)+1)
	maps.Copy(next, cur)
	fn(next)
	c.p.Store(&next)
}

// set publishes a prepared map wholesale. Only for construction, Clone and load
// paths, where the whole table is being replaced anyway — never for a single
// user-facing rule change (that is mutate, so concurrent writers are not lost).
func (c *cowMap[K, V]) set(m map[K]V) {
	next := maps.Clone(m)
	if next == nil {
		next = make(map[K]V)
	}
	c.p.Store(&next)
}

// cowSlice is the copy-on-write counterpart for append-in-place tables (the
// glob-style tool patterns). Same rationale as cowMap: the writer may be a
// handler goroutine while Decide ranges over the slice.
type cowSlice[T any] struct {
	mu sync.Mutex
	p  atomic.Pointer[[]T]
}

func (c *cowSlice[T]) init(s []T) {
	next := make([]T, len(s))
	copy(next, s)
	c.p.Store(&next)
}

// load returns the current snapshot. Treat it as immutable: it is safe to range
// over while a writer swaps in a new version.
// set publishes a prepared slice wholesale (construction / Clone paths). A nil
// argument publishes an empty slice, never nil, so load() never hands a reader
// a table it has to nil-check.
func (c *cowSlice[T]) set(s []T) {
	next := make([]T, len(s))
	copy(next, s)
	c.p.Store(&next)
}

func (c *cowSlice[T]) load() []T {
	if p := c.p.Load(); p != nil {
		return *p
	}
	return nil
}

// mutate copies with NO spare capacity, hands the copy to fn and publishes
// whatever fn returns.
//
// The signature returns the slice because a slice is a value: `fn` doing
// `s = append(s, x)` would only rebind its own parameter, and the published
// version would silently miss the append. A map does not have this problem
// (the clone is a reference type), which is exactly why cowMap can hand fn a
// plain map and cowSlice cannot.
func (c *cowSlice[T]) mutate(fn func(s []T) []T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := c.load()
	next := make([]T, len(cur), len(cur)+1)
	copy(next, cur)
	next = fn(next)
	if next == nil {
		next = []T{}
	}
	c.p.Store(&next)
}
