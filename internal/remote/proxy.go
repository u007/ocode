package remote

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// WSProtocolPrefix namespaces the bearer token carried in the
// Sec-WebSocket-Protocol header list. internal/server validates incoming
// tokens against this same prefix, so both packages must agree on one value.
const WSProtocolPrefix = "ocode.bearer."

// browserWSProtocolKey stashes the browser's originally offered
// Sec-WebSocket-Protocol value on the outbound request context. The Director
// sets it before InjectAuth rewrites the header; ModifyResponse reads it back
// to restore the browser's offer on a 101 without leaking the remote token.
type browserWSProtocolKey struct{}

// isUpgradeRequest reports whether req is a websocket handshake.
func isUpgradeRequest(req *http.Request) bool {
	return strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}

// splitProtocols splits a Sec-WebSocket-Protocol header value into its
// trimmed, non-empty tokens.
func splitProtocols(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// joinProtocols joins protocol tokens back into a header value.
func joinProtocols(protos []string) string {
	return strings.Join(protos, ", ")
}

// NewAPIProxy builds a reverse proxy that forwards requests to the
// remote API at apiURL. It strips local ?token= query params and
// injects the remote bearer token via InjectAuth.
//
// onError is called (if non-nil) from the ErrorHandler when a RoundTrip
// failure occurs, before writing the 502 response, so callers can drop
// cached workspace state. Note: a backend that dies mid-stream after
// sending headers does not trigger onError or a 502.
func NewAPIProxy(apiURL string, remoteToken string, onError func(error)) (*httputil.ReverseProxy, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse API URL %q: %w", apiURL, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(u)

	proxy.Director = func(req *http.Request) {
		// Stash the browser's offered subprotocol before InjectAuth replaces
		// it with the remote token, so ModifyResponse can restore it on 101.
		ctx := context.WithValue(req.Context(), browserWSProtocolKey{}, req.Header.Get("Sec-WebSocket-Protocol"))
		*req = *req.WithContext(ctx)
		InjectAuth(req, remoteToken)
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		req.URL.Path = singleJoiningSlash(u.Path, req.URL.Path)
	}

	// A 101 Switching Protocols response from the remote server echoes
	// ocode.bearer.<remoteToken> (terminalUpgradeRespHeader). Forwarding it
	// would break the browser handshake (it never offered that protocol) and
	// leak the remote token, which the remote token model forbids. Restore
	// exactly what the browser offered; drop the header when it offered none.
	proxy.ModifyResponse = func(res *http.Response) error {
		if res.StatusCode != http.StatusSwitchingProtocols {
			return nil
		}
		offered := ""
		if res.Request != nil {
			if v, ok := res.Request.Context().Value(browserWSProtocolKey{}).(string); ok {
				offered = v
			}
		}
		if offered == "" {
			res.Header.Del("Sec-WebSocket-Protocol")
		} else {
			res.Header.Set("Sec-WebSocket-Protocol", offered)
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("remote proxy: %s %s: %v", r.Method, r.URL.Path, err)
		if onError != nil {
			onError(err)
		}
		http.Error(w, "Remote server unreachable", http.StatusBadGateway)
	}

	return proxy, nil
}

// InjectAuth strips the local ?token= query param from the request
// and replaces the Authorization header with the remote bearer token.
// When remoteToken is empty, Authorization is deleted (but the local
// token is still stripped).
//
// For a websocket Upgrade it also rewrites the offered
// Sec-WebSocket-Protocol list: every ocode.bearer.* entry (the browser's
// local token, when the local server is itself in remote mode) is removed and
// the remote token is appended. The remote server is in --remote mode, where
// it reads the websocket token from exactly this header and rejects ?token=.
func InjectAuth(req *http.Request, remoteToken string) {
	// Strip local token query param.
	q := req.URL.Query()
	q.Del("token")
	req.URL.RawQuery = q.Encode()

	if remoteToken != "" {
		req.Header.Set("Authorization", "Bearer "+remoteToken)
	} else {
		req.Header.Del("Authorization")
	}

	if !isUpgradeRequest(req) {
		return
	}
	protos := splitProtocols(req.Header.Get("Sec-WebSocket-Protocol"))
	kept := protos[:0]
	for _, p := range protos {
		if strings.HasPrefix(p, WSProtocolPrefix) {
			continue
		}
		kept = append(kept, p)
	}
	protos = kept
	if remoteToken != "" {
		protos = append(protos, WSProtocolPrefix+remoteToken)
	}
	if len(protos) == 0 {
		req.Header.Del("Sec-WebSocket-Protocol")
		return
	}
	req.Header.Set("Sec-WebSocket-Protocol", joinProtocols(protos))
}

// singleJoiningSlash joins a and b with exactly one slash between them.
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
