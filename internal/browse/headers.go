package browse

import "net/http"

// fixedUserAgent is sent to every upstream so the real browser/user is not
// fingerprinted through the proxy and behavior is deterministic.
const fixedUserAgent = "Mozilla/5.0 (compatible; ocode-browse/1.0)"

// responseHeadersToStrip are security headers that would break embedding or
// re-impose the upstream's now-invalid trust assumptions. The whole CSP is
// removed (partial stripping would block the injected capture script and the
// rewritten same-origin assets — see spec § External mode).
var responseHeadersToStrip = []string{
	"X-Frame-Options",
	"Content-Security-Policy",
	"Content-Security-Policy-Report-Only",
	"Strict-Transport-Security",
	"Service-Worker-Allowed",
}

// stripSecurityHeaders removes framing/SW/CSP/HSTS headers from an upstream
// response so the page can render inside the sandboxed iframe.
func stripSecurityHeaders(h http.Header) {
	for _, k := range responseHeadersToStrip {
		h.Del(k)
	}
}

// requestHeadersToDrop are hop-by-hop or leak-prone headers removed before the
// request leaves for upstream. Cookie is dropped because site cookies live in
// the server-side jar, never in the browser (see cookiejar.go).
var requestHeadersToDrop = []string{
	"Referer",
	"Cookie",
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Transfer-Encoding",
	"Upgrade",
	"Te",
	"Trailer",
}

// filterRequestHeaders sanitizes an outbound upstream request: drops the
// referer (no origin leak), pins a fixed UA, and forces an encoding we can
// decode so HTML/CSS can be rewritten.
func filterRequestHeaders(r *http.Request) {
	for _, k := range requestHeadersToDrop {
		r.Header.Del(k)
	}
	r.Header.Set("User-Agent", fixedUserAgent)
	r.Header.Set("Accept-Encoding", "gzip, identity")
	// RequestURI must be empty for client requests; caller builds a fresh URL.
	r.RequestURI = ""
}
