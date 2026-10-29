package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// localHTRPort reports the port of the local htrcli daemon when HTR is enabled
// and that daemon is answering. ok is false when HTR is off or nothing is
// listening; that is the normal "nothing to expose" case, not an error.
func localHTRPort(browser config.BrowserConfig) (port int, ok bool) {
	if !browser.HTREnabled {
		return 0, false
	}
	shared := cdp.ResolveSharedDaemon(cdp.HTRSharedInput{
		Token:        browser.HTRToken,
		Shared:       browser.HTRShared,
		LegacyPort:   browser.HTRPort,
		LegacySocket: browser.HTRSocketPath,
	})
	socket := browser.HTRSocketPath
	port = browser.HTRPort
	token := browser.HTRToken
	if shared.Mode == "shared" {
		socket = shared.Socket
		port = shared.Port
		// The resolved token may come from htrcli's own config rather than
		// browser.htr_token; probing with the empty config value makes a healthy
		// shared daemon read as down.
		token = shared.Token
	}
	info := cdp.HTRDaemonStatus(port, socket, token)
	return info.Port, info.Running
}

// htrReverseForwardFn is the shape of the reverse-forward opener.
type htrReverseForwardFn func(fm *remote.ForwardManager, persisted []projects.PortMap) error

// startHTRReverseForward is the seam the tests replace, so a test can observe
// whether the reverse forward was even ASKED for — which the real
// implementation cannot show when the local htrcli daemon is not running (it
// is then a no-op for reasons unrelated to the caller).
//
// It is an atomic pointer, not a plain var: the opener runs on a BACKGROUND
// goroutine (the auto-start), and a test that installs and restores the seam
// would otherwise race with that goroutine's read.
var startHTRReverseForward atomic.Pointer[htrReverseForwardFn]

func init() {
	fn := htrReverseForwardFn(StartHTRReverseForward)
	startHTRReverseForward.Store(&fn)
}

// htrReverseForwardOpener returns the currently installed opener.
func htrReverseForwardOpener() htrReverseForwardFn {
	if fn := startHTRReverseForward.Load(); fn != nil {
		return *fn
	}
	return StartHTRReverseForward
}

// StartHTRReverseForward opens `ssh -R <port>:127.0.0.1:<port>` so htrcli on an
// SSH remote reaches the local machine's htrcli daemon (and therefore the
// local browser). It is a no-op when HTR is disabled, the daemon is not
// running, or the user already has a persisted forward on that remote port
// (their entry wins). The forward is ephemeral: it is re-evaluated on every
// remote autostart and is not written to projects.json.
func StartHTRReverseForward(fm *remote.ForwardManager, persisted []projects.PortMap) error {
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		return fmt.Errorf("load config for htr reverse forward: %w", err)
	}
	port, ok := localHTRPort(cfg.Browser)
	if !ok {
		return nil
	}
	for _, pm := range persisted {
		if pm.RemotePort == port {
			return nil
		}
	}
	if err := fm.Start(remote.ProjectPortMap{RemotePort: port, LocalPort: port, Enabled: true, Reverse: true}); err != nil {
		return fmt.Errorf("htr reverse forward on remote port %d: %w", port, err)
	}
	log.Printf("htr: remote %d -> local %d reverse forward open", port, port)
	return nil
}

// htrAutoRetryInterval is the minimum gap between automatic reverse-forward
// attempts for one host. Each failed attempt costs a ~5s readiness probe and
// an ssh child, and the trigger (a Port forwards list) can fire every few
// seconds.
const htrAutoRetryInterval = 15 * time.Second

// htrAutoRetryMax caps the backoff so a persistently-busy remote port is
// retried at most every htrAutoRetryMax rather than hammered every 15s.
const htrAutoRetryMax = 15 * time.Minute

// htrAutoMaxFails caps the failure count used to compute the backoff. The
// ladder doubles from htrAutoRetryInterval (15s, 30s, 1m, 2m, 4m, 8m), so this
// value is the first that reaches htrAutoRetryMax; larger counts are clamped by
// htrBackoff and never occur.
const htrAutoMaxFails = 7

// htrBackoff returns the wait before attempt number `failures` (1 = first
// failure). It mirrors portMapPolicy.backoff so a persistently-busy remote
// port is backed off exponentially instead of hammered every 15s.
func htrBackoff(failures int) time.Duration {
	d := htrAutoRetryInterval
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= htrAutoRetryMax {
			return htrAutoRetryMax
		}
	}
	return d
}

var errHTROpening = errors.New("htrcli reverse forward is already being opened")

// htrHost is the htrcli reverse-forward state of one SSH host, shared by every
// project on it.
type htrHost struct {
	fm      *remote.ForwardManager
	opening atomic.Bool

	// now is injectable so the backoff timing is testable.
	now func() time.Time

	mu          sync.Mutex
	lastErr     string
	lastAttempt time.Time
	// failures counts consecutive failed opens; nextTry is the earliest time a
	// new attempt is allowed; lastPort is the remote port last attempted, so a
	// config change (htr_port) resets the backoff and retries the new port.
	failures int
	nextTry  time.Time
	lastPort int

	// holder is the remote process squatting the port after the last busy-port
	// failure; nil when unknown, not busy, or the last open succeeded. It is
	// probed once at failure time, not per status poll, which would cost an ssh
	// exec every few seconds.
	holder *remote.PortHolder
}

