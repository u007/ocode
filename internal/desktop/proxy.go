package desktop

import (
	"net/http"
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
type RemoteProxy struct {
	apiProxy   http.Handler
	localToken string // desktop auth token for loopback defense-in-depth
}

// NewRemoteProxy creates a reverse proxy for the given remote workspace.
// localToken is the desktop's own auth token (for loopback defense-in-depth).
// Pass "" to skip local token validation.
func NewRemoteProxy(workspace *remote.RemoteWorkspace, localToken string) (*RemoteProxy, error) {
	proxy, err := remote.NewAPIProxy(workspace.APIURL(), workspace.State.Token, nil)
	if err != nil {
		return nil, err
	}

	return &RemoteProxy{
		apiProxy:   proxy,
		localToken: localToken,
	}, nil
}

// ServeHTTP routes incoming requests:
//   - /api/* → API proxy (all remote server endpoints)
//   - everything else → 404
//
// Auth flow:
//  1. Browser sends request with local desktop token
//     (?token=LOCAL or Authorization: Bearer LOCAL)
//  2. Proxy validates local token against known desktop token
//     (defense-in-depth; loopback binding is the primary barrier)
//  3. Proxy strips local token from request
//  4. Proxy injects remote token from ServeState
//     (Authorization: Bearer REMOTE)
//  5. Request goes through SSH tunnel to remote server
//  6. Remote server validates REMOTE token
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
