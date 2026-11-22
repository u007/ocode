// Package boxsession owns the Upstash Box browser lifecycle. A session's box is
// either ephemeral (one per session, created by Start, deleted on terminate, idle,
// hard cap and shutdown) or retained (one per login state, persistent, paused
// rather than deleted when it goes idle, and resumed by the next Start that asks
// for it). Every box is journaled, so a crashed process's boxes can be swept.
//
// Locking. Manager.mu guards only the session map and the per-entry fields. It
// is never held across a network call or a file write. Each session has an
// entry gate, a channel of capacity one, held for the whole of Start, Terminate,
// teardown and Rekey. Lock order is gate before mu, never the reverse.
package boxsession

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	gopsprocess "github.com/shirou/gopsutil/v4/process"
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/upstashbox"
)

const (
	// leaseTTL is how long a process may stop heartbeating before its ephemeral
	// boxes may count as orphans. An orphan also needs a dead owner PID.
	leaseTTL = 90 * time.Second

	// heartbeatEvery is how often Run refreshes this process's lease.
	heartbeatEvery = 20 * time.Second

	// tickEvery is the reaper period used by Run.
	tickEvery = 20 * time.Second

	// rekeyWait bounds how long Rekey waits for a session's in-flight lifecycle work.
	rekeyWait = 5 * time.Second

	// retainedPauseCap bounds one pause of a retained box. A pause may run under a
	// shutdown deadline, so it is detached from the caller's cancellation.
	retainedPauseCap = 15 * time.Second

	// shutdownSaveFloor is the least a shutdown's final saves get when a retained box
	// has a save pending. A retained box's login state is the only copy of its
	// cookies, so its save runs even when the caller's deadline is nearly gone.
	shutdownSaveFloor = 2 * time.Second

	// sessionNameChars is how many leading session-id characters go into a box name.
	sessionNameChars = 12

	installationIDFile = "installation-id"
)

// ErrNotStarted means no box is running for the session.
var ErrNotStarted = errors.New("no Upstash box is running for this session; call browser action=start with backend=upstash")

// errShutDown is returned by Start once Shutdown has begun. No box is created after it.
var errShutDown = errors.New("boxsession: the manager is shut down; no new Upstash box can be started")

// ErrExpired means the session's ephemeral box reached its time limit and was deleted.
var ErrExpired = errors.New("the Upstash box reached its time limit and was deleted; call browser action=start with backend=upstash again; page state is gone")

// ErrPaused means the session's retained box went idle and was paused. It is not
// deleted: the next Start with retain resumes it.
var ErrPaused = errors.New("the retained Upstash box was paused after it went idle; call browser action=start with backend=upstash and retain=true to resume it; open tabs are gone")

// ErrRetainNeedsLoginState means a retained box was asked for with no login state
// to key it by.
var ErrRetainNeedsLoginState = errors.New("retain needs a login state: a retained box is kept per login state. Ask the user to assign one to this project in Settings, Upstash Box")

// Account is one Upstash account a box can be created in.
type Account struct {
	ID     string
	APIKey string
	// Size is upstashbox.SizeSmall, SizeMedium or SizeLarge, or empty for the server's choice.
	Size string
	// Idle is how long a session may go unused before its box is deleted, or, for
	// a retained box, paused.
	Idle time.Duration
	// HardCap is the ephemeral box's ttl. It is also the longest an ephemeral box lives.
	HardCap time.Duration
	// Retain is how long a retained box is kept after its last use before it is
	// deleted. A retained box has no hard cap.
	Retain time.Duration
}

// BoxAPI is the subset of *upstashbox.Client the manager uses.
type BoxAPI interface {
	Create(ctx context.Context, opts upstashbox.CreateOptions) (*upstashbox.Box, error)
	Get(ctx context.Context, id string) (*upstashbox.Box, error)
	Delete(ctx context.Context, id string) error
	Pause(ctx context.Context, id string) error
	CDPURL(ctx context.Context, id string) (upstashbox.CDPURL, error)
}

// *upstashbox.Client is the production BoxAPI. This assertion fails the build if its
// method set drifts from the interface.
var _ BoxAPI = (*upstashbox.Client)(nil)

// Deps are the manager's collaborators. Every field except Now is required.
type Deps struct {
	// Account resolves the default account. It is called at Start time.
	Account func() (Account, error)
	// APIKeyFor returns the key of any account, so the sweep can delete an orphan of any account.
	APIKeyFor func(accountID string) (string, error)
	// NewAPI builds a box API client for one key.
	NewAPI func(apiKey string) (BoxAPI, error)
	// Dial opens the browser's CDP connection. cdp.DialWebSocket in production.
	Dial func(ctx context.Context, rawURL string) (*cdp.Conn, error)
	// Busy reports whether the session has an active turn or a pending ask.
	Busy func(sessionID string) bool
	// Journal records every box this process creates.
	Journal *Journal
	// Label is the installation label put on every box. See InstallationLabel.
	Label string
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
	// LeaseAlive reports whether the process that wrote a journal lease is still
	// running. Nil means the real check, leaseAlive. The orphan sweep deletes a box
	// only when its owner's lease is dead, and a reused PID does not keep one alive.
	LeaseAlive func(l Lease) bool
	// LoginStates stores the named login states. Nil with LoginStateFor nil means
	// the feature is off: every session's box is a clean browser.
	LoginStates *LoginStateStore
	// LoginStateFor returns the login state a session's new box uses, or "" for
	// none. It is called only when Start creates a box. An error fails Start.
	LoginStateFor func(sessionID string) (string, error)
}

// StartOptions chooses the kind of box Start returns.
type StartOptions struct {
	// Retain asks for a retained box: a persistent box kept per login state. It is
	// paused, not deleted, when it goes idle. It needs a login state and Account.Retain.
	Retain bool
}

// Status describes one session's box.
type Status struct {
	SessionID string
	BoxID     string
	AccountID string
	// Kind is KindEphemeral or KindRetained.
	Kind      string
	CreatedAt time.Time
	// ExpiresAt is the ephemeral box's ttl, or the time a retained box is deleted
	// if it stays unused.
	ExpiresAt time.Time
	LastUsed  time.Time
	// LoginState is the box's login state name, or "" when it has none.
	LoginState string
	// LoginStateNotes says what the restore that created the box could not apply:
	// refused cookies and oversize origins, in words. Empty when all of it applied.
	LoginStateNotes []string
	// Reused is true when Start returned a box that was already running.
	Reused bool
	// Resumed is true when Start resumed a paused retained box instead of creating one.
	Resumed bool
}

// Manager runs the lifecycle of every session's box in this process.
type Manager struct {
	deps Deps
	// err is set when Deps is incomplete. Every operation then returns it.
	err   error
	lease Lease
	// beatEvery and reapEvery are the Run periods. Tests shorten them.
	beatEvery time.Duration
	reapEvery time.Duration
	// newTicker starts a periodic tick for Run. Tests replace it with channels they drive.
	newTicker func(d time.Duration) (<-chan time.Time, func())
	// browserOf returns the storage view of a box's browser. Production returns the
	// *cdp.Manager itself; tests return a fake.
	browserOf func(*cdp.Manager) boxBrowser
	// afterFunc schedules a login-state checkpoint. Tests replace it.
	afterFunc func(d time.Duration, f func()) checkpointTimer
	// checkpointDelay is the debounce between a browser call and its checkpoint.
	checkpointDelay time.Duration
	// checkpointMaxWait is how long calls may defer a checkpoint before one runs at once.
	checkpointMaxWait time.Duration
	// shutdownSaveCap bounds the final saves of a shutdown, before the delete reserve.
	shutdownSaveCap time.Duration
	// onCommit, when set, runs after a capture writes its login state. Tests use it
	// to learn that a save has committed; production leaves it nil.
	onCommit func(name string)
	// beforeClaim, when set, runs in the reaper just before it acts on a retained box:
	// before a sweep row is claimed, and before a queued pause is re-checked. It runs
	// with no lock and no gate held. Tests use it to start a Start inside that window.
	beforeClaim func(boxID string)

	mu       sync.Mutex
	sessions map[string]*entry
	expired  map[string]bool
	// paused marks sessions whose retained box the idle reaper paused. The next
	// Browser call reports ErrPaused. Start and Terminate clear it.
	paused map[string]bool
	// undeleted holds this process's boxes whose delete failed, and Tick retries them.
	// Their journal rows are kept. These are ephemeral boxes, and a fresh retained box
	// whose discard failed, whose row stays this process's and unpaused. A journal
	// row for this process is never swept, so this set is the only retry for them.
	// A resume of the box clears its entry.
	undeleted map[string]BoxAPI
	// unpaused holds this process's retained boxes whose pause, or the journal
	// write after it, failed. The box keeps running and its row stays this
	// process's until a retry pauses it. Tick retries them.
	unpaused map[string]pendingPause
	// resuming marks retained boxes a Start is resuming, from before its journal
	// claim until it publishes or fails. sweeping marks boxes a Tick path is acting
	// on. reserveBox keeps the two apart, under mu, so neither acts on a box the other holds.
	resuming map[string]bool
	sweeping map[string]bool
	// closed is set by Shutdown. Start refuses after it, and a box whose create
	// completes after it is torn down instead of published.
	closed bool
}

