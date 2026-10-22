package server

import (
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

var errHTROpening = errors.New("htrcli reverse forward is already being opened")

// htrHost is the htrcli reverse-forward state of one SSH host, shared by every
// project on it.
type htrHost struct {
	fm      *remote.ForwardManager
	opening atomic.Bool

	mu          sync.Mutex
	lastErr     string
	lastAttempt time.Time
}

func (hh *htrHost) record(err error) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	hh.lastAttempt = time.Now()
	hh.lastErr = ""
	if err != nil {
		hh.lastErr = err.Error()
	}
}

func (hh *htrHost) snapshot() (lastErr string, lastAttempt time.Time) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	return hh.lastErr, hh.lastAttempt
}

func (reg *portMapRegistry) htrHost(target remote.Target) *htrHost {
	key := target.String()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if hh, ok := reg.htr[key]; ok {
		return hh
	}
	hh := &htrHost{fm: remote.NewForwardManager(reg.sup, target)}
	reg.htr[key] = hh
	return hh
}

// openHTRReverse opens the host's reverse forward. A concurrent open returns
// errHTROpening instead of queueing a second ssh child.
func (h *Handler) openHTRReverse(ref projects.ProjectRef, target remote.Target) error {
	hh := h.portMaps.htrHost(target)
	if !hh.opening.CompareAndSwap(false, true) {
		return errHTROpening
	}
	defer hh.opening.Store(false)
	maps, err := h.projects.PortMaps(ref)
	if err != nil {
		err = fmt.Errorf("load port forwards for %s:%s: %w", ref.Host, ref.Path, err)
		hh.record(err)
		return err
	}
	err = htrReverseForwardOpener()(hh.fm, maps)
	hh.record(err)
	return err
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
	if port, ok := localHTRPort(cfg.Browser); ok && hh.fm.IsLive(port) {
		return
	}
	if _, last := hh.snapshot(); !last.IsZero() && time.Since(last) < htrAutoRetryInterval {
		return
	}
	if err := h.openHTRReverse(ref, target); err != nil && !errors.Is(err, errHTROpening) {
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
}

func (h *Handler) htrReverseStatus(target remote.Target) (htrStatusView, error) {
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		return htrStatusView{}, fmt.Errorf("load config: %w", err)
	}
	port, ok := localHTRPort(cfg.Browser)
	hh := h.portMaps.htrHost(target)
	lastErr, _ := hh.snapshot()
	return htrStatusView{
		LocalRunning: ok,
		Port:         port,
		Live:         ok && hh.fm.IsLive(port),
		Opening:      hh.opening.Load(),
		AutoStart:    cfg.Browser.HTRRemoteAutoStart,
		LastError:    lastErr,
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
	if err := h.openHTRReverse(portMapRef(rw), rw.Target); err != nil {
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
