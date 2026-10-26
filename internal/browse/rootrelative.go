package browse

// Root-relative fallback for dev-server module graphs.
//
// rewriteHTML maps root-relative ATTRIBUTE URLs onto the /b/ route, but
// JavaScript bodies stream untouched, and the static imports inside them are
// root-absolute on Vite/TanStack dev servers ("/node_modules/.vite/deps/react.js",
// "/@fs/...", "/@vite/env"). The browser resolves those against the browse
// ORIGIN root, outside the /b/{stateKey}/{scheme}/{host} mount, so without this
// handler they 404 and the whole module graph dies — a blank page.
//
// Every such request carries a Referer: the URL of the importing module or
// document. The browser sets it itself (page script cannot forge it for a
// same-origin fetch) and it lives on the /b/ route, so the canonical route for
// the requested path is rebuilt from it and answered with a redirect.
//
// A REDIRECT, never an in-place proxy: after the redirect the module's final
// URL is the /b/ one, so its own relative imports resolve under the mount and
// its root-relative ones arrive here with a /b/ Referer again. Proxying in
// place would serve the module at a root URL and break one level deeper. 307
// keeps the method and body, so a root-relative POST from a dev SPA survives.
//
// No cookie or upstream call is involved: the target is same-origin, derived
// only from the request's own Referer, and the /b/ handler authenticates the
// redirected request exactly as it does every other one. A remote-project
// mount ("/api/remote/{host}/browse/b/...") is carried over from the Referer
// path, so the same function serves the desktop browse-proxy listener's root
// (internal/server), where the remote iframe's origin actually lives.
//
// Referrer policy: the Referer must carry the full /b/ path for any of this to
// work. The proxy therefore drops upstream Referrer-Policy headers and
// <meta name="referrer"> tags from the documents it serves (local.go and
// rewrite.go). A page that still sends a path-stripping policy such as
// no-referrer or origin, or an element-level referrerpolicy attribute, gets a
// 404 whose body names the cause (handleRootRelative).
//
// Known gap (TODO.md): a WebSocket handshake carries no Referer, so Vite's HMR
// socket at the origin root is not resolved. The page renders; hot reload
// does not connect.

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// rootRelativeMountRe splits a Referer path into its mount prefix (a remote
// project's "/api/remote/{host}/browse", or "" for the root-mounted origin)
// and the /b/ route. The host segment cannot contain "/", so the split is
// unambiguous even when the host or the upstream path contains "b".
var rootRelativeMountRe = regexp.MustCompile(`^((?:/api/remote/[^/]+/browse)?)(/b/.*)$`)

// RootRelativeRedirect rebuilds the canonical /b/ route for a root-relative
// request (escapedPath, rawQuery) from the Referer it arrived with. ok is
// false when the Referer is absent, unparseable, or not a /b/ route — the
// caller answers 404 then, exactly as the origin root did before.
func RootRelativeRedirect(referer, escapedPath, rawQuery string) (loc string, ok bool) {
	if referer == "" || !strings.HasPrefix(escapedPath, "/") {
		return "", false
	}
	u, err := url.Parse(referer)
	if err != nil {
		return "", false
	}
	m := rootRelativeMountRe.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	t, err := parseTarget(m[2], "")
	if err != nil {
		return "", false
	}
	loc = fmt.Sprintf("%s/b/%s/%s/%s%s", m[1], t.StateKey, t.Scheme, t.Host, escapedPath)
	if rawQuery != "" {
		loc += "?" + rawQuery
	}
	return loc, true
}

// handleRootRelative serves every origin path outside /b/ and the capture
// script. See the file comment.
func (s *Server) handleRootRelative(w http.ResponseWriter, r *http.Request) {
	loc, ok := RootRelativeRedirect(r.Header.Get("Referer"), r.URL.EscapedPath(), r.URL.RawQuery)
	if !ok {
		WriteRootRelativeNotFound(w, r, s.log)
		return
	}
	http.Redirect(w, r, loc, http.StatusTemporaryRedirect)
}

// WriteRootRelativeNotFound answers a root-relative request that has no usable
// /b/ Referer: a plain-text 404 that names the path and the likely cause, plus
// one log line. Both root handlers (the browse origin and the desktop
// browse-proxy listener) use it, so the body text lives only here.
func WriteRootRelativeNotFound(w http.ResponseWriter, r *http.Request, logger *log.Logger) {
	path := r.URL.EscapedPath()
	logger.Printf("browse: root-relative request %s without a /b/ Referer; answering 404", path)
	http.Error(w, fmt.Sprintf("root-relative request without a /b/ Referer: %s. The embedded browser resolves root-relative dev-server imports from the Referer; if the page sets a no-referrer policy, open it in Chrome mode.", path), http.StatusNotFound)
}