// pendingPause is one retained box whose pause did not finish.
type pendingPause struct {
	api BoxAPI
	// lastUsed and expiresAt are the journal values the paused row takes.
	lastUsed  time.Time
	expiresAt time.Time
	// pausedOnAPI is true once the API accepted the pause, and only the journal
	// write is left to retry.
	pausedOnAPI bool
}

// entry is one session's state. The fields below mu are guarded by Manager.mu,
// and gate serializes lifecycle work for the session.
type entry struct {
	gate     chan struct{}
	session  string
	box      *liveBox
	closing  bool
	lastUsed time.Time
	// checkpoint is the pending login-state checkpoint, nil when none is armed.
	checkpoint *pendingCheckpoint
}

// liveBox is a box this process created or resumed and has not yet torn down. Its
// fields are fixed once it is published, except hold, which Rekey moves under the gate.
type liveBox struct {
	id        string
	accountID string
	// kind is KindEphemeral or KindRetained.
	kind      string
	createdAt time.Time
	// expiresAt is the ephemeral box's ttl, or a retained box's delete-at.
	expiresAt time.Time
	idle      time.Duration
	// retain is the account's retain period at start. Teardown of a retained box
	// uses it for the delete-at it journals, so it never reads the account again.
	retain time.Duration
	api    BoxAPI
	// cdp is what Browser returns. storage is the same browser seen through the
	// storage seam, and is what the login-state code calls.
	cdp     *cdp.Manager
	storage boxBrowser
	// hold is this session's hold on the box's login state. Nil when it has none.
	hold *loginHold
	// lastSave is when a save of the box's login state last ran to its end. Guarded by Manager.mu.
	lastSave time.Time
	// preExpirySaved is set once the box has taken its pre-expiry save. Guarded by Manager.mu.
	preExpirySaved bool
}

// New returns a manager. A Deps that is missing a required field is reported by
// every operation, so the misconfiguration is visible without a panic.
func New(d Deps) *Manager {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.LeaseAlive == nil {
		d.LeaseAlive = leaseAlive
	}
	if d.Journal != nil {
		// Resume's compare-and-set must judge owners by the same liveness check as the reaper.
		d.Journal.bindAlive(d.LeaseAlive)
	}
	m := &Manager{
		deps:              d,
		err:               d.validate(),
		beatEvery:         heartbeatEvery,
		reapEvery:         tickEvery,
		newTicker:         realTicker,
		browserOf:         func(cm *cdp.Manager) boxBrowser { return cm },
		afterFunc:         realAfterFunc,
		checkpointDelay:   loginCheckpointDelay,
		checkpointMaxWait: loginCheckpointMaxWait,
		shutdownSaveCap:   loginStateCap,
		sessions:          map[string]*entry{},
		expired:           map[string]bool{},
		paused:            map[string]bool{},
		undeleted:         map[string]BoxAPI{},
		unpaused:          map[string]pendingPause{},
		resuming:          map[string]bool{},
		sweeping:          map[string]bool{},
	}
	m.lease = Lease{PID: os.Getpid(), ProcessStart: m.now()}
	return m
}

// realTicker is the production newTicker.
func realTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// realAfterFunc is the production afterFunc.
func realAfterFunc(d time.Duration, f func()) checkpointTimer {
	return time.AfterFunc(d, f)
}

// now is the manager's clock. Round(0) strips the monotonic reading: a monotonic
// clock does not advance while the machine sleeps, so an idle timer computed
// from it would miss the sleep and a box would outlive its idle period.
func (m *Manager) now() time.Time {
	return m.deps.Now().Round(0)
}

// pidAlive reports whether pid names a running process. It is gopsutil's
// PidExists, the check internal/browse/cdp and internal/discovery use. A probe
// error counts as alive, so the sweep never deletes a box it cannot prove orphaned.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	exists, err := gopsprocess.PidExists(int32(pid))
	if err != nil {
		return true
	}
	return exists
}

func (d Deps) validate() error {
	switch {
	case d.Account == nil:
		return errors.New("boxsession: Deps.Account is nil")
	case d.APIKeyFor == nil:
		return errors.New("boxsession: Deps.APIKeyFor is nil")
	case d.NewAPI == nil:
		return errors.New("boxsession: Deps.NewAPI is nil")
	case d.Dial == nil:
		return errors.New("boxsession: Deps.Dial is nil")
	case d.Busy == nil:
		return errors.New("boxsession: Deps.Busy is nil")
	case d.Journal == nil:
		return errors.New("boxsession: Deps.Journal is nil")
	case d.Label == "":
		return errors.New("boxsession: Deps.Label is empty")
	case (d.LoginStates == nil) != (d.LoginStateFor == nil):
		return errors.New("boxsession: Deps.LoginStates and Deps.LoginStateFor must both be set, or both nil")
	}
	return nil
}

// Start returns the session's running box, or creates one when there is none.
// With opts.Retain it returns the session's retained box for its login state,
// resuming a paused one or creating one. It is the only method that creates a box.
func (m *Manager) Start(ctx context.Context, sessionID string, opts StartOptions) (Status, error) {
	if m.err != nil {
		return Status{}, m.err
	}
	if sessionID == "" {
		return Status{}, errors.New("boxsession: session id is required")
	}
	if m.isClosed() {
		return Status{}, errShutDown
	}
	e, err := m.lockEntry(ctx, sessionID, true)
	if err != nil {
		return Status{}, err
	}
	defer m.unlockEntry(e)
	if m.isClosed() {
		m.dropIfEmpty(e)
		return Status{}, errShutDown
	}

	now := m.now()
	m.mu.Lock()
	if b := e.box; b != nil && reusable(b, now, opts.Retain) {
		e.lastUsed = now
		st := m.statusLocked(e)
		m.mu.Unlock()
		st.Reused = true
		return st, nil
	}
	replace := e.box != nil
	m.mu.Unlock()

	// Every refusal and lookup is made before anything is retired or created, so a
	// failed or refused start leaves the session exactly as it was.
	loginName, err := m.loginStateName(sessionID)
	if err != nil {
		m.dropIfEmpty(e)
		return Status{}, err
	}
	acc, err := m.deps.Account()
	if err != nil {
		m.dropIfEmpty(e)
		return Status{}, fmt.Errorf("boxsession: resolve account: %w", err)
	}
	if opts.Retain {
		if err := acc.validateRetained(); err != nil {
			m.dropIfEmpty(e)
			return Status{}, err
		}
		if loginName == "" {
			m.dropIfEmpty(e)
			return Status{}, ErrRetainNeedsLoginState
		}
	}

	if replace {
		// The live box is past its ttl, or it is ephemeral and this start wants a
		// retained box. retireBox takes the final save and releases the hold, so the
		// replacement can take the name. The entry stays in the map, so the
		// replacement is published on it.
		if err := m.retireBox(ctx, e, teardownOther, true); err != nil {
			log.Printf("boxsession: retire box of session %s before start: %v", sessionID, err)
		}
	}
	m.mu.Lock()
	delete(m.expired, sessionID)
	delete(m.paused, sessionID)
	m.mu.Unlock()

	// The hold is taken before the box is created or resumed, so a name in use
	// starts no box.
	hold, err := m.openLoginState(loginName, sessionID)
	if err != nil {
		m.dropIfEmpty(e)
		return Status{}, err
	}
	// The hold is released unless the start publishes its box. Keyed on success, so
	// a panic inside the start releases it too.
	published := false
	defer func() {
		if !published {
			releaseLogin(hold)
		}
	}()
	var st Status
	if opts.Retain {
		st, err = m.startRetained(ctx, e, acc, sessionID, now, loginName, hold)
	} else {
		st, err = m.create(ctx, e, acc, sessionID, now, hold)
	}
	if err != nil {
		m.dropIfEmpty(e)
		return Status{}, err
	}
	published = true
	return st, nil
}

// reusable reports whether a live box answers a Start with these options. A
// retained start reuses only a retained box. Any other start reuses a retained
// box, and an ephemeral box only while it is inside its ttl.
func reusable(b *liveBox, now time.Time, retain bool) bool {
	if retain {
		return b.kind == KindRetained
	}
	return b.kind == KindRetained || now.Before(b.expiresAt)
}

// isClosed reports whether Shutdown has begun.
func (m *Manager) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// create makes a new ephemeral box for the session and publishes it on e. hold is
// the session's login-state hold, or nil. The caller holds e's gate and releases
// hold when create fails.
func (m *Manager) create(ctx context.Context, e *entry, acc Account, sessionID string, now time.Time, hold *loginHold) (Status, error) {
	ttl, err := acc.ttlSeconds()
	if err != nil {
		return Status{}, err
	}
	api, err := m.deps.NewAPI(acc.APIKey)
	if err != nil {
		return Status{}, fmt.Errorf("boxsession: account %s: %w", acc.ID, err)
	}
	box, err := m.createBox(ctx, api, upstashbox.CreateOptions{
		Name:       boxName(sessionID),
		Size:       acc.Size,
		Browser:    true,
		Ephemeral:  true,
		TTLSeconds: ttl,
		Labels:     []string{m.deps.Label},
		// No NetworkPolicy is set. The denied-private-range policy was probed
		// live on 2026-11-21: custom mode with DeniedCIDRs alone loads no page,
		// and the only working variant needs a domain allowlist, which is a
		// product decision not yet made.
	}, Entry{
		AccountID: acc.ID,
		SessionID: sessionID,
		Kind:      KindEphemeral,
		Label:     m.deps.Label,
		CreatedAt: now,
		// The API's expiry is not known until Create returns, so the row
		// carries now plus the ttl. The sweep does not read it.
		ExpiresAt: now.Add(time.Duration(ttl) * time.Second),
		Lease:     m.leaseAt(now),
	})
	if err != nil {
		return Status{}, err
	}
	if box.ExpiresAt.IsZero() {
		m.discard(ctx, api, box.ID)
		return Status{}, fmt.Errorf("boxsession: create box %s: the box has no expiry", box.ID)
	}
	return m.attach(ctx, e, attachSpec{
		api:       api,
		box:       *box,
		acc:       acc,
		kind:      KindEphemeral,
		sessionID: sessionID,
		now:       now,
		expiresAt: box.ExpiresAt,
		hold:      hold,
		fresh:     true,
	})
}

