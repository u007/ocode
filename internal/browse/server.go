package browse

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/tool"
)

// NavEvent is the server-authoritative address-bar / status update. The SPA
// renders these and NEVER page-reported URLs (spoofing defense).
type NavEvent struct {
	StateKey string `json:"state_key"`
	URL      string `json:"url"`
	Status   int    `json:"status"`
	Mode     string `json:"mode"` // "local" | "chrome"
	Error    string `json:"error,omitempty"`
}

// Options configures the headless Chrome subsystem. ChromePath overrides
// discovery; IdleTimeout is how long the shared Chrome process idles before
// shutdown. Supervisor is the server-owned process supervisor.
type Options struct {
	ChromePath  string
	IdleTimeout time.Duration
	Supervisor  *tool.ProcessSupervisor
}

// chromeTarget is the subset of cdp.Target used by the browse WS. Defined
// as an interface so tests can inject a fake without a real Chrome.
type chromeTarget interface {
	Navigate(ctx context.Context, url string) error
	Back(ctx context.Context) error
	Forward(ctx context.Context) error
	Reload(ctx context.Context) error
	Resize(ctx context.Context, w, h int, dpr float64) error
	Mouse(ctx context.Context, ev cdp.MouseEvent) error
	Key(ctx context.Context, ev cdp.KeyEvent) error
	Detach()
}

// chromeManager is the subset of cdp.Manager used by the browse server.
type chromeManager interface {
	Attach(ctx context.Context, stateKey string, sink cdp.FrameSink) (chromeTarget, error)
	Revoke(stateKey string)
	Close(ctx context.Context) error
}

// realManagerAdapter wraps *cdp.Manager to satisfy chromeManager.
type realManagerAdapter struct{ *cdp.Manager }

func (r *realManagerAdapter) Attach(ctx context.Context, stateKey string, sink cdp.FrameSink) (chromeTarget, error) {
	t, err := r.Manager.Attach(ctx, stateKey, sink)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// wsOut is one outbound WS message queued to the single writer goroutine.
type wsOut struct {
	isBinary bool
	data     []byte
	closeCode int
	closeText string
	isClose  bool
}

// cdpSocketEntry tracks one active __cdp WebSocket per stateKey.
type cdpSocketEntry struct {
	// send is the writer channel (frames + JSON). Closing it signals the
	// writer to exit.
	send chan wsOut
	// wsConn is the underlying gorilla connection (for pings and direct closes).
	wsConn interface{ Close() error }
	closeFn func(code int, text string) error
}

func (e *cdpSocketEntry) connClose(code int, text string) error {
	if e.closeFn != nil {
		return e.closeFn(code, text)
	}
	if e.wsConn != nil {
		return e.wsConn.Close()
	}
	return nil
}

// Server is the isolated browse origin. Proxied content is served only here,
// cross-origin to the ocode SPA, so page scripts cannot reach the SPA DOM,
// token, or /api/*.
type Server struct {
	apiToken string
	auth     *authStore
	log      *log.Logger
	mux      *http.ServeMux
	publish  func(stateKey string, ev NavEvent)

	// spaOrigin is the main (SPA) origin, set via EnableBrowse wiring; the
	// server-wide default postMessage targetOrigin for the capture script.
	// Real traffic uses the per-stateKey origin recorded at grant mint
	// (see spaOriginFor) — the bound address ("127.0.0.1:4096") is not
	// necessarily the origin the user opened ("localhost:4096").
	spaOrigin string

	// localTransport caches the allowPrivate transport used ONLY by
	// handleLocal (Part 06). Guarded by Once; never used for external mode.
	localTransportOnce sync.Once
	localTransportVal  *http.Transport

	// conns enforces the per-stateKey concurrent upstream connection cap
	// (spec § External mode limits: 32). External and local traffic for one
	// stateKey share a single semaphore — they consume the same upstream
	// resource, so the cap is per-stateKey, not per-mode.
	conns *connLimiter

	cdp     chromeManager
	cdpMu   sync.Mutex
	cdpSocks map[string]*cdpSocketEntry
}

func New(apiToken string, logger *log.Logger, opts ...Options) *Server {
	if logger == nil {
		logger = log.Default()
	}
	s := &Server{apiToken: apiToken, auth: newAuthStore(), log: logger, mux: http.NewServeMux(), cdpSocks: make(map[string]*cdpSocketEntry)}
	s.conns = newConnLimiter(maxUpstreamConnsPerKey)
	s.mux.HandleFunc("/b/", s.handleBrowse)
	s.mux.HandleFunc("GET /__ocode_capture.js", s.serveCapture)
	s.mux.HandleFunc("GET /b/{stateKey}/__cdp", s.handleCDP)
	if len(opts) > 0 {
		s.initManager(opts[0])
	}
	return s
}

// initManager creates the CDP manager from Options. No-op if manager already set
// (tests inject a fake). Called from New and from Configure.
func (s *Server) initManager(opts Options) {
	if s.cdp != nil {
		return
	}
	mgrOpts := cdp.ManagerOptions{
		ChromePath:  opts.ChromePath,
		IdleTimeout: opts.IdleTimeout,
		Supervisor:  opts.Supervisor,
		Dialer:      NewSafeDialer(false),
		Log:         s.log,
		EmitNav: func(ev cdp.NavEvent) {
			s.emitNav(NavEvent{StateKey: ev.StateKey, URL: ev.URL, Status: ev.Status, Mode: "chrome", Error: ev.Error})
		},
	}
	m := cdp.NewManager(mgrOpts)
	s.cdp = &realManagerAdapter{Manager: m}
}

// Configure installs or replaces the CDP manager options after construction.
// Used by StartBrowse when opts are known after New.
func (s *Server) Configure(opts Options) { s.initManager(opts) }

// SetCDPManager installs a fake manager for tests.
func (s *Server) SetCDPManager(m chromeManager) { s.cdp = m }

// SetSPAOrigin records the main (SPA) origin so local HTML can carry the
// exact postMessage targetOrigin for capture-script telemetry.
// Called once from the main server's EnableBrowse wiring at boot.
func (s *Server) SetSPAOrigin(o string) { s.spaOrigin = o }

// spaOriginFor returns the postMessage targetOrigin for stateKey's capture
// script: the SPA origin recorded when its grant was minted, or the
// server-wide default when no grant has recorded one.
func (s *Server) spaOriginFor(stateKey string) string {
	if o, ok := s.auth.originFor(stateKey); ok {
		return o
	}
	return s.spaOrigin
}

func (s *Server) Handler() http.Handler { return s.mux }

// MintGrant issues a one-time grant for stateKey. spaOrigin is the origin
// of the SPA page requesting it (its Origin header); "" keeps the
// server-wide default from SetSPAOrigin.
func (s *Server) MintGrant(stateKey, spaOrigin string) string {
	return s.auth.mint(stateKey, spaOrigin)
}

func (s *Server) Revoke(stateKey string) {
	s.auth.revoke(stateKey)
	if s.cdp != nil {
		s.cdp.Revoke(stateKey)
	}
	// Close any active __cdp socket for this stateKey.
	s.cdpMu.Lock()
	if e, ok := s.cdpSocks[stateKey]; ok {
		_ = e.connClose(1011, "revoked")
		delete(s.cdpSocks, stateKey)
	}
	s.cdpMu.Unlock()
}

func (s *Server) Close(ctx context.Context) error {
	if s.cdp != nil {
		return s.cdp.Close(ctx)
	}
	return nil
}

func (s *Server) SetNavPublisher(fn func(stateKey string, ev NavEvent)) { s.publish = fn }

func (s *Server) emitNav(ev NavEvent) {
	if s.publish != nil {
		s.publish(ev.StateKey, ev)
	}
}

// Listen binds addr and returns the listener plus the base URL the SPA uses.
func (s *Server) Listen(addr string) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	return ln, "http://" + ln.Addr().String(), nil
}

