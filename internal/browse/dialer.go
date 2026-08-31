package browse

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
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
// No TLSClientConfig override — certificate verification stays on.
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