// startRetained returns the session's retained box for login state name. It
// resumes the box the journal holds for that name, or creates one when there is
// none, or when the box was deleted outside ocode.
func (m *Manager) startRetained(ctx context.Context, e *entry, acc Account, sessionID string, now time.Time, name string, hold *loginHold) (Status, error) {
	row, found, err := m.deps.Journal.Retained(acc.ID, name)
	if err != nil {
		return Status{}, fmt.Errorf("boxsession: read the retained box of login state %q: %w", name, err)
	}
	if found {
		st, gone, err := m.resumeRetained(ctx, e, acc, sessionID, now, row, hold)
		if err != nil || !gone {
			return st, err
		}
	}
	return m.createRetained(ctx, e, acc, sessionID, now, name, hold)
}

// resumeRetained resumes the journaled retained box row. gone is true when the
// box no longer exists: its row is removed and the caller creates a new one.
//
// The order is fixed. The box is reserved against the reaper first. A row a dead
// claimer was deleting is finished, and then the caller creates a new box. The box
// is read next, and the journal claim (Resume, a compare-and-set) comes after the
// read, so a transient read error never leaves a row marked running under this
// process with nothing to retry it. A claimed row clears this process's queued
// pause and delete of the box, because the box is in use now.
func (m *Manager) resumeRetained(ctx context.Context, e *entry, acc Account, sessionID string, now time.Time, row Entry, hold *loginHold) (st Status, gone bool, err error) {
	if !m.reserveBox(row.BoxID, true) {
		return Status{}, false, fmt.Errorf("login state %q: %w", row.LoginState, ErrRetainedBusy)
	}
	defer m.releaseBox(row.BoxID, true)

	api, err := m.deps.NewAPI(acc.APIKey)
	if err != nil {
		return Status{}, false, fmt.Errorf("boxsession: account %s: %w", acc.ID, err)
	}
	if row.Deleting {
		// The delete was claimed by a process that has since died. Its claim is taken
		// over and the box deleted, and the caller then creates a new box.
		action, ok, cerr := m.deps.Journal.ClaimDue(row.BoxID, now, m.leaseAt(now), leaseTTL, m.deps.LeaseAlive)
		if cerr != nil {
			return Status{}, false, fmt.Errorf("boxsession: claim the delete of retained box %s: %w", row.BoxID, cerr)
		}
		if !ok || action != "delete" {
			return Status{}, false, fmt.Errorf("login state %q: %w", row.LoginState, ErrRetainedBusy)
		}
		if err := m.retire(ctx, api, row.BoxID); err != nil {
			m.releaseClaimLogged(row.BoxID)
			return Status{}, false, fmt.Errorf("boxsession: finish the delete of retained box %s: %w", row.BoxID, err)
		}
		return Status{}, true, nil
	}
	box, err := api.Get(ctx, row.BoxID)
	if err != nil && !isNotFound(err) {
		return Status{}, false, fmt.Errorf("boxsession: read retained box %s: %w", row.BoxID, err)
	}
	if err != nil || box.Status == upstashbox.StatusDeleted {
		// The box was deleted outside ocode, such as from the settings UI. Its row
		// goes with it, and the caller creates a new box for the login state.
		log.Printf("boxsession: retained box %s no longer exists; creating a new one for login state %q", row.BoxID, row.LoginState)
		m.dropUnpaused(row.BoxID)
		m.dropUndeleted(row.BoxID)
		if err := m.deps.Journal.Remove(row.BoxID); err != nil {
			return Status{}, false, fmt.Errorf("boxsession: forget deleted box %s in journal: %w", row.BoxID, err)
		}
		return Status{}, true, nil
	}

	expires := now.Add(acc.Retain)
	if err := m.deps.Journal.Resume(row.BoxID, sessionID, m.leaseAt(now), now, expires); err != nil {
		if errors.Is(err, ErrRetainedBusy) {
			return Status{}, false, fmt.Errorf("login state %q: %w", row.LoginState, err)
		}
		return Status{}, false, fmt.Errorf("boxsession: claim retained box %s: %w", row.BoxID, err)
	}
	// The claim makes this start the box's owner. This process's queued pause and
	// queued delete of the box are void now, so neither can touch a box in use.
	m.dropUnpaused(row.BoxID)
	m.dropUndeleted(row.BoxID)
	st, err = m.attach(ctx, e, attachSpec{
		api:       api,
		box:       *box,
		acc:       acc,
		kind:      KindRetained,
		sessionID: sessionID,
		now:       now,
		expiresAt: expires,
		hold:      hold,
		fresh:     false,
	})
	if err != nil {
		return Status{}, false, err
	}
	st.Resumed = true
	return st, false, nil
}

// createRetained creates the persistent box of a login state and publishes it on e.
func (m *Manager) createRetained(ctx context.Context, e *entry, acc Account, sessionID string, now time.Time, name string, hold *loginHold) (Status, error) {
	api, err := m.deps.NewAPI(acc.APIKey)
	if err != nil {
		return Status{}, fmt.Errorf("boxsession: account %s: %w", acc.ID, err)
	}
	// A persistent box has no ttl and no expiry: it is paused when idle, and its
	// delete-at is the journal's, not the API's.
	// shortcut: a persistent box has no ttl, so after a crash it runs until the next ocode start pauses it as a stray (Upstash also auto-pauses idle boxes); upgrade if the API gains a server-side idle cap.
	box, err := m.createBox(ctx, api, upstashbox.CreateOptions{
		Name:    "ocode-ls-" + name,
		Size:    acc.Size,
		Browser: true,
		Labels:  []string{m.deps.Label},
	}, Entry{
		AccountID:  acc.ID,
		SessionID:  sessionID,
		Kind:       KindRetained,
		Label:      m.deps.Label,
		CreatedAt:  now,
		ExpiresAt:  now.Add(acc.Retain),
		Lease:      m.leaseAt(now),
		LoginState: name,
		LastUsed:   now,
	})
	if err != nil {
		return Status{}, err
	}
	return m.attach(ctx, e, attachSpec{
		api:       api,
		box:       *box,
		acc:       acc,
		kind:      KindRetained,
		sessionID: sessionID,
		now:       now,
		expiresAt: now.Add(acc.Retain),
		hold:      hold,
		fresh:     true,
	})
}

// createBox runs one Create whose box is journaled as row the moment the API
// returns its id. A failure after the box exists deletes it, so no box is left
// running without a row or without being used.
func (m *Manager) createBox(ctx context.Context, api BoxAPI, opts upstashbox.CreateOptions, row Entry) (*upstashbox.Box, error) {
	journal := m.journalOnce(row)
	opts.OnCreated = journal
	box, createErr := api.Create(ctx, opts)
	if box != nil && box.ID != "" {
		// Journal before anything looks at the box. Create already did this
		// through OnCreated, so this only matters for an API that skipped it.
		if err := journal(box.ID); err != nil && createErr == nil {
			createErr = err
		}
	}
	if createErr != nil {
		if box != nil && box.ID != "" {
			// The box exists. discard deletes it and removes its row only if the delete succeeds.
			m.discard(ctx, api, box.ID)
		}
		return nil, fmt.Errorf("boxsession: create box: %w", createErr)
	}
	if box == nil || box.ID == "" {
		return nil, errors.New("boxsession: create box: the API returned no box id")
	}
	return box, nil
}

// journalOnce returns the journal hook for one new box. It writes row, with the
// box id, the first time it is called, and is a no-op after that.
func (m *Manager) journalOnce(row Entry) func(boxID string) error {
	journaled := false
	return func(boxID string) error {
		if journaled {
			return nil
		}
		row.BoxID = boxID
		if err := m.deps.Journal.Add(row); err != nil {
			return fmt.Errorf("boxsession: journal box %s: %w", boxID, err)
		}
		journaled = true
		return nil
	}
}

// attachSpec is one box a start has in hand, ready to be attached and published.
type attachSpec struct {
	api       BoxAPI
	box       upstashbox.Box
	acc       Account
	kind      string
	sessionID string
	now       time.Time
	// expiresAt is an ephemeral box's ttl, or a retained box's delete-at.
	expiresAt time.Time
	hold      *loginHold
	// fresh is true when this start created the box. A failure then deletes it. A
	// resumed retained box is paused instead, because its data lives in its
	// login-state file and the box is not this start's to delete.
	fresh bool
}

