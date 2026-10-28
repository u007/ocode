package tool

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"
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
// trailing dot; and any IP address that is link-local or a known cloud-metadata
// address, however it is written: dotted, decimal (2852039166), hex
// (0xa9fea9fe), octal (0251.0376.0251.0376), short forms, IPv4-mapped IPv6, and
// bracketed IPv6.
//
// Not refused here: loopback and LAN addresses. The browse layer's connect-time
// guard blocks LAN for browser traffic, and loopback is a deliberate local use.
func BrowserURLDenyReason(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "browser URL is not parseable"
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "browser may open only http and https URLs, not " + scheme + ":"
	}
	host := strings.TrimRight(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return "browser URL has no host"
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