func (hh *htrHost) record(port int, err error) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	hh.lastAttempt = hh.now()
	hh.lastErr = ""
	if err != nil {
		hh.lastErr = err.Error()
		if port != hh.lastPort {
			// A new remote port (e.g. the user re-configured htr_port) starts
			// with a clean failure count.
			hh.lastPort = port
			hh.failures = 0
		}
		if hh.failures < htrAutoMaxFails {
			hh.failures++
		}
	} else {
		hh.failures = 0
		hh.lastPort = port
	}
	// Every attempt — success or failure — schedules the next one. A success
	// on a port that is still not live (htr not running) gets the base 15s
	// cooldown, matching the original fixed-throttle behaviour; a failure
	// backs off exponentially.
	hh.nextTry = hh.lastAttempt.Add(htrBackoff(hh.failures))
}

// shouldRetry reports whether a new reverse-forward attempt may be made now for
// port. It enforces the backoff after failures and resets the backoff when the
// configured remote port changes (a new port gets a fresh attempt).
func (hh *htrHost) shouldRetry(port int) bool {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	if port != hh.lastPort {
		// Not the port this host is tracking backoff for (a fresh port, or a
		// different port queried before a real attempt). Returning true here is
		// harmless: autoStartHTRReverse only ever gates the configured port, and
		// record() owns all port-state mutations on the actual attempt path.
		return true
	}
	if !hh.nextTry.IsZero() && hh.now().Before(hh.nextTry) {
		return false
	}
	return true
}

func (hh *htrHost) snapshot() (lastErr string, lastAttempt time.Time) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	return hh.lastErr, hh.lastAttempt
}

func (hh *htrHost) setHolder(p *remote.PortHolder) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	hh.holder = p
}

func (hh *htrHost) getHolder() *remote.PortHolder {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	return hh.holder
}

// dropHolderIfLive clears the recorded squatter once the htr forward is live.
// A live forward means the port is ours, so a holder left by an earlier
// busy-port failure is gone; keeping it would offer to kill a process that is
// not there. Both liveness readers (status and the auto-start skip) call this.
func (hh *htrHost) dropHolderIfLive(live bool) {
	if live {
		hh.setHolder(nil)
	}
}

// Seams for the remote holder probe/kill, replaced by tests.
var (
	findRemotePortHolder = remote.FindPortHolder
	killRemotePortHolder = remote.KillPortHolder
)

// htrHolderProbeTimeout bounds the holder probe. The probe runs inside the open
// request after a busy-port failure, so a hung ssh must not hold that request.
var htrHolderProbeTimeout = 5 * time.Second

func (reg *portMapRegistry) htrHost(target remote.Target) *htrHost {
	key := target.String()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if hh, ok := reg.htr[key]; ok {
		return hh
	}
	hh := &htrHost{fm: remote.NewForwardManager(reg.sup, target), now: time.Now}
	reg.htr[key] = hh
	return hh
}

// openHTRReverse opens the host's reverse forward. A concurrent open returns
// errHTROpening instead of queueing a second ssh child.
func (h *Handler) openHTRReverse(ref projects.ProjectRef, target remote.Target, port int) error {
	hh := h.portMaps.htrHost(target)
	if !hh.opening.CompareAndSwap(false, true) {
		return errHTROpening
	}
	defer hh.opening.Store(false)
	maps, err := h.projects.PortMaps(ref)
	if err != nil {
		err = fmt.Errorf("load port forwards for %s:%s: %w", ref.Host, ref.Path, err)
		hh.record(port, err)
		return err
	}
	err = htrReverseForwardOpener()(hh.fm, maps)
	hh.record(port, err)
	hh.setHolder(nil)
	var busy *remote.PortBusyError
	if errors.As(err, &busy) {
		h.recordHTRHolder(hh, target, port)
	}
	return err
}

// recordHTRHolder looks up who holds the busy remote port so the panel can offer
// to kill it. A failed or timed-out probe is logged and leaves the holder unknown:
// the original forward error is still what the user sees.
func (h *Handler) recordHTRHolder(hh *htrHost, target remote.Target, port int) {
	ctx, cancel := context.WithTimeout(context.Background(), htrHolderProbeTimeout)
	defer cancel()
	holder, err := findRemotePortHolder(ctx, remote.NewSSHTransport(target, h.portMaps.sup), port)
	if err != nil {
		log.Printf("htr: probe holder of remote port %d on %s: %v", port, target, err)
		return
	}
	hh.setHolder(holder)
}

