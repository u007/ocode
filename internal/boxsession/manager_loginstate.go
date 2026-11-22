package boxsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"runtime/debug"
	"sync"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/crashguard"
)

// This file holds the login-state half of the box lifecycle: restoring a saved
// state into a new box, debounced checkpoints while the box is used, the save
// before a box expires, and the final saves at shutdown and teardown. Manager.Start,
// Browser, Rekey, Tick, Shutdown and retireBox call into it.

const (
	// loginCheckpointDelay is the debounce between a browser call and its checkpoint.
	loginCheckpointDelay = 10 * time.Second

	// loginCheckpointMaxWait bounds how long browser calls may keep a checkpoint
	// waiting: once this long has passed since the box last saved, the next call
	// runs one at once. Without it a session that calls more often than the
	// debounce never saves until the box is gone.
	loginCheckpointMaxWait = 60 * time.Second

	// loginPreExpiry is how close to its expiry a box with a login state saves
	// once more, so the state is current when the box is deleted at the hard cap.
	loginPreExpiry = 90 * time.Second

	// loginStateCap bounds one capture-and-save: the storage reads, then the write.
	loginStateCap = 10 * time.Second

	// shutdownDeleteReserve is the part of the shutdown deadline kept back from the
	// final saves, so the box deletes still have time to run.
	shutdownDeleteReserve = 3 * time.Second
)

// errSaveAbandoned is returned by a capture that its shutdown gave up on.
var errSaveAbandoned = errors.New("the save was abandoned at the shutdown deadline")

// boxBrowser is the storage surface of a box's browser the manager uses. It is
// the storage calls plus Close. *cdp.Manager satisfies it; tests fake it.
type boxBrowser interface {
	Close(ctx context.Context) error
	StorageCookies(ctx context.Context) ([]json.RawMessage, error)
	// SetStorageCookies returns how many cookies the browser refused.
	SetStorageCookies(ctx context.Context, cookies []json.RawMessage) (rejected int, err error)
	LocalStorageSnapshot(ctx context.Context) (map[string]map[string]string, error)
	// SetLocalStorageSeed returns the origins too large to restore, by name.
	SetLocalStorageSeed(ctx context.Context, seed map[string]map[string]string) (skipped []string, err error)
}

var _ boxBrowser = (*cdp.Manager)(nil)

// checkpointTimer is the part of *time.Timer the checkpoint debounce uses.
type checkpointTimer interface {
	Stop() bool
}

// stoppedTimer stands in for a timer on a checkpoint that runs at once, with no timer.
type stoppedTimer struct{}

func (stoppedTimer) Stop() bool { return false }

// pendingCheckpoint is one armed checkpoint. Its identity is the token: a timer
// that fires after it was replaced or cancelled finds a different token on the
// entry and does nothing. It holds the entry, not a session id, so a checkpoint
// armed before a Rekey still finds the box under its new id.
type pendingCheckpoint struct {
	timer checkpointTimer
	entry *entry
}

// loginHold is one session's hold on a named login state. free releases it and
// is replaced when the hold moves to a new session on Rekey.
type loginHold struct {
	name    string
	session string
	free    func() error
	// saved is the state read when the box started. Nil when the name had no file.
	saved *LoginState
	// notes are what the restore could not apply, in words the user can read.
	notes []string
}

// unhold releases the hold. It is nil-safe: a box with no login state has no hold.
func (h *loginHold) unhold() error {
	if h == nil {
		return nil
	}
	if err := h.free(); err != nil {
		return fmt.Errorf("release login state %q of session %s: %w", h.name, h.session, err)
	}
	return nil
}

// loginStateName resolves the login state a new box of sessionID uses. It returns
// "" when the feature is off. A lookup failure is returned, never read as "none".
func (m *Manager) loginStateName(sessionID string) (string, error) {
	if m.deps.LoginStateFor == nil {
		return "", nil
	}
	name, err := m.deps.LoginStateFor(sessionID)
	if err != nil {
		return "", fmt.Errorf("boxsession: resolve the login state of session %s: %w", sessionID, err)
	}
	return name, nil
}

// openLoginState takes the one-writer hold on name for sessionID, then reads its
// saved state. It runs before any box is created. ErrLoginStateInUse is returned
// unchanged, so it names the session that holds the state. A read failure releases
// the hold again. An empty name returns a nil hold.
func (m *Manager) openLoginState(name, sessionID string) (*loginHold, error) {
	if name == "" {
		return nil, nil
	}
	free, err := m.deps.LoginStates.Acquire(name, sessionID)
	if err != nil {
		return nil, err
	}
	h := &loginHold{name: name, session: sessionID, free: free}
	st, found, err := m.deps.LoginStates.Load(name)
	if err != nil {
		if rerr := h.unhold(); rerr != nil {
			log.Printf("boxsession: %v", rerr)
		}
		return nil, err
	}
	if found {
		h.saved = &st
	}
	return h, nil
}

