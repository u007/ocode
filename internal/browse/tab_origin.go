package browse

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/u007/ocode/internal/crashguard"
)

// maxTabListenersPerState caps the root-mounted tab listeners one stateKey may
// hold at once.
const maxTabListenersPerState = 8

// captureScriptPath is the root-relative path of the capture bundle. A tab
// origin serves it before the upstream path is read.
const captureScriptPath = "/__ocode_capture.js"

// foreignRoutePrefix is the reserved path on every tab origin that names a
// foreign upstream: /__ocode/b/{scheme}/{host}/{path...}. A request under it is
// proxied as that upstream, through the same pipeline as prefix mode.
//
// The prefix is reserved for the whole tab origin. An app path that begins with
// "/__ocode/b/" is therefore unreachable on a tab origin and resolves to the
// foreign route instead. Paths that merely start with "/__ocode" ("/__ocode",
// "/__ocode/b", "/__ocode_x/app") stay with the binding's upstream.
const foreignRoutePrefix = "/__ocode/b/"

// tabKey is the immutable binding of one tab listener: the panel, and the one
// upstream its document and subresources come from.
type tabKey struct {
	// projectHost is the remote project host a forwarded listener serves, or ""
	// for a listener of this machine. It keys the listener apart from a local
	// tab with the same upstream.
	projectHost string
	stateKey    string
	scheme      string
	host        string
}

// tabServer is one running tab listener.
type tabServer struct {
	key tabKey
	ln  net.Listener
	srv *http.Server
}

// ListenTab opens a root-mounted origin for one (stateKey, scheme, host)
// binding on addr and returns its listener and base URL. Its document path IS
// the upstream path, so a page sees its own location.pathname. The binding is
// fixed for the listener's life: a request path or query can select a resource
// on that upstream but never another upstream.
//
// wrap, if non-nil, adds a layer (TLS in production) and makes the base URL
// https. Tests pass nil. A second call for the same binding fails, so the
// caller reuses the listener, and a stateKey holds at most
// maxTabListenersPerState listeners. Revoke and Close stop them.
func (s *Server) ListenTab(stateKey, scheme, host, addr string, wrap func(net.Listener) net.Listener) (net.Listener, string, error) {
	key, err := newTabKey("", stateKey, scheme, host)
	if err != nil {
		return nil, "", err
	}
	return s.openTab(key, addr, wrap, s.tabHandler(key))
}

// newTabKey validates one binding and lowercases its host. projectHost is ""
// for a listener of this machine.
func newTabKey(projectHost, stateKey, scheme, host string) (tabKey, error) {
	host = strings.ToLower(host)
	if stateKey == "" || (scheme != "http" && scheme != "https") || host == "" || strings.Contains(host, "/") {
		return tabKey{}, fmt.Errorf("browse: tab binding malformed: stateKey=%q scheme=%q host=%q", stateKey, scheme, host)
	}
	return tabKey{projectHost: projectHost, stateKey: stateKey, scheme: scheme, host: host}, nil
}

// openTab opens addr for key and serves handler on it. It is the one place a tab
// listener is registered: ListenTab serves the local tab handler, and
// ListenForwardTab (tab_remote.go) serves the forwarding one.
func (s *Server) openTab(key tabKey, addr string, wrap func(net.Listener) net.Listener, handler http.Handler) (net.Listener, string, error) {
	stateKey, scheme, host := key.stateKey, key.scheme, key.host
	s.tabsMu.Lock()
	defer s.tabsMu.Unlock()
	if _, dup := s.tabs[key]; dup {
		return nil, "", fmt.Errorf("browse: tab listener already open for %s://%s in state %q", scheme, host, stateKey)
	}
	open := 0
	for k := range s.tabs {
		if k.stateKey == stateKey {
			open++
		}
	}
	if open >= maxTabListenersPerState {
		return nil, "", fmt.Errorf("browse: state %q already has %d tab listeners (max %d)", stateKey, open, maxTabListenersPerState)
	}
	raw, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("browse: tab listen on %s: %w", addr, err)
	}
	ln, origin := raw, "http"
	if wrap != nil {
		ln, origin = wrap(raw), "https"
	}
	ts := &tabServer{key: key, ln: ln, srv: &http.Server{Handler: handler}}
	s.tabs[key] = ts
	crashguard.Go(func() {
		if err := ts.srv.Serve(ts.ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			s.log.Printf("browse: tab listener %s://%s for state %q: serve: %v", scheme, host, stateKey, err)
		}
		// Serve also returns when the caller closes the listener itself. Forget
		// the binding then, so the triple can be listened on again and the
		// per-state cap stops counting it. A binding closed by closeTabListeners
		// is already gone, and a newer listener on the same key is left alone.
		s.tabsMu.Lock()
		if cur, ok := s.tabs[key]; ok && cur == ts {
			delete(s.tabs, key)
		}
		s.tabsMu.Unlock()
	})
	return ln, origin + "://" + ln.Addr().String(), nil
}

