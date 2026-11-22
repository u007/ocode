package tool

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// browserIDNA maps a non-ASCII host the way a browser's URL parser does (WHATWG URL
// host parsing, which is UTS 46 with the lookup mapping): case and width folding,
// ideographic full stops as label separators, the non-transitional mapping, and
// the STD3 and hyphen rules off, so a name a browser accepts is not refused here.
// The joiner and bidi checks stay on, as the WHATWG parser runs them.
var browserIDNA = idna.New(
	idna.MapForLookup(),
	idna.StrictDomainName(false),
	idna.CheckHyphens(false),
	idna.CheckJoiners(true),
	idna.BidiRule(),
)

// BrowserURLDenyReason returns why the browser must refuse to open raw, or ""
// when the URL may proceed. It is the one check shared by the permission layer
// (which runs it before any mode shortcut, YOLO included) and the browser tool
// (which runs it on every navigate and open). It compares the NORMALISED host,
// not the raw string.
//
// It only classifies the URL the model asked for. Redirect hops, sub-resources
// and page-driven navigations are NOT re-checked here; the browse layer's proxy
// dial guard is what denies those, at resolve and connect time (see
// internal/browse/cdp/egress.go). Do not add a caller in cdp expecting this to
// cover a redirect.
//
// Refused: non-http(s) schemes; the GCP metadata hostname in any case or with a
// trailing dot; a host whose name does not map the way a browser maps it; and any
// IP address that is link-local or a known cloud-metadata address, however it is
// written: dotted, decimal (2852039166), hex (0xa9fea9fe), octal
// (0251.0376.0251.0376), short forms, IPv4-mapped IPv6, and bracketed IPv6. The
// host is mapped before these checks, so a fullwidth or ideographic spelling of a
// metadata address is refused as the address it names.
//
// Not refused here: loopback and LAN addresses. The browse layer's connect-time
// guard blocks LAN for browser traffic, and loopback is a deliberate local use.
//
// Every reason is a complete sentence that begins "browser may ...", so a caller
// shows it as it stands.
func BrowserURLDenyReason(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "browser may not open a URL that does not parse"
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "browser may open only http and https URLs, not " + scheme + ":"
	}
	host, ok := browserHostname(u)
	if !ok {
		return "browser may not open a host whose name does not map to a valid domain name"
	}
	if host == "" {
		return "browser may not open a URL with no host"
	}
	const metaReason = "browser may not open link-local or cloud-metadata addresses"
	if host == "metadata.google.internal" || strings.HasSuffix(host, ".metadata.google.internal") {
		return metaReason
	}
	if ip, ok := parseHostIP(host); ok && deniedBrowserAddr(ip) {
		return metaReason
	}
	return ""
}

// browserHostname returns u's host as a browser resolves it: lower case, with no
// trailing dot, and a non-ASCII name mapped to its ASCII form. ok is false when the
// name does not map. An ASCII host is returned unchanged, so mapping never alters
// a name that was already ASCII.
func browserHostname(u *url.URL) (string, bool) {
	host := u.Hostname()
	if !isASCIIHost(host) {
		mapped, err := browserIDNA.ToASCII(host)
		if err != nil {
			return "", false
		}
		host = mapped
	}
	return strings.TrimRight(strings.ToLower(host), "."), true
}

// isASCIIHost reports whether s is all ASCII.
func isASCIIHost(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// parseHostIP reads host as an IP address literal in any of the forms the OS
// resolver accepts: canonical IPv4 and IPv6, and the legacy IPv4 forms (decimal,
// hex and octal, one to four dot-separated parts).
func parseHostIP(host string) (netip.Addr, bool) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap(), true
	}
	v, ok := parseLegacyIPv4(host)
	if !ok {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true
}

