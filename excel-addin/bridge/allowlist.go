package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
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
//
// Two routes can start work anywhere on the machine, so their request bodies are constrained too: POST /api/chat
// (a new session runs an agent in `project_path`, in the `permission_mode` it asks for) and POST /api/projects
// (registers a directory). Both must stay inside workspaceRoot, and a chat may not ask for yolo or any other
// mode that skips permission prompts. With no workspaceRoot those two routes are refused (fail closed).
func allowAPI(workspaceRoot string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := safeAPIPath(r)
		if !ok || !apiRouteAllowed(r.Method, p) {
			refuseAPI(w)
			return
		}
		if r.Method == http.MethodPost && (p == "/api/chat" || p == "/api/projects") {
			if !constrainBody(w, r, p, workspaceRoot) {
				return
			}
		}
		stripTokenQuery(r)
		next.ServeHTTP(w, r)
	})
}

// allowedChatModes are the permission modes a pane-started session may use. "yolo" (no prompts) and the rest are refused.
var allowedChatModes = map[string]bool{"": true, "normal": true, "locked": true}

// constrainBody validates the JSON body of /api/chat and /api/projects and puts the original bytes back for the proxy.
func constrainBody(w http.ResponseWriter, r *http.Request, route, root string) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		refuseAPI(w)
		return false
	}
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		refuseAPI(w)
		return false
	}
	str := func(k string) (string, bool) {
		v, present := m[k]
		if !present || v == nil {
			return "", true
		}
		s, ok := v.(string)
		return s, ok
	}
	okBody := false
	switch route {
	case "/api/chat":
		pp, ok1 := str("project_path")
		mode, ok2 := str("permission_mode")
		okBody = ok1 && ok2 && allowedChatModes[mode] && insideRoot(root, pp)
	case "/api/projects":
		pp, ok1 := str("path")
		host, ok2 := str("host")
		okBody = ok1 && ok2 && host == "" && m["port"] == nil && insideRoot(root, pp)
	}
	if !okBody {
		refuseAPI(w)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return true
}

// insideRoot reports whether p is an absolute, clean path strictly below root. "~" and relative paths are refused
// (ocode would expand them against its own home). Symlinks are not resolved: the workspace folders are created by the bridge.
func insideRoot(root, p string) bool {
	if root == "" || p == "" || strings.HasPrefix(p, "~") || !filepath.IsAbs(p) || !filepath.IsAbs(root) {
		return false
	}
	if filepath.Clean(p) != p {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
