package server

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/u007/ocode/internal/browse"
)

// handleRemoteBrowseConfig answers /api/browse/config?host=<remote> with the
// browse origin that `ocode serve --remote` runs on that host. The workspace
// tunnel forwards the same port number to this machine (see
// remote.StartTunnel), so the base is the local end of that tunnel
// (RemoteWorkspace.BrowseURL): a page on the host's `localhost` is then
// reached from the host, not from here.
//
// Admission matches the path-less remote proxy: the host must have a saved
// project. A failed connect or a remote without a browse origin is an error,
// never a fallback to this machine's own browse origin — that would dial
// localhost on the wrong machine.
func (s *Server) handleRemoteBrowseConfig(w http.ResponseWriter, r *http.Request, host string) {
	h := s.handler
	if h.remoteHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "remote connections not available")
		return
	}
	project, ok := h.firstSavedProjectForHost(host)
	if !ok {
		writeError(w, http.StatusForbidden, "unknown host")
		return
	}
	ws, err := h.remoteHosts.workspaceForPort(host, project.Path, project.RemotePort)
	if err != nil {
		log.Printf("remote browse config: connect host %s failed: %v", host, err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("remote connect failed: %v", err))
		return
	}
	tunnelBase := ws.BrowseURL()
	if tunnelBase == "" {
		log.Printf("remote browse config: host %s reports no browse origin (browse port 0)", host)
		writeError(w, http.StatusBadGateway, "remote server has no browse origin")
		return
	}
	// The browse proxy is served on the main server's second listener, and
	// the client must reach it the same way it reaches a local project's
	// browser: a tailnet phone gets the shared HTTPS URL, a loopback client
	// the same-site loopback one (browseProxyBaseURL). Fail loudly if the
	// listener is not up — never fall back to this machine's browse origin
	// for a remote host (the global rule).
	proxyBase := s.browseProxyBaseURL(r)
	if proxyBase == "" {
		writeError(w, http.StatusServiceUnavailable, "browse proxy listener not available")
		return
	}
	baseURL := proxyBase + "/api/remote/" + host + "/browse"
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url":    baseURL,
		"remote_mode": true,
	})
}

// handleRemoteBrowseCDP streams the Chrome-mode screencast of a remote host
// (/api/browse/cdp/{key}?host=<remote>). The grant and the page live on the
// host's browse server, so the socket is that host's own /api/browse/cdp/{key},
// reached through the same saved-host proxy as every other remote call (its
// admission, connect and 503-while-connecting rules, and the websocket token
// rewrite). The host parameter is removed first: the host's handler would
// otherwise read it as a remote request of its own.
func (s *Server) handleRemoteBrowseCDP(w http.ResponseWriter, r *http.Request, host string) {
	prepareRemoteCDPRequest(r, host)
	s.handler.HandleRemoteProxy(w, r)
}

// prepareRemoteCDPRequest rewrites a main-origin CDP request into the host's
// own /api/browse/cdp/<key> request. The host parameter is removed: the host's
// handler would otherwise read it as a remote request of its own. Split out so
// the rewrite is testable without a live remote workspace.
func prepareRemoteCDPRequest(r *http.Request, host string) {
	q := r.URL.Query()
	q.Del("host")
	r.URL.RawQuery = q.Encode()
	r.SetPathValue("host", host)
	r.SetPathValue("rest", "browse/cdp/"+r.PathValue("stateKey"))
}

// handleRemoteBrowseProxy proxies browse-origin requests to the remote host's
// tunneled browse origin. This makes the embedded browser accessible from
// remote clients (mobile, web) that cannot reach the loopback tunnel directly.
func (s *Server) handleRemoteBrowseProxy(w http.ResponseWriter, r *http.Request, host string) {
	h := s.handler
	if h.remoteHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "remote connections not available")
		return
	}
	project, ok := h.firstSavedProjectForHost(host)
	if !ok {
		writeError(w, http.StatusForbidden, "unknown host")
		return
	}
	// This route sits outside authMiddleware, so it must never dial: the
	// authenticated /api/browse/config call connects the host first.
	ws, connected := h.remoteHosts.connectedWorkspace(host, project.RemotePort)
	if !connected {
		writeError(w, http.StatusConflict, "remote host not connected")
		return
	}
	tunnelBase := ws.BrowseURL()
	if tunnelBase == "" {
		writeError(w, http.StatusBadGateway, "remote server has no browse origin")
		return
	}
	target, err := url.Parse(tunnelBase)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid browse origin")
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	prefix := "/api/remote/" + host + "/browse"
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
		req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		if req.URL.RawPath != "" {
			req.URL.RawPath = strings.TrimPrefix(req.URL.RawPath, prefix)
		}
		// Tell the origin under which path prefix it is mounted. The origin
		// runs on the host and cannot know this local mount path, but it
		// emits ROOT-RELATIVE absolute paths (the grant-redemption redirect,
		// the cookie path, the rewritten HTML/CSS URLs, the capture script).
		// Without this header those paths resolve against the PROXY's root and
		// land on the wrong origin — this machine's browse origin instead of
		// the host's — which fails the panel with a blank page and a
		// permanent 401. See internal/browse/basepath.go.
		req.Header.Set(browse.BasePathHeader, prefix)
	}
	// A host running an OLD ocode does not know its mount prefix, so it
	// emits an unprefixed cookie Path ("/b/"). Rewrite it here as a
	// compatibility net. A NEW host already emits the prefixed path, and
	// this rewrite is a no-op for it (the prefix is not "Path=/b/").
	//
	// The Location header and the rewritten body paths are NOT fixed here:
	// they are produced by the origin, and only the origin knows the prefix.
	// An old host therefore keeps the broken redirect — the version banner
	// prompts the user to update the remote server.
	proxy.ModifyResponse = func(resp *http.Response) error {
		cookies := resp.Header.Values("Set-Cookie")
		resp.Header.Del("Set-Cookie")
		for _, c := range cookies {
			c = strings.Replace(c, "Path=/b/", "Path="+prefix+"/b/", 1)
			resp.Header.Add("Set-Cookie", c)
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("remote browse proxy: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("remote browse proxy: %v", err))
	}
	proxy.ServeHTTP(w, r)
}
