package browse

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// TunnelFunc resolves the tunnel end of a remote host's browse origin for one
// forwarded request: the base URL the request is sent to. It never dials. When it
// cannot resolve, it has already written the response and returns false.
type TunnelFunc func(w http.ResponseWriter, r *http.Request) (string, bool)

// errRootModeUnsupported is the failure of a reply that lacks RootModeMarker: an
// older ocode on the host, or some other server behind the tunnel. Its content
// is prefix-shaped or unknown, so it is never relayed.
var errRootModeUnsupported = errors.New("remote browse origin did not answer in root-mounted mode")

// ListenForwardTab opens a root-mounted origin for a remote project's binding. It
// is ListenTab for the same binding, except that every request is answered by the
// host's own browse origin, reached through tunnel. The request's upstream is
// served there at /b/{stateKey}/{scheme}/{host}{path}, with the binding sent in
// RootModeHeader, so the page sees the same root-mounted output a local tab sees.
// projectHost is the saved project host, and keys the listener and its binding
// apart from a local tab with the same upstream. The grant and session live on
// the host, so the listener authenticates nothing itself.
func (s *Server) ListenForwardTab(projectHost, stateKey, scheme, host, addr string, wrap func(net.Listener) net.Listener, tunnel TunnelFunc) (net.Listener, string, error) {
	if projectHost == "" {
		return nil, "", errors.New("browse: forwarded tab binding needs a project host")
	}
	if tunnel == nil {
		return nil, "", errors.New("browse: forwarded tab binding needs a tunnel")
	}
	key, err := newTabKey(projectHost, stateKey, scheme, host)
	if err != nil {
		return nil, "", err
	}
	return s.openTab(key, addr, wrap, s.forwardTabHandler(key, tunnel))
}

// forwardTabHandler answers every request on a forwarded tab listener. It resolves
// the request to its upstream exactly as a local tab does (tabTarget), serves the
// capture bundle itself, and forwards the rest. Nothing reaches the host without
// the same reserved-route checks a local tab applies first.
func (s *Server) forwardTabHandler(key tabKey, tunnel TunnelFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == captureScriptPath {
			s.serveCapture(w, r)
			return
		}
		if refuseGrantOnForeignRoute(w, r, strings.HasPrefix(r.URL.Path, foreignRoutePrefix)) {
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/") {
			http.Error(w, "browse: bad target", http.StatusBadRequest)
			return
		}
		t, err := tabTarget(key, r)
		if err != nil {
			s.log.Printf("browse: forwarded tab origin rejected %q for state %q: %v", r.URL.Path, key.stateKey, err)
			http.Error(w, "browse: bad target", http.StatusBadRequest)
			return
		}
		if selfRouteRedirect(w, r, t) {
			return
		}
		base, ok := tunnel(w, r)
		if !ok {
			return
		}
		s.forwardRemoteTab(w, r, key, base, t)
	})
}