// autoStartHTRReverse is the list-triggered open: it honours the
// browser.htr_remote_autostart setting, skips a live forward, and is throttled
// per host.
func (h *Handler) autoStartHTRReverse(ref projects.ProjectRef, target remote.Target) {
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		log.Printf("port forwards: load config for htr auto-start of %s: %v", ref.Host, err)
		return
	}
	if !cfg.Browser.HTRRemoteAutoStart {
		return
	}
	hh := h.portMaps.htrHost(target)
	port, ok := localHTRPort(cfg.Browser)
	// Only skip when htr is running AND the forward is already live. When htr
	// is not running (!ok) we still attempt: StartHTRReverseForward is a no-op
	// then, but the attempt is the observable that marks this host as tried.
	live := ok && hh.fm.IsLive(port)
	hh.dropHolderIfLive(live)
	if live {
		return
	}
	if !hh.shouldRetry(port) {
		return
	}
	if err := h.openHTRReverse(ref, target, port); err != nil && !errors.Is(err, errHTROpening) {
		log.Printf("port forwards: %s:%s: %v", ref.Host, ref.Path, err)
	}
}

// htrStatusView is the wire shape of GET/POST /api/portmaps/htr.
type htrStatusView struct {
	LocalRunning bool   `json:"local_running"`
	Port         int    `json:"port"`
	Live         bool   `json:"live"`
	Opening      bool   `json:"opening"`
	AutoStart    bool   `json:"auto_start"`
	LastError    string `json:"last_error"`
	// Holder is the remote process squatting Port after a busy-port failure.
	Holder *remote.PortHolder `json:"holder,omitempty"`
}

func (h *Handler) htrReverseStatus(target remote.Target) (htrStatusView, error) {
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		return htrStatusView{}, fmt.Errorf("load config: %w", err)
	}
	port, ok := localHTRPort(cfg.Browser)
	hh := h.portMaps.htrHost(target)
	lastErr, _ := hh.snapshot()
	live := ok && hh.fm.IsLive(port)
	hh.dropHolderIfLive(live)
	return htrStatusView{
		LocalRunning: ok,
		Port:         port,
		Live:         live,
		Opening:      hh.opening.Load(),
		AutoStart:    cfg.Browser.HTRRemoteAutoStart,
		LastError:    lastErr,
		Holder:       hh.getHolder(),
	}, nil
}

// HandleHTRStatus serves GET /api/portmaps/htr?host=&project=.
func (h *Handler) HandleHTRStatus(w http.ResponseWriter, r *http.Request) {
	rw, _, ok := h.portMapTarget(w, r)
	if !ok {
		return
	}
	st, err := h.htrReverseStatus(rw.Target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// HandleHTRStart serves POST /api/portmaps/htr/start?host=&project=: an
// explicit "map it now" that ignores the auto-start setting and the retry
// throttle, and reports a failed open to the caller.
func (h *Handler) HandleHTRStart(w http.ResponseWriter, r *http.Request) {
	rw, _, ok := h.portMapTarget(w, r)
	if !ok {
		return
	}
	st, err := h.htrReverseStatus(rw.Target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !st.LocalRunning {
		writeError(w, http.StatusConflict, "local htrcli daemon is not running, so there is nothing to map")
		return
	}
	if err := h.openHTRReverse(portMapRef(rw), rw.Target, st.Port); err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, errHTROpening) {
			code = http.StatusConflict
		}
		writeError(w, code, err.Error())
		return
	}
	st, err = h.htrReverseStatus(rw.Target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// HandleHTRAutoStart serves POST /api/portmaps/htr/autostart?host=&project=
// with body {"enabled":bool}. The setting is global (browser.htr_remote_autostart).
func (h *Handler) HandleHTRAutoStart(w http.ResponseWriter, r *http.Request) {
	rw, _, ok := h.portMapTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := config.SaveOcodeHTRRemoteAutoStart(body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	st, err := h.htrReverseStatus(rw.Target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// HandleHTRReclaim serves POST /api/portmaps/htr/reclaim?host=&project= with body
// {"pid":int}: the user's confirmed "kill the squatter and retry". The pid must
// be the holder the status last reported, and the remote re-verifies it still
// holds the port before killing.
func (h *Handler) HandleHTRReclaim(w http.ResponseWriter, r *http.Request) {
	rw, _, ok := h.portMapTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		PID int `json:"pid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	hh := h.portMaps.htrHost(rw.Target)
	holder := hh.getHolder()
	if holder == nil || holder.PID != body.PID {
		writeError(w, http.StatusConflict, "the process holding the remote port changed; refresh and try again")
		return
	}
	st, err := h.htrReverseStatus(rw.Target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !st.LocalRunning {
		writeError(w, http.StatusConflict, "local htrcli daemon is not running, so there is nothing to map")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), portReclaimTimeout)
	defer cancel()
	if err := killRemotePortHolder(ctx, remote.NewSSHTransport(rw.Target, h.portMaps.sup), st.Port, body.PID); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	hh.setHolder(nil)
	if err := h.openHTRReverse(portMapRef(rw), rw.Target, st.Port); err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, errHTROpening) {
			code = http.StatusConflict
		}
		writeError(w, code, err.Error())
		return
	}
	st, err = h.htrReverseStatus(rw.Target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}