// restoreLoginState puts a saved state into a box's browser before the box is
// published. Cookies go first, then the localStorage seed, which applies lazily on
// each origin's first visit. What the browser refused or skipped is kept on the
// hold as notes, and logged by count and origin name only, never by value.
func (m *Manager) restoreLoginState(ctx context.Context, storage boxBrowser, h *loginHold) error {
	st := h.saved
	rejected, err := storage.SetStorageCookies(ctx, st.Cookies)
	if err != nil {
		return fmt.Errorf("boxsession: restore cookies of login state %q: %w", h.name, err)
	}
	skipped, err := storage.SetLocalStorageSeed(ctx, st.LocalStorage)
	if err != nil {
		return fmt.Errorf("boxsession: restore site storage of login state %q: %w", h.name, err)
	}
	h.notes = restoreNotes(rejected, skipped)
	log.Printf("boxsession: restored login state %q into session %s: %d cookies, %d origins, %d cookies rejected, %d origins skipped %v",
		h.name, h.session, len(st.Cookies), len(st.LocalStorage), rejected, len(skipped), skipped)
	return nil
}

// restoreNotes words what a restore could not apply. An empty result means the
// whole state was restored.
func restoreNotes(rejected int, skipped []string) []string {
	var notes []string
	switch {
	case rejected == 1:
		notes = append(notes, "1 cookie was rejected by the browser")
	case rejected > 1:
		notes = append(notes, fmt.Sprintf("%d cookies were rejected by the browser", rejected))
	}
	for _, origin := range skipped {
		notes = append(notes, fmt.Sprintf("site storage for %s was too large and was not restored", origin))
	}
	return notes
}

// contextRestoreFor is the hook a box's browser runs when its shared browser context
// is replaced. It loads the latest saved file of the box's login state, which holds
// newer checkpoints than the box started with. A box with no login state gets an
// empty hook, so a clean browser stays clean after a browser restart. The hook never
// calls back into the Manager.
func (m *Manager) contextRestoreFor(h *loginHold) func(ctx context.Context) ([]json.RawMessage, map[string]map[string]string, error) {
	if h == nil {
		return func(context.Context) ([]json.RawMessage, map[string]map[string]string, error) {
			return []json.RawMessage{}, map[string]map[string]string{}, nil
		}
	}
	name, store := h.name, m.deps.LoginStates
	return func(context.Context) ([]json.RawMessage, map[string]map[string]string, error) {
		st, found, err := store.Load(name)
		if err != nil {
			return nil, nil, fmt.Errorf("boxsession: load login state %q for a replacement browser: %w", name, err)
		}
		if !found {
			return []json.RawMessage{}, map[string]map[string]string{}, nil
		}
		return st.Cookies, st.LocalStorage, nil
	}
}

// armCheckpointLocked (re)starts the debounce of e's login-state checkpoint. The
// caller holds m.mu and e.box is non-nil. A box with no login state arms nothing.
// When the box has gone checkpointMaxWait without a save, the checkpoint runs at once.
func (m *Manager) armCheckpointLocked(sessionID string, e *entry) {
	if e.box.hold == nil {
		return
	}
	if e.checkpoint != nil {
		e.checkpoint.timer.Stop()
	}
	delay := m.checkpointDelay
	if m.now().Sub(e.box.lastSave) >= m.checkpointMaxWait {
		delay = 0
	}
	pc := &pendingCheckpoint{entry: e}
	pc.timer = m.afterFunc(delay, func() {
		// A panic here must not take the server down: the checkpoint is a
		// best-effort copy, and the next browser call arms another one.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("boxsession: login-state checkpoint of session %s panicked: %v\n%s", sessionID, r, debug.Stack())
			}
		}()
		m.runCheckpoint(pc)
	})
	e.checkpoint = pc
}

// lockGate takes an entry's gate, or fails when ctx ends first. It does not
// re-look the session up: the caller holds the entry itself, which Rekey keeps
// current under its new id.
func lockGate(ctx context.Context, e *entry) error {
	select {
	case e.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("session is busy: %w", ctx.Err())
	}
}