// tabHandler returns the handler for one binding. It is one function rather
// than a ServeMux: a mux redirects paths containing "//" or "..", and the
// upstream path must reach the page unchanged.
func (s *Server) tabHandler(key tabKey) http.Handler {
	cookieName := tabCookieName(key.stateKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveTab(w, r, key, cookieName)
	})
}

// serveTab answers one request on a tab listener. A request outside the reserved
// route targets key's binding plus the request's own path and query. A request
// under foreignRoutePrefix targets the foreign upstream it names (tabTarget), or
// this origin's own binding when the route names this listener (tabTarget). The
// request's absolute-form URI and the basePath header are ignored, and the Host
// header only recognises such a route: it never selects an upstream of its own.
func (s *Server) serveTab(w http.ResponseWriter, r *http.Request, key tabKey, cookieName string) {
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == captureScriptPath {
		s.serveCapture(w, r)
		return
	}
	// A grant is redeemed only on a binding's own origin, where it names the
	// panel's own document (refuseGrantOnForeignRoute).
	if refuseGrantOnForeignRoute(w, r, strings.HasPrefix(r.URL.Path, foreignRoutePrefix)) {
		return
	}
	t, perr := tabTarget(key, r)
	if s.refuseGrantForPanel(w, r, key.stateKey) {
		return
	}
	// The session is authenticated against the binding's locality, not the
	// target's: a grant can only enter local mode for the origin it was minted
	// for. (A grant on a foreign route is refused above.)
	stateKey, redirect, ok := s.auth.authenticate(w, r, "", cookieName, hostIsLiteralPrivate(key.host))
	if !ok {
		http.Error(w, "browse: unauthorized", http.StatusUnauthorized)
		return
	}
	if redirect != "" {
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}
	if perr != nil {
		s.log.Printf("browse: tab origin rejected %q for state %q: %v", r.URL.Path, key.stateKey, perr)
		http.Error(w, "browse: bad target", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/") {
		http.Error(w, "browse: bad target", http.StatusBadRequest)
		return
	}
	if perr == nil && selfRouteRedirect(w, r, t) {
		return
	}
	s.serveBoundTarget(w, r, t, stateKey, cookieName, "")
}

// tabTarget resolves the upstream one tab-origin request names. Outside the
// reserved route that is the binding, at the request's own path. Under it, it is
// the foreign upstream named by the route (parseForeignRoute), unless the route
// names this listener itself: then it is the binding at the route's path. The
// origin's root IS the binding, and a static link to it cannot be told from a
// foreign one at rewrite time, so the rewrite does not need to know the origin.
// The returned error is non-nil only for a foreign route that does not parse,
// and then the target is zero.
func tabTarget(key tabKey, r *http.Request) (target, error) {
	rest, foreign := strings.CutPrefix(r.URL.Path, foreignRoutePrefix)
	if !foreign {
		return bindingTarget(key, r.URL.Path, r.URL.RawQuery), nil
	}
	t, err := parseForeignRoute(rest, r.URL.RawQuery)
	if err != nil {
		return target{}, err
	}
	if t.Scheme == listenerScheme(r) && t.Host == strings.ToLower(r.Host) {
		return bindingTarget(key, t.Path, t.RawQuery), nil
	}
	t.StateKey = key.stateKey
	t.RootMounted = true
	t.Foreign = true
	t.BindScheme = key.scheme
	t.BindHost = key.host
	return t, nil
}

// refuseGrantOnForeignRoute refuses a grant on the reserved foreign route. A grant
// is redeemed only on a binding's own origin, where it names the panel's own
// document. Here it would mark the session local for a host the grant never
// named, so it is refused and left unconsumed. foreign is whether the request
// names a foreign upstream.
func refuseGrantOnForeignRoute(w http.ResponseWriter, r *http.Request, foreign bool) bool {
	if !foreign || !r.URL.Query().Has("__grant") {
		return false
	}
	http.Error(w, "browse: a grant is not accepted on a foreign route", http.StatusBadRequest)
	return true
}

// refuseGrantForPanel refuses a grant minted for another panel before it can set
// a cookie on this origin. The peek leaves the grant unconsumed.
func (s *Server) refuseGrantForPanel(w http.ResponseWriter, r *http.Request, stateKey string) bool {
	g := r.URL.Query().Get("__grant")
	if g == "" {
		return false
	}
	if sk, live := s.auth.grantStateKey(g); live && sk != stateKey {
		http.Error(w, "browse: state key mismatch", http.StatusForbidden)
		return true
	}
	return false
}

// selfRouteRedirect moves a document that names the binding through the reserved
// route to the binding's own path, so location.pathname stays the upstream path
// that root mode promises. 307 keeps the method and body of a POST. Subresources
// are served in place (tabTarget), which needs no redirect.
func selfRouteRedirect(w http.ResponseWriter, r *http.Request, t target) bool {
	if t.Foreign || !strings.HasPrefix(r.URL.Path, foreignRoutePrefix) || !isDocumentRequest(r) {
		return false
	}
	loc := url.URL{Path: t.Path, RawQuery: t.RawQuery}
	http.Redirect(w, r, loc.String(), http.StatusTemporaryRedirect)
	return true
}

// rootPathFor is the path a root-mounted origin serves the upstream scheme://host
// plus path at, when its root serves the binding bScheme://bHost: the path itself
// on the binding, the reserved foreign route for any other upstream. path must
// already be escaped.
func rootPathFor(bScheme, bHost, scheme, host, path string) string {
	if scheme != bScheme || !strings.EqualFold(host, bHost) {
		return foreignRoutePrefix + scheme + "/" + strings.ToLower(host) + path
	}
	return path
}

// bindingTarget is the upstream a tab listener serves at its root: key's binding
// at path, with the query as sent.
func bindingTarget(key tabKey, path, rawQuery string) target {
	return target{
		StateKey:    key.stateKey,
		Scheme:      key.scheme,
		Host:        key.host,
		Path:        path,
		RawQuery:    rawQuery,
		Local:       hostIsLiteralPrivate(key.host),
		RootMounted: true,
	}
}

// listenerScheme is the scheme the listener that accepted r serves, which its
// connection's TLS state shows.
func listenerScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// parseForeignRoute parses the part of a foreign route after foreignRoutePrefix,
// which is {scheme}/{host}[/{path...}]. Only http and https are accepted, and the
// host must be exactly host[:port]: no userinfo, no empty host and no empty port.
// The host is lowercased, as mapURL emits it.
func parseForeignRoute(rest, rawQuery string) (target, error) {
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return target{}, fmt.Errorf("browse: foreign route too short: %q", rest)
	}
	scheme, host := parts[0], strings.ToLower(parts[1])
	if scheme != "http" && scheme != "https" {
		return target{}, fmt.Errorf("browse: foreign route scheme %q is not http or https", scheme)
	}
	u, err := url.Parse("//" + host)
	if err != nil || u.User != nil || u.Host != host || u.Hostname() == "" || strings.HasSuffix(host, ":") {
		return target{}, fmt.Errorf("browse: foreign route host %q is malformed", parts[1])
	}
	path := "/"
	if len(parts) == 3 {
		path = "/" + parts[2]
	}
	return target{
		Scheme:   scheme,
		Host:     host,
		Path:     path,
		RawQuery: rawQuery,
		Local:    hostIsLiteralPrivate(host),
	}, nil
}

