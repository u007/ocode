package browse

import (
	"log"
	"net"
	"net/http"
)

// NavEvent is the server-authoritative address-bar / status update. The SPA
// renders these and NEVER page-reported URLs (spoofing defense).
type NavEvent struct {
	StateKey string `json:"state_key"`
	URL      string `json:"url"`
	Status   int    `json:"status"`
	Mode     string `json:"mode"` // "local" | "proxied"
	Error    string `json:"error,omitempty"`
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

	// transport is the guarded upstream transport. External mode constructs
	// it with allowPrivate=false (Part 02 dialer re-validates every connect,
	// including redirect hops). Tests override it with allowPrivate=true to
	// reach httptest loopback upstreams.
	transport *http.Transport
	// jar holds upstream cookies server-side, keyed by (stateKey, origin);
	// site cookies never reach the browser (see cookiejar.go).
	jar *cookieJar
	// spaOrigin is the main (SPA) origin, set via EnableBrowse wiring; used
	// as the exact postMessage targetOrigin by the capture script (Part 05).
	spaOrigin string
}

func New(apiToken string, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	s := &Server{apiToken: apiToken, auth: newAuthStore(), log: logger, mux: http.NewServeMux()}
	s.transport = newSafeTransport(false) // external mode: private IPs blocked
	s.jar = newCookieJar()
	s.mux.HandleFunc("/b/", s.handleBrowse)
	s.mux.HandleFunc("GET /__ocode_capture.js", s.serveCapture)
	return s
}

// SetSPAOrigin records the main (SPA) origin so proxied HTML can carry the
// exact postMessage targetOrigin for capture-script telemetry (Part 05).
// Called once from the main server's EnableBrowse wiring at boot.
func (s *Server) SetSPAOrigin(o string) { s.spaOrigin = o }

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) MintGrant(stateKey string) string { return s.auth.mint(stateKey) }

func (s *Server) Revoke(stateKey string) { s.auth.revoke(stateKey) }

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
	if t.Local {
		s.handleLocal(w, r, t) // provided by Part 06 (shimmed to external for now)
		return
	}
	s.handleExternal(w, r, t)
}