// runCheckpoint is the debounced save. It takes the entry gate, and does nothing
// unless its token is still the entry's pending checkpoint. A teardown clears the
// token under the gate, so a checkpoint that fires during or after a teardown
// never reads the box.
func (m *Manager) runCheckpoint(pc *pendingCheckpoint) {
	e := pc.entry
	lctx, lcancel := context.WithTimeout(context.Background(), loginStateCap)
	err := lockGate(lctx, e)
	lcancel()
	if err != nil {
		log.Printf("boxsession: login-state checkpoint skipped: %v", err)
		return
	}
	defer m.unlockEntry(e)

	m.mu.Lock()
	if e.checkpoint != pc || e.box == nil || e.closing {
		m.mu.Unlock()
		return
	}
	e.checkpoint = nil
	b, session := e.box, e.session
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), loginStateCap)
	defer cancel()
	err = m.captureAndSave(ctx, b, nil)
	m.noteSaved(b)
	if err != nil {
		log.Printf("boxsession: login-state checkpoint of session %s (box %s): %v", session, b.id, err)
	}
}

// noteSaved records that a save of b has run to its end, successful or not. A
// failing save is therefore retried at most once per checkpointMaxWait, not on
// every browser call.
func (m *Manager) noteSaved(b *liveBox) {
	m.mu.Lock()
	b.lastSave = m.now()
	m.mu.Unlock()
}

// preExpiryCheckpoint saves an ephemeral box with a login state once, when it is
// within loginPreExpiry of its ttl. Tick calls it. It runs synchronously and holds
// the entry gate only inside runCheckpoint. An expired box is left to its
// teardown, which saves it. A retained box has no ttl, so it is never checked here.
func (m *Manager) preExpiryCheckpoint(sessionID string, b *liveBox, now time.Time) {
	if b.kind != KindEphemeral || b.hold == nil || now.Before(b.expiresAt.Add(-loginPreExpiry)) || !now.Before(b.expiresAt) {
		return
	}
	m.mu.Lock()
	e := m.sessions[sessionID]
	if e == nil || e.box != b || e.closing || b.preExpirySaved {
		m.mu.Unlock()
		return
	}
	b.preExpirySaved = true
	if e.checkpoint != nil {
		e.checkpoint.timer.Stop()
	}
	pc := &pendingCheckpoint{entry: e, timer: stoppedTimer{}}
	e.checkpoint = pc
	m.mu.Unlock()
	m.runCheckpoint(pc)
}

// saveTicket is the one write a capture makes. Shutdown abandons a capture whose
// deadline passes while it is still reading, and an abandoned capture never writes,
// so the file keeps its previous state. A capture that already wrote stays written.
type saveTicket struct {
	mu        sync.Mutex
	written   bool
	abandoned bool
}

// abandon marks the capture abandoned unless it already wrote. It reports whether
// the capture had finished, so the caller can log a capture that missed its deadline.
func (t *saveTicket) abandon() (finished bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.written {
		return true
	}
	t.abandoned = true
	return false
}

// captureAndSave reads the box's cookies and site storage and saves them under
// the box's login-state name. The caller holds the entry gate. A non-nil ticket
// lets a shutdown abandon the capture before its write.
//
// A cookie read error saves nothing, so the previous file stands. A partial
// storage snapshot is used as far as it goes: an origin in the snapshot replaces
// the stored origin, an empty one included (cleared, so removed); an origin the
// snapshot does not report keeps its stored items; an origin it dropped as too
// large keeps its stored items, and the snapshot error names it. Cookies are
// replaced wholesale by the read. A state file that does not parse is an error
// and is never overwritten.
func (m *Manager) captureAndSave(ctx context.Context, b *liveBox, t *saveTicket) error {
	name := b.hold.name
	cookies, err := b.storage.StorageCookies(ctx)
	if err != nil {
		return fmt.Errorf("read cookies of box %s, keeping login state %q as it was: %w", b.id, name, err)
	}
	snap, snapErr := b.storage.LocalStorageSnapshot(ctx)
	if snapErr != nil {
		log.Printf("boxsession: login state %q: part of the site storage was not captured, keeping the stored origins for it: %v", name, snapErr)
	}
	return m.commitCapture(name, cookies, snap, t)
}

// commitCapture merges one capture into the stored state of name and writes it.
// The read-modify-write is the only write a capture makes.
func (m *Manager) commitCapture(name string, cookies []json.RawMessage, snap map[string]map[string]string, t *saveTicket) error {
	if t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.abandoned {
			return fmt.Errorf("login state %q: %w; it keeps its previous file", name, errSaveAbandoned)
		}
	}
	stored, found, err := m.deps.LoginStates.Load(name)
	if err != nil {
		return fmt.Errorf("load login state %q before saving it, keeping it as it was: %w", name, err)
	}
	merged := make(map[string]map[string]string, len(stored.LocalStorage)+len(snap))
	if found {
		maps.Copy(merged, stored.LocalStorage)
	}
	for origin, items := range snap {
		if len(items) == 0 {
			delete(merged, origin)
			continue
		}
		merged[origin] = items
	}
	if err := m.deps.LoginStates.Save(LoginState{
		Name:         name,
		SavedAt:      m.now(),
		Cookies:      cookies,
		LocalStorage: merged,
	}); err != nil {
		return fmt.Errorf("save login state %q: %w", name, err)
	}
	if t != nil {
		t.written = true
	}
	if m.onCommit != nil {
		m.onCommit(name)
	}
	return nil
}