// foreignNavMode is the NavEvent mode for a document that leaves a root-mounted
// origin for a foreign upstream: "local" for a private host, "chrome" otherwise.
func foreignNavMode(local bool) string {
	if local {
		return "local"
	}
	return "chrome"
}

// closeTabListeners stops every tab listener whose binding match accepts. The
// bindings are removed under tabsMu, and the sockets are closed after it is
// released. Each listener closes first, so its port refuses new connections
// before the caller returns. The server then ends the connections still open.
func (s *Server) closeTabListeners(match func(tabKey) bool) {
	var doomed []*tabServer
	s.tabsMu.Lock()
	for key, ts := range s.tabs {
		if match(key) {
			delete(s.tabs, key)
			doomed = append(doomed, ts)
		}
	}
	s.tabsMu.Unlock()
	for _, ts := range doomed {
		if err := ts.ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.log.Printf("browse: close tab listener %s://%s for state %q: %v", ts.key.scheme, ts.key.host, ts.key.stateKey, err)
		}
		if err := ts.srv.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.log.Printf("browse: stop tab server %s://%s for state %q: %v", ts.key.scheme, ts.key.host, ts.key.stateKey, err)
		}
	}
}

// tabCookieName is the session cookie for one panel's tab origin. Cookies are
// scoped by host, not by port, so every tab origin on 127.0.0.1 shares one
// cookie jar. A name per stateKey keeps one tab's session from overwriting
// another's. stateKeys contain ':', which a cookie name may not, so the name
// carries a hash of the stateKey.
// TabCookieName is the session cookie of one panel's tab origin, for a caller that
// reads the cookie back (a test of the forwarding path).
func TabCookieName(stateKey string) string {
	return tabCookieName(stateKey)
}

func tabCookieName(stateKey string) string {
	sum := sha256.Sum256([]byte(stateKey))
	return browseCookie + "_" + hex.EncodeToString(sum[:])[:16]
}
