package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	shellpkg "github.com/u007/ocode/internal/shell"
)

// shellSession is the subset of *shellpkg.Session the registry needs. It is an
// interface so tests can substitute a fake shell without a pty (a host confined
// by ocode's own sandbox cannot allocate one), and so the Windows stub satisfies
// it too.
type shellSession interface {
	Run(ctx context.Context, command string) (shellpkg.Result, string, error)
	Close() error
	Alive() bool
}

// errShellSessionClosed is returned when a command races the teardown of its own
// session (the tab was closed while the request was in flight). The handler
// treats any registry error as "fall back to a one-shot run".
var errShellSessionClosed = errors.New("persistent shell session was closed")

// shellSessionEntry is one lazily-created persistent shell plus its bookkeeping.
type shellSessionEntry struct {
	mu sync.Mutex // serialises create + rebase + run for this key

	// sessMu guards sess alone so close() can read and detach the session
	// without waiting behind an in-flight command held under mu.
	sessMu   sync.Mutex
	sess     shellSession
	workDir  string
	lastUsed time.Time
	closed   atomic.Bool
}

// shellSessionRegistry owns the lazy per-session-key persistent shells. The key
// is the frontend tab id (a real session id, or a `new-*` draft id). Entries are
// created on first use, dropped on tab close, moved on /reset-id, and reaped
// after an idle timeout.
type shellSessionRegistry struct {
	mu      sync.Mutex
	entries map[string]*shellSessionEntry

	newSession func(shellpkg.SessionOptions) (shellSession, error)
	idle       time.Duration
	now        func() time.Time

	stopOnce sync.Once
	stop     chan struct{}
}

// newShellSessionRegistry builds a registry. newSession is the session factory
// (a failing factory is how the one-shot fallback is exercised), idle is the
// reap timeout, and now is the clock the reaper reads (injected so tests need
// no sleeping).
func newShellSessionRegistry(
	newSession func(shellpkg.SessionOptions) (shellSession, error),
	idle time.Duration,
	now func() time.Time,
) *shellSessionRegistry {
	if now == nil {
		now = time.Now
	}
	return &shellSessionRegistry{
		entries:    make(map[string]*shellSessionEntry),
		newSession: newSession,
		idle:       idle,
		now:        now,
		stop:       make(chan struct{}),
	}
}

// run executes command in the persistent shell for key, creating it lazily.
// workDir is the project path: when it differs from the directory the shell was
// spawned in, the registry rebases the shell there first (a project switch)
// instead of silently running in the previous project's directory. A reset
// closes any existing shell for the key before running.
//
// Any non-nil error means no usable persistent shell: the caller should fall
// back to a one-shot shellpkg.Run.
func (r *shellSessionRegistry) run(ctx context.Context, key, workDir, command string, reset bool) (shellpkg.Result, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if key == "" {
		return shellpkg.Result{}, "", errors.New("empty shell session key")
	}
	if reset {
		r.close(key)
	}
	entry := r.entry(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed.Load() {
		return shellpkg.Result{}, "", errShellSessionClosed
	}
	sess, err := r.sessionForLocked(entry, key, workDir)
	if err != nil {
		return shellpkg.Result{}, "", err
	}
	if workDir != "" && entry.workDir != workDir {
		if _, cwd, cerr := sess.Run(ctx, "cd "+shellpkg.Quote(workDir)); cerr != nil {
			log.Printf("server: persistent shell %q: rebase to %q failed: %v", key, workDir, cerr)
		} else {
			log.Printf("server: persistent shell %q: rebased to %q", key, workDir)
			entry.workDir = workDir
			_ = cwd
		}
	}
	res, cwd, err := sess.Run(ctx, command)
	entry.lastUsed = r.now()
	return res, cwd, err
}

// sessionForLocked returns the entry's live session, creating (or replacing a
// dead) one. Callers must hold entry.mu.
func (r *shellSessionRegistry) sessionForLocked(entry *shellSessionEntry, key, workDir string) (shellSession, error) {
	if cur := entry.currentSession(); cur != nil && cur.Alive() {
		return cur, nil
	} else if cur != nil {
		log.Printf("server: persistent shell %q exited; replacing it", key)
		if err := cur.Close(); err != nil {
			log.Printf("server: close dead persistent shell %q: %v", key, err)
		}
		entry.detachSession(cur)
	}
	sess, err := r.newSession(shellpkg.SessionOptions{
		Dir:     workDir,
		Timeout: shellpkg.DefaultTimeout,
	})
	if err != nil {
		return nil, err
	}
	entry.setSession(sess)
	if entry.closed.Load() {
		// Lost a race with close(): do not publish a session nobody owns.
		if err := sess.Close(); err != nil {
			log.Printf("server: close raced persistent shell %q: %v", key, err)
		}
		entry.detachSession(sess)
		return nil, errShellSessionClosed
	}
	entry.workDir = workDir
	entry.lastUsed = r.now()
	return sess, nil
}

// currentSession returns the entry's session under sessMu.
func (e *shellSessionEntry) currentSession() shellSession {
	e.sessMu.Lock()
	defer e.sessMu.Unlock()
	return e.sess
}

// setSession publishes a newly created session.
func (e *shellSessionEntry) setSession(sess shellSession) {
	e.sessMu.Lock()
	e.sess = sess
	e.sessMu.Unlock()
}

// detachSession clears sess if it is still the given session (a concurrent
// close may already have detached it).
func (e *shellSessionEntry) detachSession(sess shellSession) {
	e.sessMu.Lock()
	if e.sess == sess {
		e.sess = nil
	}
	e.sessMu.Unlock()
}

// entry returns the entry for key, creating an empty one if absent.
func (r *shellSessionRegistry) entry(key string) *shellSessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[key]
	if entry == nil {
		entry = &shellSessionEntry{}
		r.entries[key] = entry
	}
	return entry
}

