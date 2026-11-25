package agent

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

// ErrToolDigestSuperseded is returned to a run's caller when a newer run took
// its key, or when its owner was invalidated. It is always benign: the caller
// shows no error for it.
var ErrToolDigestSuperseded = errors.New("tool digest superseded")

// toolDigestCoord is the process-wide coordinator. The TUI and the web server
// share it, so the semaphore bounds explanations across all surfaces.
var toolDigestCoord = newToolDigestCoordinator(time.Now)

// toolDigestOwnerKey carries the owner identity (the session id) on the run
// context, so run can record it without a signature change.
type toolDigestOwnerKey struct{}

// withToolDigestOwner tags ctx with the owner identity used by invalidate.
func withToolDigestOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, toolDigestOwnerKey{}, owner)
}

// toolDigestCoordinator coordinates explainer runs: it cancels and replaces an
// active run that uses the same key, caches successful results briefly, and
// bounds concurrent runs with a semaphore. mu guards every field below it and
// is never held across fn.
type toolDigestCoordinator struct {
	now func() time.Time
	sem chan struct{}

	mu     sync.Mutex
	active map[string]*toolDigestRun
	cache  *list.List // front = most recently used; elements hold *toolDigestCacheEntry
	index  map[string]*list.Element
}

// toolDigestRun is one active run. dead is set when a newer run replaces it or
// its owner is invalidated; a dead run's result is never cached.
type toolDigestRun struct {
	owner  string
	cancel context.CancelFunc
	dead   bool
}

type toolDigestCacheEntry struct {
	key     string
	owner   string
	digest  ToolDigest
	expires time.Time
}

// newToolDigestCoordinator builds a coordinator whose clock is now. The
// semaphore and cache capacity are read here, so a test sets the vars first.
func newToolDigestCoordinator(now func() time.Time) *toolDigestCoordinator {
	return &toolDigestCoordinator{
		now:    now,
		sem:    make(chan struct{}, toolDigestMaxConcurrent),
		active: make(map[string]*toolDigestRun),
		cache:  list.New(),
		index:  make(map[string]*list.Element),
	}
}

// run returns the digest for cacheKey, running fn at most once per cache
// lifetime. flightKey identifies the in-flight run: starting one that is already
// active cancels the older run, whose caller gets ErrToolDigestSuperseded. The
// two keys differ because a request id supersedes by ask, while the cache must
// answer only for an identical masked payload.
//
// A cache hit returns at once and takes no semaphore slot. Otherwise run starts
// the overall deadline before waiting for a slot, so queue time spends the same
// budget as the work.
func (c *toolDigestCoordinator) run(ctx context.Context, flightKey, cacheKey string, fn func(context.Context) (ToolDigest, error)) (ToolDigest, error) {
	owner, _ := ctx.Value(toolDigestOwnerKey{}).(string)

	c.mu.Lock()
	// A newer request for the same ask supersedes the older run even when the
	// cache can answer this one, so the older caller gets the benign 409.
	if prev := c.active[flightKey]; prev != nil {
		prev.dead = true
		prev.cancel()
		delete(c.active, flightKey)
	}
	if digest, ok := c.lookupLocked(cacheKey); ok {
		c.mu.Unlock()
		return digest, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, toolDigestRunTimeout)
	mine := &toolDigestRun{owner: owner, cancel: cancel}
	c.active[flightKey] = mine
	c.mu.Unlock()

	digest, err := c.execute(runCtx, fn)

	c.mu.Lock()
	if c.active[flightKey] == mine {
		delete(c.active, flightKey)
	}
	dead := mine.dead
	if err == nil && !dead {
		c.storeLocked(cacheKey, owner, digest)
	}
	c.mu.Unlock()
	cancel()

	if dead {
		return ToolDigest{}, ErrToolDigestSuperseded
	}
	if err != nil {
		return ToolDigest{}, err
	}
	return digest, nil
}

// execute waits for a semaphore slot, then runs fn. A run whose context ends in
// the queue never starts fn.
func (c *toolDigestCoordinator) execute(runCtx context.Context, fn func(context.Context) (ToolDigest, error)) (ToolDigest, error) {
	select {
	case c.sem <- struct{}{}:
	case <-runCtx.Done():
		return ToolDigest{}, toolDigestRunEndError(runCtx)
	}
	defer func() { <-c.sem }()
	if err := runCtx.Err(); err != nil {
		return ToolDigest{}, toolDigestRunEndError(runCtx)
	}
	return fn(runCtx)
}

// invalidate cancels owner's active runs and drops owner's cached digests. A
// cancelled caller gets ErrToolDigestSuperseded. Other owners are untouched.
func (c *toolDigestCoordinator) invalidate(owner string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, run := range c.active {
		if run.owner == owner {
			run.dead = true
			run.cancel()
			delete(c.active, key)
		}
	}
	for el := c.cache.Front(); el != nil; {
		next := el.Next()
		if el.Value.(*toolDigestCacheEntry).owner == owner {
			c.removeElemLocked(el)
		}
		el = next
	}
}

// lookupLocked returns a live cached digest for key and marks it most recently
// used. An expired entry is removed.
func (c *toolDigestCoordinator) lookupLocked(key string) (ToolDigest, bool) {
	el, ok := c.index[key]
	if !ok {
		return ToolDigest{}, false
	}
	entry := el.Value.(*toolDigestCacheEntry)
	if !c.now().Before(entry.expires) {
		c.removeElemLocked(el)
		return ToolDigest{}, false
	}
	c.cache.MoveToFront(el)
	return entry.digest, true
}

// storeLocked caches a successful digest and evicts the least recently used
// entries beyond toolDigestCacheEntries.
func (c *toolDigestCoordinator) storeLocked(key, owner string, digest ToolDigest) {
	if el, ok := c.index[key]; ok {
		c.removeElemLocked(el)
	}
	entry := &toolDigestCacheEntry{
		key:     key,
		owner:   owner,
		digest:  digest,
		expires: c.now().Add(toolDigestCacheTTL),
	}
	c.index[key] = c.cache.PushFront(entry)
	for c.cache.Len() > toolDigestCacheEntries {
		c.removeElemLocked(c.cache.Back())
	}
}

func (c *toolDigestCoordinator) removeElemLocked(el *list.Element) {
	entry := c.cache.Remove(el).(*toolDigestCacheEntry)
	delete(c.index, entry.key)
}