// handleBrowse is the single entrypoint. Part 01 authenticates and returns a
// stub 200 for a valid target; Parts 03/06 replace the stub with real
// external/local proxying.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	stateKey, redirect, ok := s.auth.authenticate(w, r)
	if !ok {
		http.Error(w, "browse: unauthorized", http.StatusUnauthorized)
		return
	}
	if redirect != "" {
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}
	t, err := parseTarget(r.URL.Path, r.URL.RawQuery)
	if err != nil {
		s.log.Printf("browse: parseTarget failed for %q: %v", r.URL.Path, err)
		http.Error(w, "browse: bad target", http.StatusBadRequest)
		return
	}
	if t.StateKey != stateKey {
		// Cookie belongs to a different panel; never cross state keys.
		http.Error(w, "browse: state key mismatch", http.StatusForbidden)
		return
	}
	// Authenticate has already validated the session against this request, so
	// the cookie is present unless this is the grant-redeem request (which
	// redirects above before the gate runs). Guard nonetheless.
	cookieVal := ""
	if c, err := r.Cookie(browseCookie); err == nil {
		cookieVal = c.Value
	}
	// Server-authoritative local-mode gate (spec § Local mode). External pages
	// must never navigate (or fetch) the panel into local mode: local upstreams
	// are only reachable while this session is marked local, and a session is
	// only ever marked local by a fresh SPA grant whose target was a private
	// upstream (see auth.authenticate). The gate keys off session state, NOT
	// the Referer header — a suppressed, missing, or malicious Referer must not
	// widen the surface. It applies to ALL local-mode requests, documents and
	// subresources alike, so an external page cannot probe the local network
	// through <img>/<script>/fetch either.
	if t.Local {
		if !s.auth.sessionLocalDoc(cookieVal) {
			// Entering local mode requires a fresh grant minted by the
			// authenticated SPA (typed / back-forward / /rc address-bar
			// navigation). Without one, the session is either brand new (no
			// earlier local document) or sat on an external document — both
			// mean an external page could be driving this request.
			s.emitNav(NavEvent{
				StateKey: t.StateKey,
				URL:      upstreamOrigin(t) + t.Path,
				Status:   http.StatusForbidden,
				Mode:     "local",
				Error:    "local navigation requires user action",
			})
			http.Error(w, "browse: local navigation requires user action", http.StatusForbidden)
			return
		}
	} else if isDocumentRequest(r) {
		// Serving an external document moves the session BACK out of local
		// mode: the next local-mode request from this session is refused until
		// a fresh local grant re-enters it. Subresources do not flip the mode —
		// an external <img> on a local page is still local traffic.
		s.auth.setLocalDoc(cookieVal, false)
	}
	if t.Local {
		s.handleLocal(w, r, t)
		return
	}
	// Non-local (chrome) mode: never proxy. The SPA switches to ChromeViewport
	// and the target navigates via the CDP socket. No upstream fetch happens here.
	urlStr := t.Scheme + "://" + t.Host + t.Path
	if t.RawQuery != "" {
		urlStr += "?" + t.RawQuery
	}
	if isDocumentRequest(r) {
		s.emitNav(NavEvent{StateKey: t.StateKey, URL: urlStr, Status: 0, Mode: "chrome"})
	}
	w.WriteHeader(http.StatusNoContent)
}
