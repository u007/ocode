package desktop

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/u007/ocode/internal/remote"
)

// RemoteProxy is a reverse proxy that forwards ALL /api/* requests
// to a remote ocode server through an SSH tunnel. It injects the
// remote auth token from ServeState and strips local tokens.
//
// All /api/* endpoints (chat, sessions, files, git, browse config,
// browse grant, etc.) are served by the remote API server. There
// is no separate browse-origin proxy in V1: browse endpoints live
// on the main API server (internal/server EnableBrowse).
//
// Created in internal/desktop (not internal/server) because it needs
// to import internal/remote for workspace state, and internal/server
// cannot import internal/remote (cycle per serve.go:17-19).
type RemoteProxy struct {
	apiProxy   *httputil.ReverseProxy
	localToken string // desktop auth token for loopback defense-in-depth
}

// NewRemoteProxy creates a reverse proxy for the given remote workspace.
// localToken is the desktop's own auth token (for loopback defense-in-depth).
// Pass "" to skip local token validation.
func NewRemoteProxy(workspace *remote.RemoteWorkspace, localToken string) (*RemoteProxy, error) {
	apiURL, err := url.Parse(workspace.APIURL())
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}

	apiProxy := httputil.NewSingleHostReverseProxy(apiURL)

	remoteToken := workspace.State.Token

	apiProxy.Director = func(req *http.Request) {
		injectAuth(req, remoteToken)
		// Rewrite URL to target (default Director does this;
		// we replaced it so must include it)
		req.URL.Scheme = apiURL.Scheme
		req.URL.Host = apiURL.Host
		req.URL.Path = singleJoiningSlash(apiURL.Path, req.URL.Path)
	}
	apiProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Remote server unreachable", http.StatusBadGateway)
	}

	return &RemoteProxy{
		apiProxy:   apiProxy,
		localToken: localToken,
	}, nil
}

// injectAuth strips the local desktop auth token from the request
// and injects the remote bearer token from ServeState. The remote
// token never reaches the browser, is never logged, and is never stored.
func injectAuth(req *http.Request, remoteToken string) {
	// Strip local desktop token (query param)
	q := req.URL.Query()
	q.Del("token")
	req.URL.RawQuery = q.Encode()
	// Inject remote auth token
	if remoteToken != "" {
		req.Header.Set("Authorization", "Bearer "+remoteToken)
	}
}

// ServeHTTP routes incoming requests:
//   - /api/* → API proxy (all remote server endpoints)
//   - everything else → 404
//
// Auth flow:
//   1. Browser sends request with local desktop token
//      (?token=LOCAL or Authorization: Bearer LOCAL)
//   2. Proxy validates local token against known desktop token
//      (defense-in-depth; loopback binding is the primary barrier)
//   3. Proxy strips local token from request
//   4. Proxy injects remote token from ServeState
//      (Authorization: Bearer REMOTE)
//   5. Request goes through SSH tunnel to remote server
//   6. Remote server validates REMOTE token
func (rp *RemoteProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if rp.localToken != "" {
			local := r.URL.Query().Get("token")
			if local == "" {
				auth := r.Header.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					local = strings.TrimPrefix(auth, "Bearer ")
				}
			}
			if local != rp.localToken {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		rp.apiProxy.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

// singleJoiningSlash joins a and b with exactly one slash between them
// (mirrors net/http/httputil's unexported helper).
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
