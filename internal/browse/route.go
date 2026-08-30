package browse

import (
	"fmt"
	"net/netip"
	"strings"
)

// target is the stateless parse of /b/{stateKey}/{scheme}/{host}/{path...}.
// No server-side "current upstream" state exists; every request self-describes
// its upstream, so concurrent subresources and back/forward never race.
type target struct {
	StateKey string
	Scheme   string
	Host     string // may include :port
	Path     string // always starts with "/"
	RawQuery string
	Local    bool // host resolves to loopback/RFC1918 by literal inspection
}

// parseTarget splits the browse path. Shape: /b/{stateKey}/{scheme}/{host}/{rest...}
func parseTarget(urlPath, rawQuery string) (target, error) {
	rest, ok := strings.CutPrefix(urlPath, "/b/")
	if !ok {
		return target{}, fmt.Errorf("browse: path missing /b/ prefix: %q", urlPath)
	}
	// stateKey / scheme / host / path... — stateKey and scheme contain no slash.
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 {
		return target{}, fmt.Errorf("browse: path too short: %q", urlPath)
	}
	stateKey, scheme, host := parts[0], parts[1], parts[2]
	if stateKey == "" || (scheme != "http" && scheme != "https") || host == "" {
		return target{}, fmt.Errorf("browse: malformed target: %q", urlPath)
	}
	path := "/"
	if len(parts) == 4 && parts[3] != "" {
		path = "/" + parts[3]
	}
	return target{
		StateKey: stateKey,
		Scheme:   scheme,
		Host:     host,
		Path:     path,
		RawQuery: rawQuery,
		Local:    hostIsLiteralPrivate(host),
	}, nil
}

// hostIsLiteralPrivate reports whether host is an IP literal in a private
// range (best-effort literal check for routing mode only; the authoritative
// SSRF guard runs at dial time in Part 02). Hostnames return false here.
func hostIsLiteralPrivate(host string) bool {
	h := host
	if i := strings.LastIndex(h, ":"); i != -1 && !strings.Contains(h[i+1:], "]") {
		h = h[:i]
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	return isPrivateIP(addr)
}

// isPrivateIP is the enumerated private-range check. Part 02 tests it
// exhaustively; defined here so Part 01 compiles and routes correctly.
func isPrivateIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	// CGNAT 100.64.0.0/10.
	if ip.Is4() {
		b := ip.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
	}
	return false
}
