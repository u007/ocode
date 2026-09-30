package server

import (
	"log"
	"sync"
	"time"

	"github.com/u007/ocode/internal/remote"
)

// Forward liveness monitoring.
//
// internal/remote's ForwardManager owns each `ssh -N -L` child's Wait, so a
// forward that dies on its own (network drop, laptop sleep/wake, host reboot)
// is observed the moment it happens and reported through ForwardManager's
// SetOnExit hook — it is never inferred from stale bookkeeping. This file is
// the policy half: when a dead forward may be re-opened, when to stop trying,
// and how the loop is driven.
//
// What is deliberately NOT monitored: whether the service *behind* a forward is
// up. Start's readiness probe only dials 127.0.0.1:localPort, and ssh's local
// listener accepts whether or not the remote end is reachable, so "the forward
// is live" means "our ssh child is running", not "the remote app answers".

const (
	// portMapWatchInterval is the safety-net cadence. A forward that dies is
	// normally re-opened within milliseconds by the onExit wake-up, so this
	// only covers a manager created before the hook existed and a nudge lost to
	// a full wake channel.
	portMapWatchInterval = 10 * time.Second
	// portMapSettleWindow is how long a replacement child must live before the
	// forward counts as healthy again. Anything that dies sooner is a flap.
	portMapSettleWindow = 30 * time.Second
	// portMapBaseBackoff is the wait after the first failure, doubling per
	// consecutive failure up to portMapMaxBackoff.
	portMapBaseBackoff = 15 * time.Second
	portMapMaxBackoff  = 60 * time.Second
	// portMapMaxFailures bounds the attempts before a forward is left down for
	// the user to act on. At the defaults that is roughly six minutes of
	// trying, which covers a host reboot and still gives up on a host that is
	// genuinely off.
	portMapMaxFailures = 8
)

// portMapRetry is the restart bookkeeping for one remote port.
//
// A forward is only "recovered" once a child has outlived portMapSettleWindow,
// which is why a successful open does not clear failures: a forward that opens
// and dies immediately would otherwise look healthy on every attempt and be
// re-opened forever.
type portMapRetry struct {
	failures int
	nextTry  time.Time
	givenUp  bool
	// openedAt is when the policy last re-opened this forward. noteHealthy uses
	// it to tell a child that has settled from one that was only just reopened.
	// Zero when the current child was not opened by the policy.
	openedAt time.Time
}

// portMapPolicy decides when a dead forward may be re-opened. One per remote
// project, held by its portMapEntry, so the bookkeeping for a project's
// forwards stays separate and is released with the entry.
//
// start, stop and now are injected: the real Start blocks for up to five
// seconds on its readiness probe, which would make the policy's timing
// untestable.
type portMapPolicy struct {
	start func(remote.ProjectPortMap) error
	stop  func(remotePort int) error
	now   func() time.Time

	mu    sync.Mutex
	retry map[int]*portMapRetry
	// suppressed holds ports the user disabled or removed whose teardown is in
	// flight. tryStart refuses them, which closes the race where a watchdog pass
	// that read a stale "enabled" snapshot re-opens a forward between Stop and
	// the persisted removal — leaving an ssh child the store no longer tracks.
	// Kept OUTSIDE retry so forget (which drops retry bookkeeping) cannot clear
	// it: a stale pass can call tryStart after forget. reset clears it when the
	// user re-adds or re-enables the port.
	suppressed map[int]struct{}
	maxLen     int
}

func newPortMapPolicy(start func(remote.ProjectPortMap) error, stop func(remotePort int) error) *portMapPolicy {
	return &portMapPolicy{
		start:      start,
		stop:       stop,
		now:        time.Now,
		retry:      make(map[int]*portMapRetry),
		suppressed: make(map[int]struct{}),
		maxLen:     portMapMaxFailures,
	}
}

// state returns the bookkeeping for remotePort, creating it on first use.
// Callers must hold p.mu.
func (p *portMapPolicy) state(remotePort int) *portMapRetry {
	r, ok := p.retry[remotePort]
	if !ok {
		r = &portMapRetry{}
		p.retry[remotePort] = r
	}
	return r
}

