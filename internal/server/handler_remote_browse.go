package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
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
	// The browse proxy is served on the main server's second listener. Build
	// the base URL from the request host so a remote client reaches it.
	// Fail loudly if the listener is not up — never fall back to this
	// machine's browse origin for a remote host (the global rule).
	if s.browseProxyPort == 0 {
		writeError(w, http.StatusServiceUnavailable, "browse proxy listener not available")
		return
	}
	reqHost := r.Host
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		reqHost = h
	}
	baseURL := "http://" + reqHost + ":" + strconv.Itoa(s.browseProxyPort) + "/api/remote/" + host + "/browse"
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url":    baseURL,
		"remote_mode": true,
	})
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
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		cookies := resp.Header.Values("Set-Cookie")
		resp.Header.Del("Set-Cookie")
		for _, c := range cookies {
			c = strings.Replace(c, "Path=/b/", "Path=/api/remote/"+host+"/browse/b/", 1)
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
