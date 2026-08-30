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

// browseBlockedPrefixes is the enumerated SSRF block list from the design
// spec. Kept as explicit prefixes (rather than only stdlib Is* helpers) so
// the list is auditable against the spec and testable range by range.
var browseBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),    // loopback
	netip.MustParsePrefix("10.0.0.0/8"),     // RFC1918
	netip.MustParsePrefix("172.16.0.0/12"),  // RFC1918
	netip.MustParsePrefix("192.168.0.0/16"), // RFC1918
	netip.MustParsePrefix("100.64.0.0/10"),  // CGNAT
	netip.MustParsePrefix("169.254.0.0/16"), // link-local incl. metadata 169.254.169.254
	netip.MustParsePrefix("0.0.0.0/32"),     // unspecified v4
	netip.MustParsePrefix("::1/128"),        // loopback v6
	netip.MustParsePrefix("fc00::/7"),       // ULA
	netip.MustParsePrefix("fe80::/10"),      // link-local v6
	netip.MustParsePrefix("::/128"),         // unspecified v6
}

// isPrivateIP reports whether ip falls in any blocked range. IPv4-mapped
// IPv6 addresses (::ffff:a.b.c.d) are unmapped first so they classify by
// their embedded IPv4 address. Non-canonical literal encodings (decimal,
// octal) never reach this function: classification happens on the resolved
// netip.Addr at connect time, not on user-supplied text.
func isPrivateIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range browseBlockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