// attach starts the box's browser, restores the login state into it, and publishes
// the box on e. It is the one tail shared by the ephemeral create, the retained
// create and the retained resume. The caller holds e's gate.
func (m *Manager) attach(ctx context.Context, e *entry, s attachSpec) (Status, error) {
	boxID := s.box.ID
	cdpMgr := cdp.NewAttachManager(cdp.ManagerOptions{Log: log.Default()}, m.dialerFor(s.api, boxID))
	// A replacement browser context is filled from the login state's latest saved
	// file, or emptied when the box has no login state.
	cdpMgr.SetContextRestore(m.contextRestoreFor(s.hold))
	storage := m.browserOf(cdpMgr)
	// Asking for the CDP URL starts the box's browser, which Chromium does on first use.
	// The URL itself is not kept here: the dialer asks for a fresh one each time.
	if _, err := s.api.CDPURL(ctx, boxID); err != nil {
		m.closeBrowser(ctx, storage, boxID)
		if s.fresh {
			m.discard(ctx, s.api, boxID)
		} else if perr := m.parkRetained(ctx, s.api, boxID, s.sessionID, s.acc.Retain); perr != nil {
			// The request may have resumed the box before it failed, so the box is paused
			// as a failed start pauses it. A failed pause is retried by Tick.
			log.Printf("boxsession: %v", perr)
		}
		return Status{}, fmt.Errorf("boxsession: start browser of box %s: %w", boxID, err)
	}
	// From here the browser is running. A failure deletes a box this start created,
	// and pauses a resumed one.
	fail := func(err error) (Status, error) {
		m.closeBrowser(ctx, storage, boxID)
		if s.fresh {
			m.discard(ctx, s.api, boxID)
		} else if perr := m.parkRetained(ctx, s.api, boxID, s.sessionID, s.acc.Retain); perr != nil {
			log.Printf("boxsession: %v", perr)
		}
		return Status{}, err
	}
	// The saved login state goes into the browser before the box is published, so
	// no session ever sees the box without its cookies.
	if s.hold != nil && s.hold.saved != nil {
		if err := m.restoreLoginState(ctx, storage, s.hold); err != nil {
			return fail(err)
		}
	}

	lb := &liveBox{
		id:        boxID,
		accountID: s.acc.ID,
		kind:      s.kind,
		createdAt: s.box.CreatedAt,
		expiresAt: s.expiresAt,
		idle:      s.acc.Idle,
		retain:    s.acc.Retain,
		api:       s.api,
		cdp:       cdpMgr,
		storage:   storage,
		hold:      s.hold,
		// The restore is the box's first state, so the max-wait clock starts here.
		lastSave: s.now,
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		// Shutdown began while this start was in flight. Shutdown waits on this
		// session's gate, so the box is torn down here, not left running.
		return fail(errShutDown)
	}
	e.box = lb
	e.lastUsed = s.now
	st := m.statusLocked(e)
	m.mu.Unlock()
	return st, nil
}

// discard deletes a box that was created but will not be used, and logs a failure.
func (m *Manager) discard(ctx context.Context, api BoxAPI, boxID string) {
	if err := m.ownedRetire(ctx, api, boxID); err != nil {
		log.Printf("boxsession: discard box %s: %v", boxID, err)
	}
}

// closeBrowser closes the browser of a box that will not be published. A failure is logged.
func (m *Manager) closeBrowser(ctx context.Context, storage boxBrowser, boxID string) {
	if err := storage.Close(ctx); err != nil {
		log.Printf("boxsession: close browser of box %s: %v", boxID, err)
	}
}

// dialerFor returns the attach manager's dial function for one box. Every dial
// asks the API for a fresh CDP URL, because the token can change.
func (m *Manager) dialerFor(api BoxAPI, boxID string) func(context.Context) (*cdp.Conn, error) {
	dial := m.deps.Dial
	return func(ctx context.Context) (*cdp.Conn, error) {
		u, err := api.CDPURL(ctx, boxID)
		if err != nil {
			return nil, fmt.Errorf("boxsession: browser URL of box %s: %w", boxID, err)
		}
		conn, err := dial(ctx, u.RevealRaw())
		if err != nil {
			return nil, fmt.Errorf("boxsession: dial browser of box %s: %w", boxID, err)
		}
		return conn, nil
	}
}

// Browser returns the session's browser manager. It never creates a box, and it
// records the session as used now.
func (m *Manager) Browser(sessionID string) (*cdp.Manager, error) {
	if m.err != nil {
		return nil, m.err
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.expired[sessionID] {
		return nil, ErrExpired
	}
	if m.paused[sessionID] {
		return nil, ErrPaused
	}
	e := m.sessions[sessionID]
	if e == nil || e.box == nil || e.closing {
		return nil, ErrNotStarted
	}
	// A retained box has no ttl, so only an ephemeral box can be past it.
	if e.box.kind == KindEphemeral && !now.Before(e.box.expiresAt) {
		return nil, ErrExpired
	}
	e.lastUsed = now
	m.armCheckpointLocked(sessionID, e)
	return e.box.cdp, nil
}

// Terminate tears the session's box down now. An ephemeral box is deleted, and a
// retained box is paused, so its login state can be resumed later. When the session
// has no box and the reaper had expired or paused one, it returns that error once
// and clears the marker, so a closed session leaves nothing behind.
func (m *Manager) Terminate(ctx context.Context, sessionID string) error {
	if m.err != nil {
		return m.err
	}
	e, err := m.lockEntry(ctx, sessionID, false)
	if err != nil {
		return err
	}
	if e != nil {
		defer m.unlockEntry(e)
	}

	m.mu.Lock()
	expired := m.expired[sessionID]
	paused := m.paused[sessionID]
	delete(m.expired, sessionID)
	delete(m.paused, sessionID)
	hasBox := e != nil && e.box != nil
	m.mu.Unlock()
	if !hasBox {
		switch {
		case expired:
			return ErrExpired
		case paused:
			return ErrPaused
		}
		return ErrNotStarted
	}
	if err := m.teardown(ctx, e, teardownOther); err != nil {
		return fmt.Errorf("boxsession: terminate session %s: %w", sessionID, err)
	}
	return nil
}

// Rekey moves a session's box, its expired and paused markers and its journal rows
// to a new session id. It fails when newSession already has a box, and when the old
// session's lifecycle work does not finish within rekeyWait.
func (m *Manager) Rekey(oldSession, newSession string) error {
	if m.err != nil {
		return m.err
	}
	if oldSession == "" || newSession == "" {
		return errors.New("boxsession: rekey needs both session ids")
	}
	if oldSession == newSession {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), rekeyWait)
	defer cancel()

	old, err := m.lockEntry(ctx, oldSession, false)
	if err != nil {
		return fmt.Errorf("boxsession: rekey %s: %w", oldSession, err)
	}
	if old != nil {
		defer m.unlockEntry(old)
	}
	placeholder, err := m.lockEntry(ctx, newSession, true)
	if err != nil {
		return fmt.Errorf("boxsession: rekey to %s: %w", newSession, err)
	}
	defer m.unlockEntry(placeholder)

	m.mu.Lock()
	taken := placeholder.box != nil
	m.mu.Unlock()
	if taken {
		return fmt.Errorf("boxsession: rekey to %s: the session already has a box", newSession)
	}
	moved, err := m.deps.Journal.rekeyMoved(oldSession, newSession)
	if err != nil {
		m.dropIfEmpty(placeholder)
		return fmt.Errorf("boxsession: rekey journal: %w", err)
	}
	// The login-state hold follows the box. The old gate is held, so no checkpoint
	// or teardown reads the box's hold meanwhile. If the hold cannot move, the
	// journal move is reversed, so the rekey either happens whole or not at all. The
	// reversal touches only the rows this move changed, never a row the new session
	// already had.
	if old != nil && old.box != nil && old.box.hold != nil {
		if err := old.box.hold.moveTo(m.deps.LoginStates, newSession); err != nil {
			if rerr := m.deps.Journal.restoreSession(moved, oldSession); rerr != nil {
				log.Printf("boxsession: rekey %s: undo the journal move after the login-state move failed: %v", oldSession, rerr)
			}
			m.dropIfEmpty(placeholder)
			return fmt.Errorf("boxsession: rekey %s: %w", oldSession, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, newSession)
	if old != nil {
		delete(m.sessions, oldSession)
		old.session = newSession
		m.sessions[newSession] = old
	}
	if m.expired[oldSession] {
		delete(m.expired, oldSession)
		m.expired[newSession] = true
	}
	if m.paused[oldSession] {
		delete(m.paused, oldSession)
		m.paused[newSession] = true
	}
	return nil
}

// Statuses returns every running box, sorted by CreatedAt and then session id.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	var out []Status
	for _, e := range m.sessions {
		if e.box == nil || e.closing {
			continue
		}
		out = append(out, m.statusLocked(e))
	}
	m.mu.Unlock()
	slices.SortFunc(out, func(a, b Status) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.SessionID, b.SessionID)
	})
	return out
}

// statusLocked builds a Status. The caller holds m.mu and e.box is non-nil.
func (m *Manager) statusLocked(e *entry) Status {
	st := Status{
		SessionID: e.session,
		BoxID:     e.box.id,
		AccountID: e.box.accountID,
		Kind:      e.box.kind,
		CreatedAt: e.box.createdAt,
		ExpiresAt: e.box.expiresAt,
		LastUsed:  e.lastUsed,
	}
	if e.box.hold != nil {
		st.LoginState = e.box.hold.name
		st.LoginStateNotes = slices.Clone(e.box.hold.notes)
	}
	return st
}

