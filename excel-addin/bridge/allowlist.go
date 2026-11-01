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
	newAPIRoute("GET", "/api/sessions/{id}/context"),
	newAPIRoute("POST", "/api/sessions/{id}/message"),
	newAPIRoute("POST", "/api/sessions/{id}/cancel"),
	newAPIRoute("POST", "/api/sessions/{id}/retry"),
	newAPIRoute("POST", "/api/sessions/{id}/compact"),
	newAPIRoute("PUT", "/api/sessions/{id}/title"),
	newAPIRoute("PUT", "/api/sessions/{id}/model"),
	newAPIRoute("POST", "/api/sessions/{id}/rewinds"),
	newAPIRoute("GET", "/api/sessions/{id}/user-messages"),
	newAPIRoute("POST", "/api/permissions/resolve"),
	newAPIRoute("PUT", "/api/permissions/mode"),
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
// Some routes change what ocode does with the request, so their bodies are constrained too (bodyConstrained):
// POST /api/chat (a new session runs an agent in `project_path`, in the `permission_mode` it asks for, with the MCP
// servers it pins), POST /api/projects (registers a directory) and PUT /api/permissions/mode (sets a session's
// permission mode). Chat and projects must stay inside workspaceRoot. A chat may not ask for sandbox, may pin only
// the excel MCP server (pinnedServersOK), and a mode may only be one of allowedChatModes or allowedPermissionModes.
// With no workspaceRoot the chat and project routes are refused (fail closed).
func allowAPI(workspaceRoot string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := safeAPIPath(r)
		if !ok || !apiRouteAllowed(r.Method, p) {
			refuseAPI(w)
			return
		}
		if bodyConstrained(r.Method, p) {
			if !constrainBody(w, r, p, workspaceRoot) {
				return
			}
		}
		stripTokenQuery(r)
		next.ServeHTTP(w, r)
	})
}

// allowedChatModes are the permission modes a pane-started session may use: ask (normal), locked, and yolo (no
// prompts). yolo is allowed on purpose: the pane's permission picker offers it behind a confirm (RESUME.md,
// 2026-11-01). sandbox is still refused.
var allowedChatModes = map[string]bool{"": true, "normal": true, "locked": true, "yolo": true}

// allowedPermissionModes are the modes PUT /api/permissions/mode may set on one session. The field is required.
var allowedPermissionModes = map[string]bool{"normal": true, "locked": true, "yolo": true}

// apiSessionIDRe is apiIDPattern anchored to the whole value: the session id in a PUT /api/permissions/mode body.
var apiSessionIDRe = regexp.MustCompile("^" + apiIDPattern + "$")

// bodyConstrained reports whether constrainBody checks the JSON body of this (method, path) pair.
func bodyConstrained(method, p string) bool {
	switch {
	case method == http.MethodPost && (p == "/api/chat" || p == "/api/projects"):
		return true
	case method == http.MethodPut && p == "/api/permissions/mode":
		return true
	}
	return false
}

// constrainBody validates the JSON body of a bodyConstrained route and puts the original bytes back for the proxy.
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
		okBody = ok1 && ok2 && allowedChatModes[mode] && insideRoot(root, pp) && pinnedServersOK(m) && bodyKeysExact(m, "project_path", "permission_mode", "pinned_mcp_servers")
	case "/api/projects":
		pp, ok1 := str("path")
		host, ok2 := str("host")
		okBody = ok1 && ok2 && host == "" && m["port"] == nil && insideRoot(root, pp) && bodyKeysExact(m, "path", "host", "port")
	case "/api/permissions/mode":
		mode, ok1 := str("mode")
		sid, ok2 := str("session_id")
		okBody = ok1 && ok2 && allowedPermissionModes[mode] && apiSessionIDRe.MatchString(sid) && bodyKeysExact(m, "mode", "session_id")
	}
	if !okBody {
		refuseAPI(w)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return true
}

// excelMCPServer is the one MCP server a pane chat may pin: the bridge's own tools, named as in ocode's `mcp` map.
const excelMCPServer = "excel"

// pinnedServersOK accepts an absent or null pinned_mcp_servers, or exactly the JSON array ["excel"]. A pin attaches
// every tool of the named server from the first turn, so any other array, a string, a non-string element, an empty
// list or a duplicate is refused.
func pinnedServersOK(m map[string]any) bool {
	v, present := m["pinned_mcp_servers"]
	if !present || v == nil {
		return true
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 1 {
		return false
	}
	name, ok := arr[0].(string)
	return ok && name == excelMCPServer
}

// bodyKeysExact refuses a body with a key that differs from one of the names the checks read only by letter case
// or Unicode case folding. ocode decodes bodies with encoding/json into structs, and that matches "MODE" or a long-s
// "ſession_id" to the field tagged "mode" or "session_id", while the exact-name lookups above would not see the key.
func bodyKeysExact(m map[string]any, names ...string) bool {
	for k := range m {
		for _, n := range names {
			if k != n && strings.EqualFold(k, n) {
				return false
			}
		}
	}
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