// close drops the shell for key. It is unconditional and never blocks on an
// in-flight command: Session.Close hangs up the shell, which makes the blocked
// Run return. Safe to call for an unknown key.
func (r *shellSessionRegistry) close(key string) {
	if key == "" {
		return
	}
	r.mu.Lock()
	entry := r.entries[key]
	delete(r.entries, key)
	r.mu.Unlock()
	if entry == nil {
		return
	}
	entry.closed.Store(true)
	entry.sessMu.Lock()
	sess := entry.sess
	entry.sess = nil
	entry.sessMu.Unlock()
	if sess != nil {
		if err := sess.Close(); err != nil {
			log.Printf("server: close persistent shell %q: %v", key, err)
		}
	}
}

// rekey moves the shell for oldKey to newKey so a /reset-id keeps shell state
// (cwd, exported vars, functions) instead of paying a fresh rc load.
func (r *shellSessionRegistry) rekey(oldKey, newKey string) {
	if oldKey == "" || newKey == "" || oldKey == newKey {
		return
	}
	r.mu.Lock()
	entry := r.entries[oldKey]
	if entry == nil {
		r.mu.Unlock()
		return
	}
	replaced := r.entries[newKey]
	delete(r.entries, oldKey)
	r.entries[newKey] = entry
	r.mu.Unlock()

	if replaced != nil && replaced != entry {
		// The target key already had a shell (e.g. a draft id reused): drop it
		// so the moved entry is the single owner.
		log.Printf("server: persistent shell rekey %q -> %q replaced an existing shell", oldKey, newKey)
		replaced.closed.Store(true)
		replaced.sessMu.Lock()
		sess := replaced.sess
		replaced.sess = nil
		replaced.sessMu.Unlock()
		if sess != nil {
			if err := sess.Close(); err != nil {
				log.Printf("server: close replaced persistent shell %q: %v", newKey, err)
			}
		}
	}
}

// closeAll tears down every shell and stops the reaper. Called from shutdown.
func (r *shellSessionRegistry) closeAll() {
	r.stopOnce.Do(func() { close(r.stop) })
	r.mu.Lock()
	entries := r.entries
	r.entries = make(map[string]*shellSessionEntry)
	r.mu.Unlock()
	for key, entry := range entries {
		entry.closed.Store(true)
		entry.sessMu.Lock()
		sess := entry.sess
		entry.sess = nil
		entry.sessMu.Unlock()
		if sess != nil {
			if err := sess.Close(); err != nil {
				log.Printf("server: close persistent shell %q on shutdown: %v", key, err)
			}
		}
	}
}

// reap closes shells idle for longer than the configured timeout. Driven by the
// reaper goroutine, and called directly by tests with an injected clock.
func (r *shellSessionRegistry) reap() {
	if r.idle <= 0 {
		return
	}
	cutoff := r.now().Add(-r.idle)
	r.mu.Lock()
	snapshot := make(map[string]*shellSessionEntry, len(r.entries))
	for key, entry := range r.entries {
		snapshot[key] = entry
	}
	r.mu.Unlock()

	for key, entry := range snapshot {
		entry.mu.Lock()
		last := entry.lastUsed
		entry.mu.Unlock()
		if last.IsZero() || !last.Before(cutoff) {
			continue
		}
		log.Printf("server: reaping idle persistent shell %q (last used %s)", key, last.Format(time.RFC3339))
		r.close(key)
	}
}

// startReaper runs reap on a ticker until closeAll. interval is capped at the
// idle timeout so a shell cannot sit past its deadline by much.
func (r *shellSessionRegistry) startReaper(interval time.Duration) {
	if r.idle <= 0 {
		return
	}
	if interval <= 0 || interval > r.idle {
		interval = r.idle
	}
	r.stopOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-r.stop:
					return
				case <-ticker.C:
					r.reap()
				}
			}
		}()
	})
}