// heartbeat stamps every box this process owns with the current time.
func (m *Manager) heartbeat() {
	now := m.now()
	if err := m.deps.Journal.Heartbeat(m.leaseAt(now), now); err != nil {
		log.Printf("boxsession: heartbeat: %v", err)
	}
}

// Tick runs one reaper pass: heartbeat, expiry and idle reaping for this
// process's boxes, the orphan sweep, retries of failed deletes and pauses, and the
// retained-box sweep.
func (m *Manager) Tick(ctx context.Context) {
	if m.err != nil {
		log.Printf("boxsession: tick skipped: %v", m.err)
		return
	}
	m.heartbeat()
	now := m.now()
	lease := m.leaseAt(now)

	m.mu.Lock()
	var sessions []string
	for id, e := range m.sessions {
		if e.box != nil {
			sessions = append(sessions, id)
		}
	}
	m.mu.Unlock()
	slices.Sort(sessions)

	for _, id := range sessions {
		m.reapSession(ctx, id, now)
	}
	m.sweepOrphans(ctx, lease, now)
	m.retryUndeleted(ctx)
	m.sweepRetained(ctx, lease, now)
	m.retryUnpaused(ctx)
}

// reapSession applies the expiry, busy and idle rules to one session. The
// decision reads state only, and the teardown runs under the session gate.
func (m *Manager) reapSession(ctx context.Context, sessionID string, now time.Time) {
	m.mu.Lock()
	e := m.sessions[sessionID]
	if e == nil || e.box == nil || e.closing {
		m.mu.Unlock()
		return
	}
	b := e.box
	lastUsed := e.lastUsed
	m.mu.Unlock()

	// A retained box has no ttl, so only an ephemeral box reaches its expiry here.
	if b.kind == KindEphemeral && !now.Before(b.expiresAt) {
		m.reap(ctx, sessionID, b, true)
		return
	}
	m.preExpiryCheckpoint(sessionID, b, now)
	if m.deps.Busy(sessionID) {
		// A turn or a pending ask is running: the idle timer does not count it.
		m.mu.Lock()
		if e.box == b {
			e.lastUsed = now
		}
		m.mu.Unlock()
		return
	}
	if now.Sub(lastUsed) >= b.idle {
		m.reap(ctx, sessionID, b, false)
	}
}

// reap tears down the session's box b under the session gate. An expired box
// is marked, so the next Browser or Terminate reports ErrExpired. An idle box is
// checked again under the gate, for Browser use and for a turn that started
// since the decision. Busy is called under the gate and never under m.mu.
func (m *Manager) reap(ctx context.Context, sessionID string, b *liveBox, expired bool) {
	e, err := m.lockEntry(ctx, sessionID, false)
	if err != nil {
		log.Printf("boxsession: reap session %s: %v", sessionID, err)
		return
	}
	if e == nil {
		return
	}
	defer m.unlockEntry(e)

	m.mu.Lock()
	still := e.box == b && !e.closing
	m.mu.Unlock()
	if !still {
		return
	}
	if !expired && m.deps.Busy(sessionID) {
		// A turn started after reapSession decided. It keeps the box, and the idle clock pauses.
		m.mu.Lock()
		if e.box == b {
			e.lastUsed = m.now()
		}
		m.mu.Unlock()
		return
	}

	now := m.now()
	m.mu.Lock()
	idleAgain := now.Sub(e.lastUsed) >= b.idle
	m.mu.Unlock()
	if !expired && !idleAgain {
		return
	}
	reason := teardownIdle
	if expired {
		reason = teardownExpired
	}
	if err := m.teardown(ctx, e, reason); err != nil {
		log.Printf("boxsession: reap session %s (box %s): %v", sessionID, b.id, err)
	}
}

// apiFor returns a box API client for accountID, using that account's key.
func (m *Manager) apiFor(accountID string) (BoxAPI, error) {
	key, err := m.deps.APIKeyFor(accountID)
	if err != nil {
		return nil, fmt.Errorf("key of account %s: %w", accountID, err)
	}
	api, err := m.deps.NewAPI(key)
	if err != nil {
		return nil, fmt.Errorf("account %s: %w", accountID, err)
	}
	return api, nil
}

// sweepOrphans deletes the ephemeral boxes of processes that are gone: lease
// expired and PID dead. A failure keeps the journal row, and the next sweep retries it.
func (m *Manager) sweepOrphans(ctx context.Context, lease Lease, now time.Time) {
	orphans, err := m.deps.Journal.Orphans(lease, now, leaseTTL, m.deps.LeaseAlive)
	if err != nil {
		log.Printf("boxsession: sweep: %v", err)
		return
	}
	for _, o := range orphans {
		api, err := m.apiFor(o.AccountID)
		if err != nil {
			log.Printf("boxsession: sweep box %s: %v", o.BoxID, err)
			continue
		}
		if err := m.retire(ctx, api, o.BoxID); err != nil {
			log.Printf("boxsession: sweep box %s: %v", o.BoxID, err)
		}
	}
}

// sweepRetained acts on the retained rows the journal lists as due: it deletes the
// boxes whose delete-at has passed, and pauses the running boxes of a dead process.
// The snapshot is only a candidate list. Each row is claimed in the journal before
// it is touched, and the claim re-checks the row, so a Start that resumed a box
// after the snapshot is never deleted or paused under its session. A failure keeps
// the row for the next Tick. A box a live session holds is never touched.
func (m *Manager) sweepRetained(ctx context.Context, lease Lease, now time.Time) {
	expired, strays, err := m.deps.Journal.RetainedDue(now, lease, leaseTTL, m.deps.LeaseAlive)
	if err != nil {
		log.Printf("boxsession: sweep retained boxes: %v", err)
		return
	}
	for _, r := range append(expired, strays...) {
		m.sweepRow(ctx, r, lease, now)
	}
}

// sweepRow acts on one retained row of the sweep snapshot. The reservation and the
// journal claim are both taken before any API call.
func (m *Manager) sweepRow(ctx context.Context, r Entry, lease Lease, now time.Time) {
	if m.beforeClaim != nil {
		m.beforeClaim(r.BoxID)
	}
	api, err := m.apiFor(r.AccountID)
	if err != nil {
		log.Printf("boxsession: sweep retained box %s: %v", r.BoxID, err)
		return
	}
	if !m.reserveBox(r.BoxID, false) {
		return
	}
	defer m.releaseBox(r.BoxID, false)

	action, ok, err := m.deps.Journal.ClaimDue(r.BoxID, now, lease, leaseTTL, m.deps.LeaseAlive)
	if err != nil {
		log.Printf("boxsession: claim retained box %s: %v", r.BoxID, err)
		return
	}
	if !ok {
		// The row changed since the snapshot. A Start or a live process owns it now.
		return
	}
	switch action {
	case "delete":
		if err := m.retire(ctx, api, r.BoxID); err != nil {
			log.Printf("boxsession: sweep retained box %s: %v", r.BoxID, err)
			m.releaseClaimLogged(r.BoxID)
		}
	case "pause":
		m.pauseStray(ctx, api, r.BoxID)
	}
}

// pauseStray pauses a claimed retained box of a dead process. A failed pause gives
// the claim back, so the row is a dead owner's again and the next sweep retries it.
// A failed journal write after a good pause is queued for retryUnpaused, because the
// row is this process's by then and only that retry can finish it.
func (m *Manager) pauseStray(ctx context.Context, api BoxAPI, boxID string) {
	if err := m.pauseRetained(ctx, api, boxID); err != nil {
		if isNotFound(err) {
			// The box is gone at the API, so no later sweep can pause it. Its row goes too.
			m.forgetGone(boxID)
			return
		}
		log.Printf("boxsession: pause stray retained box %s of a dead process: %v; the next sweep retries it", boxID, err)
		m.releaseClaimLogged(boxID)
		return
	}
	// Zero times keep the row's values, which the claim left as they were. A snapshot
	// value would overwrite a Start's fresh claim.
	if err := m.deps.Journal.SetPaused(boxID, time.Time{}, time.Time{}); err != nil {
		log.Printf("boxsession: journal stray retained box %s as paused: %v; Tick retries the journal write", boxID, err)
		m.setUnpaused(boxID, pendingPause{api: api, pausedOnAPI: true})
	}
}

// releaseClaimLogged gives this process's claim on a retained row back after the
// action it was taken for failed. The row then waits for the next sweep. A failure
// to release is logged.
func (m *Manager) releaseClaimLogged(boxID string) {
	if err := m.deps.Journal.releaseClaim(boxID, m.lease); err != nil {
		log.Printf("boxsession: release the claim of retained box %s: %v", boxID, err)
	}
}

// retryUndeleted deletes again the boxes whose earlier delete failed. A success
// leaves the retry set. A box a live session holds is dropped from it. A box whose
// resume is in flight is skipped for now: a failed resume re-queues its box.
func (m *Manager) retryUndeleted(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.undeleted))
	for id := range m.undeleted {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	slices.Sort(ids)
	for _, id := range ids {
		if !m.reserveBox(id, false) {
			if m.holdsBox(id) {
				m.dropUndeleted(id)
			}
			continue
		}
		m.mu.Lock()
		api, queued := m.undeleted[id]
		m.mu.Unlock()
		if queued {
			if err := m.retire(ctx, api, id); err != nil {
				log.Printf("boxsession: retry delete of box %s: %v", id, err)
			} else {
				m.dropUndeleted(id)
			}
		}
		m.releaseBox(id, false)
	}
}