// backoff is the wait after the given number of consecutive failures.
func (p *portMapPolicy) backoff(failures int) time.Duration {
	d := portMapBaseBackoff
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= portMapMaxBackoff {
			return portMapMaxBackoff
		}
	}
	return d
}

// noteExit records a child that has been reaped. uptime is how long it lived,
// which is what distinguishes a forward that was healthy from one that flapped
// on the way up.
func (p *portMapPolicy) noteExit(remotePort int, uptime time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.state(remotePort)
	r.openedAt = time.Time{}
	if uptime >= portMapSettleWindow {
		// It ran long enough to count as healthy, so this death starts over.
		r.failures = 0
		r.givenUp = false
		r.nextTry = time.Time{}
		return
	}
	r.failures++
	r.givenUp = r.failures >= p.maxLen
	r.nextTry = p.now().Add(p.backoff(r.failures))
}

// noteHealthy clears the bookkeeping for a forward that is currently live and
// has stayed up for portMapSettleWindow. A child the policy reopened moments ago
// is live but not yet proven, so it is left alone: clearing failures for it
// would let a forward that dies after 10-30s restart its count on every cycle
// and never reach the give-up limit.
func (p *portMapPolicy) noteHealthy(remotePort int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.state(remotePort)
	if !r.openedAt.IsZero() && p.now().Sub(r.openedAt) < portMapSettleWindow {
		return
	}
	r.failures = 0
	r.givenUp = false
	r.nextTry = time.Time{}
	r.openedAt = time.Time{}
}

// suppress marks remotePort as being torn down (disabled or removed). tryStart
// refuses a suppressed port until reset clears it.
func (p *portMapPolicy) suppress(remotePort int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.suppressed[remotePort] = struct{}{}
}

// isSuppressed reports whether remotePort is suppressed. Callers must hold p.mu.
func (p *portMapPolicy) isSuppressed(remotePort int) bool {
	_, ok := p.suppressed[remotePort]
	return ok
}

// reset clears a give-up AND any suppression so the forward is tried again
// immediately. Used by the user's own Enable/Add, which is the deliberate
// "try it now" action.
func (p *portMapPolicy) reset(remotePort int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.suppressed, remotePort)
	r := p.state(remotePort)
	r.failures = 0
	r.givenUp = false
	r.nextTry = time.Time{}
	r.openedAt = time.Time{}
}

// givenUp reports whether the monitor has stopped retrying this forward. The
// monitor uses it to log the give-up once, rather than on every later pass.
func (p *portMapPolicy) givenUp(remotePort int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state(remotePort).givenUp
}

// forget drops the retry bookkeeping for a forward that was disabled or
// removed. Suppression is deliberately NOT cleared: a watchdog pass holding a
// stale "enabled" snapshot may still call tryStart after this, and it must keep
// being refused until the user re-adds or re-enables the port (reset).
func (p *portMapPolicy) forget(remotePort int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.retry, remotePort)
}

// tryStart re-opens pm if the policy allows an attempt right now. It reports
// whether an attempt was made, so the caller can log the outcome without
// having to distinguish "backing off" from "gave up".
//
// The suppression check is done twice — before the open and after it. A teardown
// (Disable/Remove) can land while the open is in flight, and because start is a
// slow readiness probe it must not be called while holding p.mu. The first check
// refuses an already-suppressed port; the second catches a teardown that started
// mid-open and closes the child it raced into existence, so no forward can
// outlive its removal as an orphan.
func (p *portMapPolicy) tryStart(pm remote.ProjectPortMap) (bool, error) {
	p.mu.Lock()
	r := p.state(pm.RemotePort)
	now := p.now()
	allowed := !p.isSuppressed(pm.RemotePort) && !r.givenUp && !now.Before(r.nextTry)
	p.mu.Unlock()
	if !allowed {
		return false, nil
	}

	err := p.start(pm)

	p.mu.Lock()
	suppressed := p.isSuppressed(pm.RemotePort)
	if !suppressed {
		if err != nil {
			r.failures++
			r.givenUp = r.failures >= p.maxLen
			r.nextTry = now.Add(p.backoff(r.failures))
		} else {
			// Deliberately leaves failures and givenUp alone: the open is not yet
			// a recovery. nextTry holds off the next attempt long enough for the
			// child to either settle or be reaped (and for that reap to set the
			// real backoff), which is what keeps a flapping forward from
			// hot-looping.
			r.nextTry = now.Add(portMapSettleWindow)
			r.openedAt = now
		}
	}
	p.mu.Unlock()

	if suppressed && err == nil {
		// A teardown raced this open. After the first check passed, the port was
		// suppressed and its forward stopped; if we opened one in that window it
		// must be closed, or it would outlive the removal untracked.
		if serr := p.stop(pm.RemotePort); serr != nil {
			log.Printf("port forwards: closing remote:%d after a teardown raced its restart: %v", pm.RemotePort, serr)
		}
	}
	return true, err
}