// finalLoginSave is the save at a single-box teardown. It is bounded by loginStateCap
// on a context that is not already cancelled, because a teardown often runs under a
// caller's context that has just ended. A failure is logged and teardown goes on:
// the last checkpoint stands. The caller holds the entry gate.
func (m *Manager) finalLoginSave(ctx context.Context, sessionID string, b *liveBox) {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loginStateCap)
	defer cancel()
	if err := m.captureAndSave(sctx, b, nil); err != nil {
		log.Printf("boxsession: final login-state save of box %s (session %s): %v; the last checkpoint stands", b.id, sessionID, err)
	}
}

// pendingSave is one box whose final save a shutdown runs.
type pendingSave struct {
	session string
	e       *entry
	b       *liveBox
	t       *saveTicket
}

// finalSaves runs the final capture-and-save of every box with a login state at
// once, under one deadline of budget. A capture that has not finished by the
// deadline is abandoned, so it keeps its previous file and cannot write later.
// It returns when every capture has finished or been abandoned. The caller holds
// no entry gate.
func (m *Manager) finalSaves(ctx context.Context, budget time.Duration, saves []pendingSave) {
	if len(saves) == 0 {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	done := make(chan string, len(saves))
	pending := make(map[string]pendingSave, len(saves))
	for _, s := range saves {
		pending[s.session] = s
		crashguard.Go(func() {
			defer func() { done <- s.session }()
			m.saveOne(sctx, s)
		})
	}
	for len(pending) > 0 {
		select {
		case id := <-done:
			delete(pending, id)
		case <-sctx.Done():
			for id, s := range pending {
				if !s.t.abandon() {
					log.Printf("boxsession: final login-state save of session %s (box %s) did not finish by the shutdown deadline; its login state keeps the previous file", id, s.b.id)
				}
			}
			m.awaitAbandoned(done, pending)
			return
		}
	}
}

// awaitAbandoned waits, for at most shutdownDeleteReserve, for the abandoned captures
// to return. A capture that returns releases its entry gate, so its box can still be
// torn down. One that does not return in time is logged; its gate stays busy.
func (m *Manager) awaitAbandoned(done <-chan string, pending map[string]pendingSave) {
	timer := time.NewTimer(shutdownDeleteReserve)
	defer timer.Stop()
	for len(pending) > 0 {
		select {
		case id := <-done:
			delete(pending, id)
		case <-timer.C:
			for id := range pending {
				log.Printf("boxsession: abandoned login-state save of session %s has not returned; its box's delete may be skipped", id)
			}
			return
		}
	}
}

// saveOne takes the box's entry gate and runs its capture. It does nothing when the
// box was torn down before the gate was free.
func (m *Manager) saveOne(ctx context.Context, s pendingSave) {
	if err := lockGate(ctx, s.e); err != nil {
		log.Printf("boxsession: final login-state save of session %s (box %s) did not start: %v; it keeps the previous file", s.session, s.b.id, err)
		return
	}
	defer m.unlockEntry(s.e)
	m.mu.Lock()
	current := s.e.box == s.b && !s.e.closing
	m.mu.Unlock()
	if !current {
		return
	}
	if err := m.captureAndSave(ctx, s.b, s.t); err != nil {
		log.Printf("boxsession: final login-state save of session %s (box %s): %v; the last checkpoint stands", s.session, s.b.id, err)
	}
}

// loginSavesLocked lists every box with a login state, with a fresh ticket for each.
// The caller holds m.mu.
func (m *Manager) loginSavesLocked() []pendingSave {
	var out []pendingSave
	for id, e := range m.sessions {
		if e.box == nil || e.closing || e.box.hold == nil {
			continue
		}
		out = append(out, pendingSave{session: id, e: e, b: e.box, t: &saveTicket{}})
	}
	return out
}

// releaseLogin releases a hold after a start that did not publish its box.
func releaseLogin(h *loginHold) {
	if err := h.unhold(); err != nil {
		log.Printf("boxsession: %v", err)
	}
}

// moveTo hands the hold to toSession, as Rekey does. The store moves the holder
// file under the name lock, so no other session can take the name in between.
func (h *loginHold) moveTo(store *LoginStateStore, toSession string) error {
	free, err := store.Transfer(h.name, h.session, toSession)
	if err != nil {
		return fmt.Errorf("move login state %q to session %s: %w", h.name, toSession, err)
	}
	h.free = free
	h.session = toSession
	return nil
}