// retryUnpaused finishes the pauses that failed. A box a live session holds again
// is dropped from the set: that session's own teardown pauses it, and a pause from
// here would pause a browser in use. A box whose resume is in flight is skipped, and
// its entry is kept. The membership is re-read under mu once the box is reserved,
// so a Start that resumed the box after the id was listed is never paused under it.
func (m *Manager) retryUnpaused(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.unpaused))
	for id := range m.unpaused {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	slices.Sort(ids)
	for _, id := range ids {
		if m.beforeClaim != nil {
			m.beforeClaim(id)
		}
		if !m.reserveBox(id, false) {
			if m.holdsBox(id) {
				m.dropUnpaused(id)
			}
			continue
		}
		m.mu.Lock()
		p, queued := m.unpaused[id]
		m.mu.Unlock()
		if queued {
			m.finishPause(ctx, id, p)
		}
		m.releaseBox(id, false)
	}
}

// finishPause completes one queued pause: the API pause if it has not happened,
// then the journal write. A success leaves the retry set. The caller holds the
// reservation of id.
func (m *Manager) finishPause(ctx context.Context, id string, p pendingPause) {
	if !p.pausedOnAPI {
		if err := m.pauseRetained(ctx, p.api, id); err != nil {
			if isNotFound(err) {
				m.forgetGone(id)
				return
			}
			log.Printf("boxsession: retry pause of retained box %s: %v", id, err)
			return
		}
		p.pausedOnAPI = true
		// Record the pause, so a failed journal write below is retried without a second pause.
		m.setUnpaused(id, p)
	}
	if err := m.deps.Journal.SetPaused(id, p.lastUsed, p.expiresAt); err != nil {
		log.Printf("boxsession: retry journal of paused retained box %s: %v", id, err)
		return
	}
	m.dropUnpaused(id)
}

// forgetGone drops a retained box the API no longer has: its journal row and its
// retry entries. It touches no live side. A caller in a teardown has already closed
// the browser, and a reaper caller reserved the box, which excludes a live holder.
func (m *Manager) forgetGone(boxID string) {
	m.dropUnpaused(boxID)
	m.dropUndeleted(boxID)
	if err := m.deps.Journal.Remove(boxID); err != nil {
		log.Printf("boxsession: forget gone retained box %s in journal: %v", boxID, err)
		return
	}
	log.Printf("boxsession: retained box %s no longer exists at the API; its journal row is removed", boxID)
}

// Run is the reaper loop and blocks until ctx ends. The lease heartbeat runs on
// its own goroutine every heartbeatEvery, so a slow reap or sweep pass (network
// teardown) cannot starve it. Tick runs every tickEvery. Start it with
// crashguard.Go(func() { m.Run(ctx) }).
func (m *Manager) Run(ctx context.Context) {
	if m.err != nil {
		log.Printf("boxsession: reaper not started: %v", m.err)
		return
	}
	beating := make(chan struct{})
	beat, stopBeat := m.newTicker(m.beatEvery)
	crashguard.Go(func() {
		defer close(beating)
		defer stopBeat()
		for {
			select {
			case <-ctx.Done():
				return
			case <-beat:
				m.heartbeat()
			}
		}
	})
	// Wait for the heartbeat goroutine, so Run never returns while it still writes.
	defer func() { <-beating }()

	reap, stopReap := m.newTicker(m.reapEvery)
	defer stopReap()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reap:
			m.Tick(ctx)
		}
	}
}

// Shutdown refuses every later Start, then saves the login state of every box that
// has one, then tears every box down: an ephemeral box is deleted and a retained
// box is paused, and any failed delete or pause is retried.
//
// The two phases are separate. First, every final save runs at once under one
// deadline: the shorter of shutdownSaveCap and the caller's deadline less
// shutdownDeleteReserve, floored at shutdownSaveFloor when a retained box has a
// save pending. A save still running at that deadline is abandoned and
// keeps its previous file. Then every box is closed and torn down, concurrently,
// bounded by ctx. The final save of a box always comes before its delete or pause,
// and no box is read once its teardown starts.
//
// It takes each session's gate, so a Start still in flight finishes first, and its
// box is torn down by that Start or by this call.
func (m *Manager) Shutdown(ctx context.Context) {
	if m.err != nil {
		log.Printf("boxsession: shutdown skipped: %v", m.err)
		return
	}
	m.mu.Lock()
	m.closed = true
	saves := m.loginSavesLocked()
	var sessions []string
	for id := range m.sessions {
		sessions = append(sessions, id)
	}
	m.mu.Unlock()
	slices.Sort(sessions)

	// A retained box's login state is the only copy of its cookies, so when one has a
	// save pending the saves get at least shutdownSaveFloor, even on a deadline that
	// is nearly gone. The save phase is then detached from that deadline, so the
	// floor is real and not cancelled before the first read.
	budget := m.shutdownSaveBudget(ctx)
	saveCtx := ctx
	if hasRetainedSave(saves) && budget < shutdownSaveFloor {
		budget = shutdownSaveFloor
		saveCtx = context.WithoutCancel(ctx)
	}
	m.finalSaves(saveCtx, budget, saves)

	var wg sync.WaitGroup
	for _, id := range sessions {
		wg.Add(1)
		crashguard.Go(func() {
			defer wg.Done()
			m.shutdownTeardown(ctx, id)
		})
	}
	wg.Wait()
	m.retryUndeleted(ctx)
	m.retryUnpaused(ctx)
}

// shutdownSaveBudget is the deadline of a shutdown's final saves: shutdownSaveCap,
// cut so that shutdownDeleteReserve of the caller's deadline is left for the deletes.
func (m *Manager) shutdownSaveBudget(ctx context.Context) time.Duration {
	budget := m.shutdownSaveCap
	if dl, ok := ctx.Deadline(); ok {
		if rem := time.Until(dl) - shutdownDeleteReserve; rem < budget {
			budget = rem
		}
	}
	return budget
}

// hasRetainedSave reports whether one of the pending final saves is a retained box's.
func hasRetainedSave(saves []pendingSave) bool {
	for _, s := range saves {
		if s.b.kind == KindRetained {
			return true
		}
	}
	return false
}

// shutdownTeardown closes and tears down one session's box during a shutdown. Its
// final save already ran in the save phase, so the teardown does not save again.
func (m *Manager) shutdownTeardown(ctx context.Context, sessionID string) {
	e, err := m.lockEntry(ctx, sessionID, false)
	if err != nil {
		log.Printf("boxsession: shutdown session %s: %v", sessionID, err)
		return
	}
	if e == nil {
		return
	}
	defer m.unlockEntry(e)
	if err := m.retireBox(ctx, e, teardownOther, false); err != nil {
		log.Printf("boxsession: shutdown session %s: %v", sessionID, err)
	}
	m.dropIfEmpty(e)
}

// teardownReason says why a box is torn down. It decides which marker the session
// is left with.
type teardownReason int

const (
	// teardownOther is an explicit Terminate, a shutdown, or a replacement. It leaves no marker.
	teardownOther teardownReason = iota
	// teardownIdle is the idle reaper, and PauseBox's detach of a live session. A
	// retained box leaves the session paused.
	teardownIdle
	// teardownExpired is the reaper at an ephemeral box's ttl. The session is marked expired.
	teardownExpired
)

// teardown tears the session's box down and then drops the session's map entry if
// it holds no box. The caller holds e's gate. A failed delete keeps the journal
// row and queues the box for retry.
func (m *Manager) teardown(ctx context.Context, e *entry, reason teardownReason) error {
	err := m.retireBox(ctx, e, reason, true)
	m.dropIfEmpty(e)
	return err
}

// retireBox is the one teardown path. In order, under the caller's gate: mark the
// entry closing and cancel the pending checkpoint; take the final login-state save
// (when the box has a login state); close the browser; then either delete the box
// and its journal row (ephemeral) or pause it and journal it paused (retained);
// release the login-state hold. The hold is released even when the delete or pause
// fails: nothing writes the state after this point. A failed delete is retried by
// the sweep. A failed pause is retried by Tick. The map entry stays, so a caller
// that keeps the gate can publish a new box on it. The caller holds e's gate.
//
// finalSave says whether this teardown takes the final login-state save itself. A
// shutdown saves every box in one phase before any teardown, and passes false.
func (m *Manager) retireBox(ctx context.Context, e *entry, reason teardownReason, finalSave bool) error {
	m.mu.Lock()
	b := e.box
	if b == nil {
		m.mu.Unlock()
		return nil
	}
	e.closing = true
	session := e.session
	switch {
	case b.kind == KindEphemeral && reason == teardownExpired:
		m.expired[session] = true
	case b.kind == KindRetained && reason == teardownIdle:
		m.paused[session] = true
	}
	// A timer that already fired finds its token gone, so it cannot save this box.
	if e.checkpoint != nil {
		e.checkpoint.timer.Stop()
		e.checkpoint = nil
	}
	m.mu.Unlock()

	var errs []error
	if finalSave && b.hold != nil {
		m.finalLoginSave(ctx, session, b)
	}
	if err := b.storage.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("close browser of box %s: %w", b.id, err))
	}
	if b.kind == KindRetained {
		if err := m.parkRetained(ctx, b.api, b.id, session, b.retain); err != nil {
			errs = append(errs, err)
		}
	} else if err := m.ownedRetire(ctx, b.api, b.id); err != nil {
		errs = append(errs, err)
	}
	if err := b.hold.unhold(); err != nil {
		errs = append(errs, err)
	}

	m.mu.Lock()
	e.box = nil
	e.closing = false
	m.mu.Unlock()
	return errors.Join(errs...)
}

