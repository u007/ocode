# Part 02 — Hardened SSRF Dialer

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ SSRF guard).

**Goal:** Build the connect-time SSRF guard. The dialer's `Control` hook validates the **resolved** IP the socket is actually about to connect to — the hostname is never trusted, which defeats DNS rebinding (a hostname that resolves public at check time and private at dial time is caught, because the check *is* the dial). Non-canonical IP literal encodings (decimal `2130706433`, octal `0177.0.0.1`) are neutralized for the same reason: by the time `Control` runs, the OS resolver has produced a canonical `host:port` address string that `netip` parses — we classify the post-resolution address, never the user-supplied literal. Redirects are covered automatically (every hop dials through the same guarded dialer); the HTTP client only needs a hop-count cap.

**Files:**
- Create: `internal/browse/dialer.go`, `internal/browse/dialer_test.go`
- Modify: `internal/browse/route.go` (expand `isPrivateIP` — the Part 01 stub is replaced by the exhaustive enumerated version below; it stays in `route.go` as the single canonical definition)

**Interfaces:**
- Consumes: `isPrivateIP(ip netip.Addr) bool` from `internal/browse/route.go` (Part 01 — expanded here, not redefined elsewhere).
- Produces (consumed by Part 03's upstream client):
  - `func newSafeDialer(allowPrivate bool) *net.Dialer`
  - `func newSafeTransport(allowPrivate bool) *http.Transport`
  - `var errPrivateAddr = errors.New(...)` — sentinel so callers can map the failure to a 403-style "SSRF blocked" nav event.
  - `const maxRedirects = 10` — Part 03 wires this into its `http.Client.CheckRedirect`.

---

- [ ] **Step 1: Write the failing enumerated-range test**

Create `internal/browse/dialer_test.go`:

```go
package browse

import (
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
```

Note on decimal/octal encodings: `netip.ParseAddr("2130706433")` and `netip.ParseAddr("0177.0.0.1")` both fail to parse — such literals never reach `isPrivateIP` as distinct forms. They are resolved (or rejected) by the OS before `Control` sees a canonical address, so no test case exists for them here; the guard classifies only canonical post-resolution addresses. This is by design, per the spec.

- [ ] **Step 2: Run to verify current state**

Run: `go test ./internal/browse/ -run TestIsPrivateIPEnumeratedRanges -v`
Expected: FAIL — the Part 01 stub relies on `ip.Unmap()` plus stdlib helpers but does not yet handle every enumerated case explicitly (in particular, verify the CGNAT boundary cases and IPv4-mapped forms; if the stub passes by accident, the explicit rewrite in Step 3 still replaces it so the block list is auditable against the spec).

- [ ] **Step 3: Replace the `isPrivateIP` stub with the canonical enumerated version**

In `internal/browse/route.go`, replace the Part 01 stub entirely (this file remains the single canonical definition):

```go
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
```

(Keep the existing `netip` import; remove any now-unused branches from the old stub.)

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/browse/ -run 'TestIsPrivateIPEnumeratedRanges|TestParseTarget' -v`
Expected: PASS — both the new range test and the Part 01 route tests (which exercise `hostIsLiteralPrivate` → `isPrivateIP`) stay green.

- [ ] **Step 5: Write the failing dialer Control test**

Append to `internal/browse/dialer_test.go`:

```go
import (
	"net"
	"net/netip"
	"testing"
)

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
```

(Merge the `import` block with the one from Step 1 — Go allows only one per file.)

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestSafeDialer -v`
Expected: FAIL — `undefined: newSafeDialer`, `undefined: newSafeTransport`.

- [ ] **Step 7: Implement the dialer and transport**

Create `internal/browse/dialer.go`:

```go
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
// private/blocked address. Part 03 maps it to an "SSRF blocked" nav event.
var errPrivateAddr = errors.New("browse: connection to private address blocked")

// maxRedirects caps redirect chains in the upstream HTTP client (Part 03's
// CheckRedirect). The dialer guard itself needs no per-hop logic: every
// redirect hop dials through this same Control hook, so each hop's resolved
// IP is re-validated automatically.
const maxRedirects = 10

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
```

- [ ] **Step 8: Run to verify pass**

Run: `go test ./internal/browse/ -v`
Expected: PASS — dialer tests plus all Part 01 tests (route, auth) stay green.

- [ ] **Step 9: Vet and build**

Run: `go vet ./internal/browse/ && go build ./...`
Expected: no findings, build OK.

- [ ] **Step 10: Commit**

```bash
git add internal/browse/dialer.go internal/browse/dialer_test.go internal/browse/route.go
git commit -m "feat(browse): connect-time SSRF guard — enumerated ranges, DNS-rebinding-proof dialer"
```
