package remote

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

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
		InjectAuth(req, remoteToken)
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		req.URL.Path = singleJoiningSlash(u.Path, req.URL.Path)
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