// parkRetained pauses a retained box and journals it paused, with its delete-at
// retain after now. The browser must already be closed.
//
// A failed pause leaves the box running. The row is first written running under
// this process, and only then is the box queued in the unpaused set, so a Tick that
// retries the pause runs after the row says running and its own paused write is the
// last one. Tick retries the pause. A failed journal write after a successful pause
// is retried the same way, without a second pause.
func (m *Manager) parkRetained(ctx context.Context, api BoxAPI, boxID, sessionID string, retain time.Duration) error {
	now := m.now()
	expires := now.Add(retain)
	p := pendingPause{api: api, lastUsed: now, expiresAt: expires}
	if err := m.pauseRetained(ctx, api, boxID); err != nil {
		if isNotFound(err) {
			// The box is gone at the API, so there is nothing left to pause or to keep.
			m.forgetGone(boxID)
			return nil
		}
		// The box keeps running, so its row stays this process's. A crash now leaves
		// a running box with a dead owner, which the stray rule pauses.
		if rerr := m.deps.Journal.Resume(boxID, sessionID, m.leaseAt(now), now, expires); rerr != nil {
			log.Printf("boxsession: mark box %s running after a failed pause: %v", boxID, rerr)
		}
		m.setUnpaused(boxID, p)
		return fmt.Errorf("pause retained box %s: %w; the box keeps running and Tick retries the pause", boxID, err)
	}
	p.pausedOnAPI = true
	if err := m.deps.Journal.SetPaused(boxID, now, expires); err != nil {
		m.setUnpaused(boxID, p)
		return fmt.Errorf("journal retained box %s as paused: %w; Tick retries the journal write", boxID, err)
	}
	return nil
}

// pauseRetained pauses one retained box through the API. It is detached from the
// caller's cancellation, so a pause under a shutdown deadline still runs, and it is
// bounded by retainedPauseCap.
func (m *Manager) pauseRetained(ctx context.Context, api BoxAPI, boxID string) error {
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), retainedPauseCap)
	defer cancel()
	return api.Pause(pctx, boxID)
}

// setUnpaused queues a retained box for a pause retry.
func (m *Manager) setUnpaused(boxID string, p pendingPause) {
	m.mu.Lock()
	m.unpaused[boxID] = p
	m.mu.Unlock()
}

// dropUnpaused removes a box from the pause retry set.
func (m *Manager) dropUnpaused(boxID string) {
	m.mu.Lock()
	delete(m.unpaused, boxID)
	m.mu.Unlock()
}

// dropUndeleted removes a box from the delete retry set.
func (m *Manager) dropUndeleted(boxID string) {
	m.mu.Lock()
	delete(m.undeleted, boxID)
	m.mu.Unlock()
}

// reserveBox takes this process's reservation of boxID, so that a Start and the
// reaper never act on one box at once. resume is true for a Start (the resuming
// map) and false for a reaper path (the sweeping map). The check and the mark are
// one step under mu. It fails when a live session holds the box or another
// reservation of it exists, so the caller must skip the box.
func (m *Manager) reserveBox(boxID string, resume bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resuming[boxID] || m.sweeping[boxID] || m.holdsBoxLocked(boxID) {
		return false
	}
	if resume {
		m.resuming[boxID] = true
	} else {
		m.sweeping[boxID] = true
	}
	return true
}

// releaseBox drops a reservation taken by reserveBox with the same resume flag.
func (m *Manager) releaseBox(boxID string, resume bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if resume {
		delete(m.resuming, boxID)
	} else {
		delete(m.sweeping, boxID)
	}
}

// holdsBox reports whether a live session of this process holds boxID. A session
// that is tearing its box down does not count: the teardown finishes its own work.
func (m *Manager) holdsBox(boxID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.holdsBoxLocked(boxID)
}

// holdsBoxLocked is holdsBox for a caller that already holds m.mu.
func (m *Manager) holdsBoxLocked(boxID string) bool {
	for _, e := range m.sessions {
		if e.box != nil && !e.closing && e.box.id == boxID {
			return true
		}
	}
	return false
}

// sessionOfBox returns the session whose entry holds boxID, or "" when none does.
func (m *Manager) sessionOfBox(boxID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.sessions {
		if e.box != nil && e.box.id == boxID {
			return id
		}
	}
	return ""
}

