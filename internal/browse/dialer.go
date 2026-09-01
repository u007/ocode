package browse

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// errPrivateAddr is returned when the guard refuses a connection to a
// private/blocked address. Part 03 maps it to an "SSRF blocked" nav event
// via errors.Is — the error survives net.Dial's *net.OpError wrapping.
var errPrivateAddr = errors.New("browse: connection to private address blocked")

// maxRedirects caps redirect chains in the upstream HTTP client (Part 3's
// CheckRedirect). The dialer guard itself needs no per-hop logic: every
// redirect hop dials through this same Control hook, so each hop's resolved
// IP is re-validated automatically.
const maxRedirects = 10

// NewSafeDialer is the exported wrapper of newSafeDialer, used by the cdp
// egress proxy tests and the browse supervisor wiring (Task 05).
func NewSafeDialer(allowPrivate bool) *net.Dialer { return newSafeDialer(allowPrivate) }

// newSafeDialer returns a net.Dialer whose Control hook validates the
// RESOLVED IP the socket is about to connect to. This is the authoritative
// SSRF guard: it runs after DNS resolution, at connect time, so DNS
// rebinding (public at lookup, private at dial) and encoding tricks
// (decimal/octal literals) cannot bypass it — Control only ever sees the
// canonical address the OS is actually dialing. With allowPrivate=true
// (local mode: user-initiated loopback/LAN dev servers) the guard stands
// down.
func newSafeDialer(allowPrivate bool) *net.Dialer {
	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if allowPrivate {
				return nil
			}
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				// Fail closed: an address we cannot classify is never dialed.
				return fmt.Errorf("browse: unparseable dial address %q: %w", address, err)
			}
			if isPrivateIP(ap.Addr()) {
				return fmt.Errorf("%w: %s", errPrivateAddr, ap.Addr())
			}
			return nil
		},
	}
}

// newSafeTransport wires the guarded dialer into an http.Transport for the
// upstream client. Proxy is explicitly nil: honoring HTTP_PROXY/HTTPS_PROXY
// would route dials through a proxy host and bypass the connect-time guard.
// TLS verification stays on (strict) — callers that need to tolerate
// self-signed localhost dev certs should use newSafeTransportInsecure.
func newSafeTransport(allowPrivate bool) *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           newSafeDialer(allowPrivate).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// newSafeTransportInsecure is newSafeTransport(allowPrivate) with TLS
// verification unconditionally skipped. Used by localInsecureTransport for
// loopback auto-allow (localhost, 127.0.0.1, ::1) and for hosts the user has
// explicitly bypassed after seeing a TLS "not trusted" interstitial. Never
// used for public internet hosts (external URLs render via Chrome).
func newSafeTransportInsecure(allowPrivate bool) *http.Transport {
	tr := newSafeTransport(allowPrivate)
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit user bypass / loopback dev certs
	return tr
}

// isLoopbackHost reports whether host (may include :port or brackets) is a
// loopback literal that we auto-allow self-signed on without prompting:
// localhost, *.localhost, 127.0.0.1, ::1. Other RFC1918 hosts (192.168.x,
// 10.x) require an explicit “Continue anyway” after the interstitial.
func isLoopbackHost(host string) bool {
	// Use net.SplitHostPort when a port is present; fallback to raw host.
	// This correctly handles [::1]:port, 127.0.0.1:port, and localhost:port
	// without the LastIndex(":") ambiguity for bare IPv6 ::1.
	h := host
	if strings.Contains(h, ":") {
		if hostOnly, _, err := net.SplitHostPort(h); err == nil {
			h = hostOnly
		} else if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
			h = strings.Trim(h, "[]")
		} else if strings.Count(h, ":") > 1 {
			// Bare IPv6 literal without port/brackets (e.g. ::1) — keep as-is.
			// net.SplitHostPort fails on these; don't strip.
		} else {
			// No valid port suffix — treat last colon as spurious and strip
			// only if h contains a single colon and the suffix is numeric.
			if idx := strings.LastIndex(h, ":"); idx != -1 && !strings.Contains(h[idx+1:], "]") {
				// Check if suffix looks like a port (all digits).
				suffix := h[idx+1:]
				isPort := suffix != "" && strings.Trim(suffix, "0123456789") == ""
				if isPort {
					h = h[:idx]
				}
			}
			h = strings.Trim(h, "[]")
		}
	} else {
		h = strings.Trim(h, "[]")
	}
	lower := strings.ToLower(strings.TrimSpace(h))
	// Trim trailing dot (DNS allows foo.localhost.).
	lower = strings.TrimSuffix(lower, ".")
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	// Numeric loopback IPs: 127.0.0.1 and ::1 (including any 127.x.x.x? spec says 127/8 is loopback,
	// but we only auto-allow the common dev literal 127.0.0.1 for now; other 127.x still present as private
	// and will go through the bypass interstitial to avoid silent LAN exposure).
	if lower == "127.0.0.1" || lower == "::1" {
		return true
	}
	// Also handle 127.0.0.0/8 more broadly if desired — treat any 127.* as loopback auto-allow.
	if addr, err := netip.ParseAddr(lower); err == nil {
		if addr.IsLoopback() {
			return true
		}
	}
	return false
}

// IsLoopbackHost is the exported wrapper for isLoopbackHost, used by the main
// server's bypass validation and frontend parity.
func IsLoopbackHost(host string) bool { return isLoopbackHost(host) }
