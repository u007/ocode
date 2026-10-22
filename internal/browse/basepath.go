package browse

import (
	"net/http"
	"strings"
)

// BasePathHeader is the header a reverse proxy in front of the browse origin
// uses to tell the origin under which path prefix it is mounted.
//
// The origin emits ROOT-RELATIVE absolute paths ("/b/{stateKey}/...",
// "/__ocode_capture.js") in every response it rewrites: the grant-redemption
// redirect, the HTML body rewrite, and the injected capture script. Those
// paths are only correct when the origin is mounted at the root. A remote
// project's origin is reached through the browse proxy under
// "/api/remote/{host}/browse", so a root-relative path would resolve against
// the proxy's root and hit the WRONG origin (this machine's, instead of the
// host's).
//
// The origin runs on the host and cannot know the local mount path, so the
// proxy sends it per request and the origin prefixes every absolute path it
// emits. An empty header (the normal, root-mounted case) changes nothing.
//
// The value MUST be a path prefix: it starts with "/" and never ends with
// one (except the root itself, which is empty). Anything else is ignored, so a
// hostile or malformed header cannot inject a scheme or an authority.
const BasePathHeader = "X-Ocode-Browse-Base-Path"

// basePathFor returns the normalized mount prefix for a request: "" for a
// root-mounted origin, or "/api/remote/host/browse" style prefix. A malformed
// value is ignored (treated as root-mounted) rather than trusted, because the
// prefix is spliced into every emitted URL.
func basePathFor(r *http.Request) string {
	return normalizeBasePath(r.Header.Get(BasePathHeader))
}

// normalizeBasePath validates and normalizes a mount prefix. It returns "" for
// the root mount. A value that does not start with "/", contains a character
// outside the path-safe set, a scheme or authority, or a ".." segment is
// rejected (""), because the prefix is spliced verbatim into emitted HTML
// attributes, URLs and cookie paths.
func normalizeBasePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	// Whitelist, not blacklist: a quote, angle bracket or whitespace in the
	// prefix would break out of the src="..." attribute it is spliced into.
	for i := 0; i < len(p); i++ {
		if !isBasePathByte(p[i]) {
			return ""
		}
	}
	if strings.Contains(p, "://") {
		return ""
	}
	// A protocol-relative "//host" prefix is an AUTHORITY, not a path: it
	// would make every emitted URL point at another host.
	if strings.HasPrefix(p, "//") {
		return ""
	}
	// Reject traversal segments: a prefix must not be able to climb out of
	// its own mount point.
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return ""
		}
	}
	// Strip a trailing slash so joining never produces "//".
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p
}

// isBasePathByte reports whether c may appear in a mount prefix. The set is
// ASCII letters, digits and the punctuation a remote host key uses ("user@ip",
// "host:port" style names), plus "/" as the segment separator.
func isBasePathByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("/-._~@:", c) >= 0
}

// cookiePath returns the Path attribute for the browse session cookie.
//
// The cookie must cover every path the origin serves under this mount: the
// "/b/{stateKey}/..." route tree AND "/__ocode_capture.js" (referenced by the
// injected script as a ROOT-RELATIVE path). A cookie scoped to "/b/" is not
// sent for the capture script, so the bundle 401s at the proxy gate.
//
// The root mount ("") therefore yields "/" — the whole origin — which is
// correct: the origin serves nothing but browse content. A prefixed mount
// yields the prefix, which covers both trees under it.
func cookiePath(basePath string) string {
	if basePath == "" {
		return "/"
	}
	return basePath + "/"
}

// withBasePath prefixes a root-relative path with the mount prefix.
// "/b/x" with prefix "/api/remote/h/browse" becomes
// "/api/remote/h/browse/b/x". An empty prefix is a no-op, which is the
// root-mounted (local) case.
func withBasePath(basePath, path string) string {
	if basePath == "" {
		return path
	}
	if path == "" {
		return basePath
	}
	if !strings.HasPrefix(path, "/") {
		return path
	}
	return basePath + path
}