// ForgetBox drops a box that was deleted from outside ocode, such as from the
// settings UI. Its journal row goes, and a live session holding it is torn down on
// this side only: the browser is closed and the login-state hold released. No API
// call is made.
func (m *Manager) ForgetBox(boxID string) error {
	if m.err != nil {
		return m.err
	}
	if boxID == "" {
		return errors.New("boxsession: forget box needs a box id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginStateCap)
	defer cancel()
	if err := m.forgetLive(ctx, boxID); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.undeleted, boxID)
	m.mu.Unlock()
	m.dropUnpaused(boxID)
	return m.deps.Journal.Remove(boxID)
}

// PauseBox pauses a retained box of this installation from outside its session,
// such as from the settings UI. handled is false when the journal holds no retained
// row for boxID; the caller then pauses it another way.
//
// The box's session gate is taken first. It is the live holder's gate, or the row's
// session's gate when nothing holds the box. That gate serializes the pause with a
// Start that is creating or resuming the box, so a pause never lands between a
// row's journal write and the box's publish, where the attach's connect would
// resume a box the row says is paused.
//
// A box a live session holds is detached first, through retireBox: the checkpoint is
// cancelled, the final login save runs, the browser closes, the session is marked
// paused, and only then is the box paused through parkRetained. The session is
// closing for that whole sequence, so nothing can redial the box, and a failed pause
// is queued for Tick. A box no session holds is paused through the API, and its row
// is marked paused with its last-use and delete-at kept. A row another live process
// runs, or one being deleted, is refused with ErrRetainedBusy.
func (m *Manager) PauseBox(ctx context.Context, boxID string) (handled bool, err error) {
	if m.err != nil {
		return false, m.err
	}
	if boxID == "" {
		return false, errors.New("boxsession: pause box needs a box id")
	}
	row, found, err := m.journaledRetained(boxID)
	if err != nil || !found {
		return false, err
	}
	session := m.sessionOfBox(boxID)
	if session == "" {
		session = row.SessionID
	}
	e, err := m.lockEntry(ctx, session, false)
	if err != nil {
		return true, fmt.Errorf("boxsession: pause box %s: %w", boxID, err)
	}
	if e != nil {
		defer m.unlockEntry(e)
		if done, err := m.detachHeldBox(ctx, e, boxID); done {
			return true, err
		}
	}
	return true, m.pauseUnheldBox(ctx, boxID)
}

// detachHeldBox detaches the box e holds and pauses it, under e's gate. done is false
// when e does not hold boxID, and the caller then handles the box as unheld.
func (m *Manager) detachHeldBox(ctx context.Context, e *entry, boxID string) (done bool, err error) {
	m.mu.Lock()
	held := e.box != nil && e.box.id == boxID
	m.mu.Unlock()
	if !held {
		return false, nil
	}
	// retireBox runs the final save, closes the browser, pauses the box and releases
	// the hold, in that order. The teardownIdle reason marks the session paused.
	err = m.retireBox(ctx, e, teardownIdle, true)
	m.dropIfEmpty(e)
	if err != nil {
		return true, fmt.Errorf("boxsession: pause box %s: %w", boxID, err)
	}
	return true, nil
}

// journaledRetained returns the retained journal row of boxID. found is false when
// there is none.
func (m *Manager) journaledRetained(boxID string) (Entry, bool, error) {
	rows, err := m.deps.Journal.List()
	if err != nil {
		return Entry{}, false, err
	}
	for _, e := range rows {
		if e.BoxID == boxID && e.Kind == KindRetained {
			return e, true, nil
		}
	}
	return Entry{}, false, nil
}

// pauseLiveBox detaches the box session holds and pauses it. done is false when the
// session no longer holds boxID once its gate is taken, and the caller then handles
// the box as unheld.
func (m *Manager) pauseLiveBox(ctx context.Context, session, boxID string) (done bool, err error) {
	e, err := m.lockEntry(ctx, session, false)
	if err != nil {
		return true, fmt.Errorf("boxsession: pause box %s: %w", boxID, err)
	}
	if e == nil {
		return false, nil
	}
	defer m.unlockEntry(e)

	m.mu.Lock()
	held := e.box != nil && e.box.id == boxID
	m.mu.Unlock()
	if !held {
		m.dropIfEmpty(e)
		return false, nil
	}
	// retireBox runs the final save, closes the browser, pauses the box and releases
	// the hold, in that order. The teardownIdle reason marks the session paused.
	err = m.retireBox(ctx, e, teardownIdle, true)
	m.dropIfEmpty(e)
	if err != nil {
		return true, fmt.Errorf("boxsession: pause box %s: %w", boxID, err)
	}
	return true, nil
}

// pauseUnheldBox pauses a retained box no live session holds. It reserves the box
// against a Start and the reaper, re-reads the row under that reservation, and pauses
// through the API. A failed pause is queued for Tick, and a failed journal write
// after a good pause is queued without a second pause.
func (m *Manager) pauseUnheldBox(ctx context.Context, boxID string) error {
	if !m.reserveBox(boxID, false) {
		return fmt.Errorf("boxsession: box %s: %w", boxID, ErrRetainedBusy)
	}
	defer m.releaseBox(boxID, false)

	row, found, err := m.journaledRetained(boxID)
	if err != nil || !found {
		return err
	}
	now := m.now()
	if row.Deleting || (!row.Paused && !sameOwner(row.Lease, m.lease) && !leaseDead(row.Lease, now, leaseTTL, m.deps.LeaseAlive)) {
		return fmt.Errorf("boxsession: box %s: %w", boxID, ErrRetainedBusy)
	}
	api, err := m.apiFor(row.AccountID)
	if err != nil {
		return err
	}
	if err := m.pauseRetained(ctx, api, boxID); err != nil {
		if isNotFound(err) {
			m.forgetGone(boxID)
			return nil
		}
		// Zero times keep the row's last-use and delete-at when the retry lands.
		m.setUnpaused(boxID, pendingPause{api: api})
		return fmt.Errorf("pause retained box %s: %w; Tick retries the pause", boxID, err)
	}
	// Zero times keep the row's last-use and delete-at.
	if err := m.deps.Journal.SetPaused(boxID, time.Time{}, time.Time{}); err != nil {
		m.setUnpaused(boxID, pendingPause{api: api, pausedOnAPI: true})
		return fmt.Errorf("journal retained box %s as paused: %w; Tick retries the journal write", boxID, err)
	}
	return nil
}

// forgetLive tears down the live side of boxID without calling the API. The
// browser is closed and the login-state hold released.
func (m *Manager) forgetLive(ctx context.Context, boxID string) error {
	session := m.sessionOfBox(boxID)
	if session == "" {
		return nil
	}
	e, err := m.lockEntry(ctx, session, false)
	if err != nil {
		return fmt.Errorf("boxsession: forget box %s: %w", boxID, err)
	}
	if e == nil {
		return nil
	}
	defer m.unlockEntry(e)

	m.mu.Lock()
	b := e.box
	if b == nil || b.id != boxID {
		m.mu.Unlock()
		m.dropIfEmpty(e)
		return nil
	}
	e.closing = true
	if e.checkpoint != nil {
		e.checkpoint.timer.Stop()
		e.checkpoint = nil
	}
	m.mu.Unlock()

	m.closeBrowser(ctx, b.storage, b.id)
	herr := b.hold.unhold()

	m.mu.Lock()
	e.box = nil
	e.closing = false
	m.mu.Unlock()
	m.dropIfEmpty(e)
	if herr != nil {
		return fmt.Errorf("boxsession: forget box %s: %w", boxID, herr)
	}
	return nil
}

// retire deletes one box and then its journal row. A 404 counts as deleted.
// On error the journal row is kept.
func (m *Manager) retire(ctx context.Context, api BoxAPI, boxID string) error {
	if err := api.Delete(ctx, boxID); err != nil && !isNotFound(err) {
		return fmt.Errorf("delete box %s: %w", boxID, err)
	}
	if err := m.deps.Journal.Remove(boxID); err != nil {
		return fmt.Errorf("forget box %s in journal: %w", boxID, err)
	}
	return nil
}

// ownedRetire is retire for a box this process created and has not published, or an
// ephemeral box it owns. A failure also queues the box for retry, and the retry
// deletes it. discard calls it for a fresh retained box too: that box was never
// published, so no session holds it, and its row is this process's. Never call it
// for a retained box a session holds or a resume has claimed, since the retry would
// then delete a box in use.
func (m *Manager) ownedRetire(ctx context.Context, api BoxAPI, boxID string) error {
	if err := m.retire(ctx, api, boxID); err != nil {
		m.mu.Lock()
		m.undeleted[boxID] = api
		m.mu.Unlock()
		return err
	}
	return nil
}

func isNotFound(err error) bool {
	var apiErr *upstashbox.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// lockEntry takes a session's gate and returns its entry. With create false it
// returns nil, nil when the session has no entry. A waiter that finds the entry
// replaced re-looks it up. The wait ends when ctx does; a free gate is always taken.
func (m *Manager) lockEntry(ctx context.Context, sessionID string, create bool) (*entry, error) {
	for {
		m.mu.Lock()
		e := m.sessions[sessionID]
		if e == nil {
			if !create {
				m.mu.Unlock()
				return nil, nil
			}
			e = &entry{gate: make(chan struct{}, 1), session: sessionID}
			m.sessions[sessionID] = e
		}
		m.mu.Unlock()

		// A free gate is taken even when ctx is already done: ctx bounds the wait,
		// and a shutdown that has run out of time still tears down idle boxes.
		select {
		case e.gate <- struct{}{}:
		default:
			select {
			case e.gate <- struct{}{}:
			case <-ctx.Done():
				return nil, fmt.Errorf("session %s is busy: %w", sessionID, ctx.Err())
			}
		}
		m.mu.Lock()
		current := m.sessions[sessionID] == e
		m.mu.Unlock()
		if current {
			return e, nil
		}
		<-e.gate
	}
}

func (m *Manager) unlockEntry(e *entry) {
	<-e.gate
}

// dropIfEmpty removes an entry that holds no box. The caller holds its gate.
func (m *Manager) dropIfEmpty(e *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.box == nil && m.sessions[e.session] == e {
		delete(m.sessions, e.session)
	}
}

func (m *Manager) leaseAt(now time.Time) Lease {
	return Lease{PID: m.lease.PID, ProcessStart: m.lease.ProcessStart, Heartbeat: now}
}

// ttlSeconds validates the account's timing and returns the ephemeral box ttl in seconds.
func (a Account) ttlSeconds() (int, error) {
	switch {
	case a.ID == "":
		return 0, errors.New("boxsession: account has no id")
	case a.HardCap <= 0:
		return 0, fmt.Errorf("boxsession: account %s: hard cap must be positive", a.ID)
	case a.Idle <= 0:
		return 0, fmt.Errorf("boxsession: account %s: idle timeout must be positive", a.ID)
	case a.Idle > a.HardCap:
		return 0, fmt.Errorf("boxsession: account %s: idle timeout %s exceeds the hard cap %s", a.ID, a.Idle, a.HardCap)
	}
	secs := int(a.HardCap / time.Second)
	if secs < 1 {
		return 0, fmt.Errorf("boxsession: account %s: hard cap %s is under one second", a.ID, a.HardCap)
	}
	if secs > upstashbox.MaxTTLSeconds {
		return 0, fmt.Errorf("boxsession: account %s: hard cap %s exceeds the %d s limit", a.ID, a.HardCap, upstashbox.MaxTTLSeconds)
	}
	return secs, nil
}

// validateRetained checks the timing a retained box needs: an id, an idle timeout
// that pauses it, and a retain period that deletes it.
func (a Account) validateRetained() error {
	switch {
	case a.ID == "":
		return errors.New("boxsession: account has no id")
	case a.Idle <= 0:
		return fmt.Errorf("boxsession: account %s: idle timeout must be positive", a.ID)
	case a.Retain <= 0:
		return fmt.Errorf("boxsession: account %s: retain period must be positive", a.ID)
	}
	return nil
}

// boxName is "ocode-" plus the first sessionNameChars characters of the session id.
func boxName(sessionID string) string {
	r := []rune(sessionID)
	if len(r) > sessionNameChars {
		r = r[:sessionNameChars]
	}
	return "ocode-" + string(r)
}

var installationIDPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)

// InstallationLabel returns "ocode-<8 hex>" for this installation. The id is
// stored in <dataDir>/installation-id and created once, atomically. A stored id
// that is not valid is an error, never a silent regeneration.
func InstallationLabel(dataDir string) (string, error) {
	if dataDir == "" {
		return "", errors.New("boxsession: installation label needs a data directory")
	}
	path := filepath.Join(dataDir, installationIDFile)
	if id, found, err := readInstallationID(path); err != nil {
		return "", err
	} else if found {
		return "ocode-" + id, nil
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", fmt.Errorf("boxsession: create data directory: %w", err)
	}
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("boxsession: generate installation id: %w", err)
	}
	id := hex.EncodeToString(raw[:])

	tmp, err := os.CreateTemp(dataDir, ".installation-id-*.tmp")
	if err != nil {
		return "", fmt.Errorf("boxsession: create temp installation id: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(id + "\n"); err != nil {
		tmp.Close()
		return "", fmt.Errorf("boxsession: write temp installation id: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("boxsession: close temp installation id: %w", err)
	}
	// Link fails if the file exists, so a concurrent creator wins once and the
	// loser reads the winner's id.
	if err := os.Link(tmpName, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("boxsession: publish installation id: %w", err)
		}
		existing, found, rerr := readInstallationID(path)
		if rerr != nil {
			return "", rerr
		}
		if !found {
			return "", fmt.Errorf("boxsession: installation id %s vanished during creation", path)
		}
		return "ocode-" + existing, nil
	}
	return "ocode-" + id, nil
}

// readInstallationID reads a stored id. found is false when the file is absent.
func readInstallationID(path string) (id string, found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("boxsession: read installation id: %w", err)
	}
	id = strings.TrimSpace(string(data))
	if !installationIDPattern.MatchString(id) {
		return "", false, fmt.Errorf("boxsession: %s does not hold an 8-character hex installation id", path)
	}
	return id, true, nil
}