// forwardRemoteTab sends one request on a forwarded tab to the host's browse origin
// at base, which serves the upstream t names at /b/{stateKey}/{scheme}/{host}{path}.
// The binding travels in RootModeHeader. The browser's cookies are reduced to this
// tab's own session cookie: the rest belong to the SPA's origin and never reach the
// host. Every reply must carry RootModeMarker before anything is relayed, so a host
// that ignored the request contributes no content.
func (s *Server) forwardRemoteTab(w http.ResponseWriter, r *http.Request, key tabKey, base string, t target) {
	tunnel, err := url.Parse(base)
	if err != nil {
		s.log.Printf("browse: forwarded tab %s://%s for state %q: tunnel base %q: %v", key.scheme, key.host, key.stateKey, base, err)
		http.Error(w, "browse: remote tunnel base is not a URL", http.StatusBadGateway)
		return
	}
	cookieName := tabCookieName(key.stateKey)
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Director: func(req *http.Request) {
			req.URL.Scheme = tunnel.Scheme
			req.URL.Host = tunnel.Host
			req.Host = tunnel.Host
			req.URL.Path = "/b/" + key.stateKey + "/" + t.Scheme + "/" + t.Host + t.Path
			req.URL.RawPath = ""
			req.URL.RawQuery = t.RawQuery
			req.Header.Set(RootModeHeader, key.scheme+"://"+key.host)
			// The host's own prefix must never be requested by a browser: the
			// root-mode answer ignores it, and forwarding it would be a way to ask
			// for a prefix-shaped reply.
			req.Header.Del(BasePathHeader)
			keepOnlyCookie(req, cookieName)
		},
		ModifyResponse: func(resp *http.Response) error {
			if resp.Header.Get(RootModeHeader) != RootModeMarker {
				return errRootModeUnsupported
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			s.log.Printf("browse: forwarded tab %s://%s%s for state %q: %v", key.scheme, key.host, t.Path, key.stateKey, err)
			if errors.Is(err, errRootModeUnsupported) {
				http.Error(w, "browse: remote host too old for root-mounted browse; restart the host", http.StatusBadGateway)
				return
			}
			http.Error(w, "browse: remote browse origin unreachable", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

// keepOnlyCookie leaves exactly the cookie named name on req, and drops the rest.
func keepOnlyCookie(req *http.Request, name string) {
	cookies := req.Cookies()
	req.Header.Del("Cookie")
	for _, c := range cookies {
		if c.Name == name {
			req.AddCookie(c)
		}
	}
}

// isRootModeRequest reports whether r asks for root-mounted output (RootModeHeader
// present). The presence decides the mode, never the path.
func isRootModeRequest(r *http.Request) bool {
	_, ok := r.Header[http.CanonicalHeaderKey(RootModeHeader)]
	return ok
}

// handleRootModeBrowse serves one request a forwarding tab origin sent here
// (forwardRemoteTab). The binding comes from RootModeHeader and the upstream from
// the /b/ path. The output is exactly what ListenTab serves at its root: the
// rewrite, the capture injection, the redirects and the per-tab cookie. Every
// reply carries RootModeMarker, which the sender checks first.
func (s *Server) handleRootModeBrowse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(RootModeHeader, RootModeMarker)
	bScheme, bHost, err := parseRootBinding(r.Header.Get(RootModeHeader))
	if err != nil {
		s.log.Printf("browse: root-mode request for %q: %v", r.URL.Path, err)
		http.Error(w, "browse: bad root-mode binding", http.StatusBadRequest)
		return
	}
	parsed, perr := parseTarget(r.URL.Path, r.URL.RawQuery)
	if perr != nil {
		s.log.Printf("browse: root-mode request rejected %q: %v", r.URL.Path, perr)
		http.Error(w, "browse: bad target", http.StatusBadRequest)
		return
	}
	t := rootTarget(parsed, bScheme, bHost)
	if refuseGrantOnForeignRoute(w, r, t.Foreign) {
		return
	}
	if s.refuseGrantForPanel(w, r, t.StateKey) {
		return
	}
	cookieName := tabCookieName(t.StateKey)
	stateKey, redirect, ok := s.auth.authenticate(w, r, "", cookieName, t.Local)
	if !ok {
		http.Error(w, "browse: unauthorized", http.StatusUnauthorized)
		return
	}
	if redirect != "" {
		http.Redirect(w, r, rootRedirect(r, t), http.StatusFound)
		return
	}
	s.serveBoundTarget(w, r, t, stateKey, cookieName, "")
}

// rootTarget marks t as served by a root-mounted origin whose root is the binding
// bScheme://bHost. A target on any other upstream is foreign.
func rootTarget(t target, bScheme, bHost string) target {
	t.RootMounted = true
	if t.Scheme != bScheme || !strings.EqualFold(t.Host, bHost) {
		t.Foreign, t.BindScheme, t.BindHost = true, bScheme, bHost
	}
	return t
}

// rootRedirect is the location a root-mounted origin redirects a redeemed grant
// to: the target's own path, on the origin's root, with the grant removed.
func rootRedirect(r *http.Request, t target) string {
	bScheme, bHost := t.binding()
	loc := rootPathFor(bScheme, bHost, t.Scheme, t.Host, (&url.URL{Path: t.Path}).EscapedPath())
	q := r.URL.Query()
	q.Del("__grant")
	if enc := q.Encode(); enc != "" {
		loc += "?" + enc
	}
	return loc
}

// parseRootBinding reads a RootModeHeader value, "scheme://host" with an optional
// port. It names exactly one origin: no path, query, fragment or userinfo.
func parseRootBinding(v string) (string, string, error) {
	u, err := url.Parse(v)
	if err != nil {
		return "", "", fmt.Errorf("browse: root-mode binding %q: %w", v, err)
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.Host == "" {
		return "", "", fmt.Errorf("browse: root-mode binding %q is not a bare origin", v)
	}
	bound, err := parseForeignRoute(u.Scheme+"/"+u.Host, "")
	if err != nil {
		return "", "", fmt.Errorf("browse: root-mode binding %q: %w", v, err)
	}
	return bound.Scheme, bound.Host, nil
}