// parseLegacyIPv4 implements inet_aton: 1 to 4 parts, each decimal, octal (a
// leading 0) or hex (a leading 0x); the last part fills the remaining bytes.
func parseLegacyIPv4(host string) (uint32, bool) {
	parts := strings.Split(host, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return 0, false
	}
	vals := make([]uint64, len(parts))
	for i, p := range parts {
		v, ok := parseIPv4Part(p)
		if !ok {
			return 0, false
		}
		vals[i] = v
	}
	last := len(vals) - 1
	for i := 0; i < last; i++ {
		if vals[i] > 255 {
			return 0, false
		}
	}
	if vals[last] >= 1<<(8*(4-last)) {
		return 0, false
	}
	var out uint64
	for i := 0; i < last; i++ {
		out |= vals[i] << (8 * (3 - i))
	}
	out |= vals[last]
	return uint32(out), true
}

// parseIPv4Part reads one inet_aton component.
func parseIPv4Part(p string) (uint64, bool) {
	if p == "" {
		return 0, false
	}
	base, digits := 10, p
	switch {
	case len(p) >= 2 && (p[:2] == "0x" || p[:2] == "0X"):
		base, digits = 16, p[2:]
		if digits == "" {
			return 0, true
		}
	case len(p) >= 2 && p[0] == '0':
		base, digits = 8, p[1:]
	}
	v, err := strconv.ParseUint(digits, base, 32)
	if err != nil {
		return 0, false
	}
	return v, true
}

// deniedBrowserAddr reports whether ip is link-local or a known cloud-metadata
// address. Metadata endpoints sit in these ranges or at fixed extra addresses
// (Alibaba's 100.100.100.200, AWS's fd00:ec2::254).
func deniedBrowserAddr(ip netip.Addr) bool {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	switch ip {
	case netip.MustParseAddr("100.100.100.200"), netip.MustParseAddr("fd00:ec2::254"):
		return true
	}
	return false
}

// BoxBrowserURLDenyReason is BrowserURLDenyReason for the upstash box backend. In
// a box, loopback and private addresses are the box's own network (its CDP
// endpoint sits on loopback), so they are refused here as well. It refuses the
// localhost names, loopback, private (RFC 1918 and fc00::/7), CGNAT 100.64/10,
// link-local and unspecified addresses, and IP literals in every form
// parseHostIP reads. The NAT64 (64:ff9b::/96) and 6to4 (2002::/16) prefixes are
// refused whole, since either can carry one of those addresses in its low bits.
// It is a literal check only: no name is resolved here, and the box driver's
// request interception and the box network policy cover the rest.
func BoxBrowserURLDenyReason(raw string) string {
	if reason := BrowserURLDenyReason(raw); reason != "" {
		return reason
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "browser may not open a URL that does not parse"
	}
	host, ok := browserHostname(u)
	if !ok {
		return "browser may not open a host whose name does not map to a valid domain name"
	}
	if host == "" {
		return "browser may not open a URL with no host"
	}
	const boxReason = "browser on the upstash box may not open loopback, private, link-local or unspecified addresses, or localhost"
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return boxReason
	}
	if ip, ok := parseHostIP(host); ok && deniedBoxAddr(ip) {
		return boxReason
	}
	return ""
}

// boxDeniedPrefixes are the ranges the box refuses that net/netip does not classify
// as private: "this network" (0.0.0.0/8, which reaches the local host on some
// systems), carrier-grade NAT (100.64.0.0/10), and the two IPv6 transition prefixes
// whose low bits carry an IPv4 address (NAT64 64:ff9b::/96 and 6to4 2002::/16).
// Refusing them whole is simpler than unwrapping each, and a public site has no
// reason to be addressed through them.
var boxDeniedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2002::/16"),
}

// deniedBoxAddr reports whether ip is one the box backend refuses. An IPv4-compatible
// IPv6 address (::a.b.c.d) is judged as the IPv4 address it carries.
func deniedBoxAddr(ip netip.Addr) bool {
	if ip.Is6() {
		b := ip.As16()
		if [12]byte(b[:12]) == [12]byte{} {
			ip = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
		}
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	for _, p := range boxDeniedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
