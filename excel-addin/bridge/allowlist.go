package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// The /api/ proxy injects the bridge's Bearer token, so whatever it forwards runs with ocode serve's full
// authority: terminals, shell, permission modes, files, git. The pane key that gates /api/ travels in the
// manifest URL and can leak, so the proxy forwards only the (method, path) pairs the task pane calls
// (taskpane/src/ocodeClient.ts). Everything else under /api/ is refused here and never reaches ocode.

// apiIDPattern matches one path segment that is a session, project or request id.
const apiIDPattern = `[A-Za-z0-9_.-]+`

type apiRoute struct {
	method string
	re     *regexp.Regexp
}

// allowedAPIRoutes is the complete list. Adding a route is a security decision: check what the handler can do.
var allowedAPIRoutes = []apiRoute{
	newAPIRoute("GET", "/api/events"),
	newAPIRoute("POST", "/api/chat"),
	newAPIRoute("GET", "/api/sessions"),
	newAPIRoute("GET", "/api/sessions/{id}"),
	newAPIRoute("GET", "/api/sessions/{id}/state"),
	newAPIRoute("GET", "/api/sessions/{id}/recap"),
	newAPIRoute("POST", "/api/sessions/{id}/message"),
	newAPIRoute("POST", "/api/sessions/{id}/cancel"),
	newAPIRoute("POST", "/api/sessions/{id}/retry"),
	newAPIRoute("POST", "/api/sessions/{id}/compact"),
	newAPIRoute("PUT", "/api/sessions/{id}/title"),
	newAPIRoute("PUT", "/api/sessions/{id}/model"),
	newAPIRoute("POST", "/api/sessions/{id}/rewinds"),
	newAPIRoute("GET", "/api/sessions/{id}/user-messages"),
	newAPIRoute("POST", "/api/permissions/resolve"),
	newAPIRoute("POST", "/api/questions"),
	newAPIRoute("GET", "/api/models"),
	newAPIRoute("POST", "/api/projects"),
}

func newAPIRoute(method, pattern string) apiRoute {
	expr := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\{id\}`, apiIDPattern) + "$"
	return apiRoute{method: method, re: regexp.MustCompile(expr)}
}

// allowAPI forwards only allowlisted (method, path) pairs to next. It must sit behind auth, so an
// unauthenticated caller gets 401 before the route is even checked.
func allowAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := safeAPIPath(r)
		if !ok || !apiRouteAllowed(r.Method, p) {
			refuseAPI(w)
			return
		}
		stripTokenQuery(r)
		next.ServeHTTP(w, r)
	})
}

// safeAPIPath returns the decoded request path only when it is already clean: no traversal, no empty or
// dot segments, no encoded or literal separator tricks, no NUL. Matching is done on this exact string,
// so the path matched is the path forwarded.
func safeAPIPath(r *http.Request) (string, bool) {
	p := r.URL.Path
	if p == "" || p[0] != '/' {
		return "", false
	}
	for _, bad := range []string{"..", "//", "\x00", `\`} {
		if strings.Contains(p, bad) {
			return "", false
		}
	}
	// %2F decodes to "/" in URL.Path, so the decoded check alone cannot see it; refuse the encoded form.
	for _, raw := range []string{r.URL.EscapedPath(), r.URL.RawPath} {
		lower := strings.ToLower(raw)
		if strings.Contains(lower, "%2f") || strings.Contains(lower, "%00") || strings.Contains(lower, "%5c") {
			return "", false
		}
	}
	if path.Clean(p) != p {
		return "", false
	}
	return p, true
}

func apiRouteAllowed(method, p string) bool {
	for _, rt := range allowedAPIRoutes {
		if rt.method == method && rt.re.MatchString(p) {
			return true
		}
	}
	return false
}

// stripTokenQuery removes a token= query parameter. The pane never sends one; if one arrives it is
// dropped, not forwarded to ocode.
func stripTokenQuery(r *http.Request) {
	if r.URL.RawQuery == "" {
		return
	}
	kept := make([]string, 0)
	for _, part := range strings.Split(r.URL.RawQuery, "&") {
		name, _, _ := strings.Cut(part, "=")
		if name == "token" {
			continue
		}
		if u, err := url.QueryUnescape(name); err == nil && u == "token" {
			continue
		}
		kept = append(kept, part)
	}
	r.URL.RawQuery = strings.Join(kept, "&")
}

func refuseAPI(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "this route is not available through the Excel bridge"})
}