// portMapWatchdogLoop re-opens forwards that die on their own, until stop is
// closed (Serve return). It is event-driven: ForwardManager's reaper nudges
// wake, so the common case is a restart within milliseconds of the death, and
// the ticker is only a safety net.
func (h *Handler) portMapWatchdogLoop(stop <-chan struct{}) {
	if h.portMaps == nil || h.projects == nil {
		return
	}
	ticker := time.NewTicker(portMapWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-h.portMaps.wake:
		case <-ticker.C:
		}
		h.portMapWatchdogPass()
	}
}

// portMapWatchdogPass is one monitoring pass over every project's persisted
// forwards. Exported to the package's tests so the policy can be driven
// without a real clock.
func (h *Handler) portMapWatchdogPass() {
	if h.portMaps == nil || h.projects == nil {
		return
	}
	for _, entry := range h.portMaps.snapshot() {
		h.monitorPortMaps(entry)
	}
}

// monitorPortMaps reconciles one project's forwards against what is actually
// running: a live forward clears its bookkeeping, a dead but enabled one is
// offered to the policy, and a disabled one is forgotten.
func (h *Handler) monitorPortMaps(entry *portMapEntry) {
	maps, err := h.projects.PortMaps(entry.ref)
	if err != nil {
		// Logged, not swallowed: a store read failure means the monitor is
		// blind for this project and only a user action can revive a forward.
		log.Printf("port forwards: monitor load %s:%s: %v", entry.ref.Host, entry.ref.Path, err)
		return
	}
	for _, m := range maps {
		if !m.Enabled {
			entry.policy.forget(m.RemotePort)
			continue
		}
		if entry.fm.IsLive(m.RemotePort) {
			entry.policy.noteHealthy(m.RemotePort)
			continue
		}
		pm := remote.ProjectPortMap{RemotePort: m.RemotePort, LocalPort: m.LocalPort, Enabled: true}
		attempted, err := entry.policy.tryStart(pm)
		if !attempted {
			// Backing off, or already given up — the panel shows the row as
			// not-live either way, so there is nothing to report.
			continue
		}
		if err != nil {
			log.Printf("port forwards: monitor restart remote:%d for %s:%s failed: %v",
				m.RemotePort, entry.ref.Host, entry.ref.Path, err)
			if entry.policy.givenUp(m.RemotePort) {
				log.Printf("port forwards: monitor gave up on remote:%d for %s:%s after %d consecutive failures; re-enable it to try again",
					m.RemotePort, entry.ref.Host, entry.ref.Path, portMapMaxFailures)
			}
			continue
		}
		log.Printf("port forwards: monitor restarted remote:%d for %s:%s", m.RemotePort, entry.ref.Host, entry.ref.Path)
	}
}

// wakeMonitor nudges the watchdog loop. Non-blocking: the loop's next tick
// picks up anything a dropped nudge missed, so losing one is harmless — and
// this runs on a reaper goroutine, which must never block.
func (reg *portMapRegistry) wakeMonitor() {
	select {
	case reg.wake <- struct{}{}:
	default:
	}
}

// snapshot returns the registry's entries. The returned slice is detached from
// the registry so a monitoring pass never holds reg.mu across an ssh open.
func (reg *portMapRegistry) snapshot() []*portMapEntry {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	out := make([]*portMapEntry, 0, len(reg.byKey))
	for _, e := range reg.byKey {
		out = append(out, e)
	}
	return out
}
