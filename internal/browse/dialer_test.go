package browse

import (
	"errors"
	"net"
	"net/netip"
	"testing"
)

// TestIsPrivateIPEnumeratedRanges pins the exact block list from the spec's
// SSRF guard section. Every one of these must be treated as private.
func TestIsPrivateIPEnumeratedRanges(t *testing.T) {
	private := []string{
		// 127.0.0.0/8 loopback
		"127.0.0.1", "127.255.255.254",
		// RFC1918
		"10.0.0.1", "10.255.255.255",
		"172.16.0.1", "172.31.255.255",
		"192.168.0.1", "192.168.255.255",
		// CGNAT 100.64.0.0/10
		"100.64.0.1", "100.127.255.255",
		// Link-local 169.254.0.0/16, incl. cloud metadata
		"169.254.0.1", "169.254.169.254",
		// Unspecified
		"0.0.0.0",
		// IPv6 loopback / ULA / link-local / unspecified
		"::1", "fc00::1", "fdff::1", "fe80::1", "::",
		// IPv4-mapped IPv6 forms of private addresses
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:192.168.1.1",
		"::ffff:169.254.169.254", "::ffff:100.64.0.1",
	}
	for _, s := range private {
		ip := netip.MustParseAddr(s)
		if !isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%s) = false, want true", s)
		}
	}

	public := []string{
		// Documentation/test ranges and real-world publics — must NOT be blocked.
		"93.184.216.34", "8.8.8.8", "1.1.1.1",
		"172.15.255.255", "172.32.0.1", // just outside 172.16/12
		"100.63.255.255", "100.128.0.1", // just outside 100.64/10
		"9.255.255.255", "11.0.0.1", // just outside 10/8
		"2606:2800:220:1:248:1893:25c8:1946", // example.com IPv6
		"::ffff:8.8.8.8",                     // IPv4-mapped public
	}
	for _, s := range public {
		ip := netip.MustParseAddr(s)
		if isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%s) = true, want false", s)
		}
	}
}

// controlCheck mirrors how the guard is invoked: the Control hook receives
// the canonical post-resolution "host:port" address. We call the hook
// directly (c syscall.RawConn is unused by our guard, so nil is safe).
func controlCheck(t *testing.T, d *net.Dialer, address string) error {
	t.Helper()
	if d.Control == nil {
		t.Fatal("newSafeDialer returned a dialer with no Control hook")
	}
	return d.Control("tcp4", address, nil)
}

func TestSafeDialerBlocksPrivateAddrs(t *testing.T) {
	d := newSafeDialer(false)
	blocked := []string{
		"127.0.0.1:80",
		"10.1.2.3:443",
		"169.254.169.254:80", // cloud metadata
		"[::1]:8080",
		"[fc00::1]:443",
		"[::ffff:192.168.1.1]:80", // IPv4-mapped
		"0.0.0.0:80",
	}
	for _, addr := range blocked {
		if err := controlCheck(t, d, addr); err == nil {
			t.Errorf("Control(%q) = nil, want errPrivateAddr", addr)
		}
	}
}

func TestSafeDialerAllowsPublicAddrs(t *testing.T) {
	d := newSafeDialer(false)
	allowed := []string{
		"93.184.216.34:443", // example.com documentation IP
		"8.8.8.8:53",
		"[2606:2800:220:1:248:1893:25c8:1946]:443",
	}
	for _, addr := range allowed {
		if err := controlCheck(t, d, addr); err != nil {
			t.Errorf("Control(%q) = %v, want nil", addr, err)
		}
	}
}

func TestSafeDialerAllowPrivateMode(t *testing.T) {
	// Local mode (loopback dev servers) constructs the dialer with
	// allowPrivate=true; the guard must stand down entirely.
	d := newSafeDialer(true)
	if err := controlCheck(t, d, "127.0.0.1:5173"); err != nil {
		t.Errorf("allowPrivate Control(127.0.0.1:5173) = %v, want nil", err)
	}
}

func TestSafeDialerRejectsUnparseableAddr(t *testing.T) {
	// A Control address that cannot be parsed must fail closed, never open.
	d := newSafeDialer(false)
	if err := controlCheck(t, d, "not-an-address"); err == nil {
		t.Error("Control(not-an-address) = nil, want error (fail closed)")
	}
	// Unix sockets (no host:port shape) must also fail closed — the browse
	// proxy only ever dials tcp/tcp4/tcp6.
	if err := d.Control("unix", "/tmp/daemon.sock", nil); err == nil {
		t.Error("Control(unix socket) = nil, want error (fail closed)")
	}
}

func TestSafeDialerBlockedErrorIsInspectionFriendly(t *testing.T) {
	// Part 03 maps errPrivateAddr to a 403-style "SSRF blocked" nav event
	// via errors.Is; the guard's error must stay unwrappable through
	// net.Dial's OpError wrapping. errors.Is on the direct Control result
	// is the contract here.
	d := newSafeDialer(false)
	err := controlCheck(t, d, "169.254.169.254:80")
	if err == nil {
		t.Fatal("Control(169.254.169.254:80) = nil, want errPrivateAddr")
	}
	if !errors.Is(err, errPrivateAddr) {
		t.Errorf("errors.Is(%v, errPrivateAddr) = false, want true", err)
	}
}

func TestSafeTransportUsesGuardedDialer(t *testing.T) {
	tr := newSafeTransport(false)
	if tr.DialContext == nil {
		t.Fatal("newSafeTransport: DialContext not wired to the safe dialer")
	}
	if tr.Proxy != nil {
		t.Error("newSafeTransport: environment proxy must be disabled (Proxy != nil) — an env proxy would bypass the connect-time guard")
	}
}
