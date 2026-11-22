package tool

import (
	"net/url"
	"strings"
	"testing"
)

// TestBrowserURLDenyReasonRefusesMetadataForms pins the normalised refusal:
// every encoding of the metadata address and of link-local space is refused.
func TestBrowserURLDenyReasonRefusesMetadataForms(t *testing.T) {
	refuse := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://2852039166/",               // decimal 169.254.169.254
		"http://0xa9fea9fe/",               // hex
		"http://0XA9FEA9FE/",               // hex, upper case
		"http://0251.0376.0251.0376/",      // octal dotted
		"http://169.254.43518/",            // short form: 169.254 . (169*256+254 in last 2 bytes)
		"http://169.16689662/",             // 169 . (254.169.254 packed into 3 bytes) form
		"http://[::ffff:169.254.169.254]/", // IPv4-mapped dotted
		"http://[::ffff:a9fe:a9fe]/",       // IPv4-mapped hex
		"http://[fe80::1]/",                // IPv6 link-local
		"http://169.254.1.1/",              // link-local, not the metadata host
		"http://100.100.100.200/",          // Alibaba metadata
		"http://[fd00:ec2::254]/",          // AWS IPv6 metadata
		"http://metadata.google.internal/",
		"http://Metadata.Google.Internal/",
		"http://metadata.google.internal./",  // trailing dot
		"http://x.metadata.google.internal/", // subdomain form
		"http://2852039167/",                 // 169.254.169.255: still link-local
	}
	for _, u := range refuse {
		if reason := BrowserURLDenyReason(u); reason == "" {
			t.Errorf("BrowserURLDenyReason(%q) allowed; want refusal", u)
		}
	}
}

// TestBrowserURLDenyReasonSchemesAndHosts pins the other refusals and the cases
// that must stay allowed.
func TestBrowserURLDenyReasonSchemesAndAllowed(t *testing.T) {
	refuse := []string{"file:///etc/passwd", "chrome://settings", "javascript:alert(1)", "data:text/html,hi", "ftp://x/"}
	for _, u := range refuse {
		if BrowserURLDenyReason(u) == "" {
			t.Errorf("BrowserURLDenyReason(%q) allowed; want scheme refusal", u)
		}
	}
	allow := []string{
		"https://example.com/", "http://localhost:3000/", "http://127.0.0.1:8080/",
		"http://192.168.1.10/", "http://10.0.0.5/", "http://8.8.8.8/", "http://0/", "",
		"http://169.253.1.1/",
	}
	for _, u := range allow {
		if r := BrowserURLDenyReason(u); r != "" {
			t.Errorf("BrowserURLDenyReason(%q) = %q; want allowed", u, r)
		}
	}
}

// TestBoxBrowserURLDenyReason pins the upstash box refusal: loopback, private,
// CGNAT, link-local, unspecified, localhost names and every IP literal form are
// refused, while public hosts and the shared BrowserURLDenyReason refusals hold.
func TestBoxBrowserURLDenyReason(t *testing.T) {
	refuse := []string{
		// localhost names, with case and a trailing dot
		"http://localhost/", "https://LOCALHOST:8080/x", "http://localhost./", "http://a.localhost/", "http://x.y.localhost/",
		// loopback in every literal form
		"http://127.0.0.1:9222/json/version", "http://127.1/", "http://2130706433/", "http://0x7f000001/", "http://0177.0.0.1/",
		"http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://[::7f00:1]/",
		// private: RFC 1918, ULA
		"http://10.0.0.1/", "http://172.16.0.1/", "http://172.31.255.255/", "http://192.168.1.1/", "http://[fc00::1]/", "http://[fd12:3456::1]/",
		// CGNAT 100.64.0.0/10
		"http://100.64.0.1/", "http://100.127.255.255/",
		// link-local and unspecified
		"http://169.254.1.1/", "http://[fe80::1]/", "http://[fe80::1%25eth0]/", "http://0.0.0.0/", "http://0/", "http://[::]/", "http://0.1.2.3/",
		// IPv4-compatible IPv6 of a refused address
		"http://[::127.0.0.1]/",
		// Unicode hosts that a browser maps to a refused address: full stops, digits and
		// letters in their compatibility forms
		"http://127。0。0。1/", "http://①②⑦.0.0.1/", "http://１２７．０．０．１/", "http://ｌｏｃａｌｈｏｓｔ/", "http://127．0．0．1./",
		// NAT64 and 6to4 prefixes, refused whole
		"http://[64:ff9b::7f00:1]/", "http://[64:ff9b::a9fe:a9fe]/", "http://[2002:7f00:1::]/", "http://[2002:c0a8:101::1]/",
		// shared refusals still apply (scheme, metadata), including the mapped forms of them
		"file:///etc/passwd", "http://metadata.google.internal/", "http://169.254.169.254/",
		"http://１６９．２５４．１６９．２５４/", "http://169。254。169。254/",
	}
	for _, u := range refuse {
		if BoxBrowserURLDenyReason(u) == "" {
			t.Errorf("BoxBrowserURLDenyReason(%q) allowed; want a refusal", u)
		}
	}
	allow := []string{
		"https://example.com/", "http://example.com", "https://localhostx.com/", "https://notlocalhost/",
		"http://8.8.8.8/", "http://100.128.0.1/", "http://172.32.0.1/", "http://11.0.0.1/",
		"http://[2606:4700:4700::1111]/", "https://www.example.org:443/path?q=1#frag",
		// a punycode host and a Unicode host that maps to a public name, and ASCII
		// names with an underscore or a fullwidth spelling of a public name
		"https://xn--bcher-kva.example/", "https://bücher.example/", "https://a_b.example.com/",
		"https://r3---sn-abc.googlevideo.com/", "https://ｅｘａｍｐｌｅ.com/",
	}
	for _, u := range allow {
		if r := BoxBrowserURLDenyReason(u); r != "" {
			t.Errorf("BoxBrowserURLDenyReason(%q) = %q; want allowed", u, r)
		}
	}
}

// TestBrowserURLDenyReasonAllowsPublicUnicodeHosts pins that the local check allows
// the same public names. A punycode host and a Unicode host that maps to one both
// pass, and so does an ASCII name with an underscore.
func TestBrowserURLDenyReasonAllowsPublicUnicodeHosts(t *testing.T) {
	for _, u := range []string{"https://xn--bcher-kva.example/", "https://bücher.example/", "https://a_b.example.com/"} {
		if r := BrowserURLDenyReason(u); r != "" {
			t.Errorf("BrowserURLDenyReason(%q) = %q; want allowed", u, r)
		}
	}
}

// TestBrowserHostnameMapsUnicodeHostsLikeABrowser pins the mapping itself: each
// Unicode spelling maps to the ASCII host a browser resolves. Every input must parse
// as a URL, or the mapping is never reached.
func TestBrowserHostnameMapsUnicodeHostsLikeABrowser(t *testing.T) {
	cases := map[string]string{
		"http://127。0。0。1/":         "127.0.0.1",
		"http://①②⑦.0.0.1/":         "127.0.0.1",
		"http://ｌｏｃａｌｈｏｓｔ/":         "localhost",
		"http://127．0．0．1./":        "127.0.0.1",
		"https://bücher.example/":   "xn--bcher-kva.example",
		"https://example.com./":     "example.com",
		"https://a_b.example.com/":  "a_b.example.com",
		"https://xn--bcher-kva.ex/": "xn--bcher-kva.ex",
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v; the mapping is not reached", raw, err)
		}
		got, ok := browserHostname(u)
		if !ok || got != want {
			t.Errorf("browserHostname(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	// A name that does not map at all is refused, not passed through unmapped.
	u, err := url.Parse("http://a�.example/")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if got, ok := browserHostname(u); ok {
		t.Errorf("browserHostname of an unmappable name = %q, true; want a refusal", got)
	}
	if r := BrowserURLDenyReason("http://a�.example/"); r == "" {
		t.Error("an unmappable host is allowed; want a refusal")
	}
}

// TestBrowserURLDenyReasonWordingIsOneFormat pins the refusal wording: every reason
// is a complete sentence that begins "browser may" (or the box's "browser on the
// upstash box may"), and none doubles the "browser" prefix a caller adds.
func TestBrowserURLDenyReasonWordingIsOneFormat(t *testing.T) {
	local := []string{"file:///etc/passwd", "http://169.254.169.254/", "http://a�.example/", "http:///path", "http://[::1"}
	for _, u := range local {
		r := BrowserURLDenyReason(u)
		if r == "" {
			t.Errorf("BrowserURLDenyReason(%q) allowed; want a refusal", u)
			continue
		}
		if !strings.HasPrefix(r, "browser may ") || strings.Contains(r, "browser browser") || strings.Contains(r, "browser:") {
			t.Errorf("BrowserURLDenyReason(%q) = %q; want one \"browser may ...\" sentence", u, r)
		}
	}
	box := []string{"http://127.0.0.1/", "http://[64:ff9b::7f00:1]/", "http://localhost/"}
	for _, u := range box {
		r := BoxBrowserURLDenyReason(u)
		if r == "" {
			t.Errorf("BoxBrowserURLDenyReason(%q) allowed; want a refusal", u)
			continue
		}
		if !strings.HasPrefix(r, "browser on the upstash box may ") || strings.Contains(r, "browser: ") {
			t.Errorf("BoxBrowserURLDenyReason(%q) = %q; want one \"browser on the upstash box may ...\" sentence", u, r)
		}
	}
}

// TestBoxBrowserURLDenyReasonIsStricterThanLocal checks the box check refuses the
// loopback and private URLs the local check still allows.
func TestBoxBrowserURLDenyReasonIsStricterThanLocal(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1/", "http://192.168.0.5/", "http://[::1]/"} {
		if BrowserURLDenyReason(u) != "" {
			t.Fatalf("local check refuses %q; this test needs the local check to allow it", u)
		}
		if BoxBrowserURLDenyReason(u) == "" {
			t.Errorf("box check allows %q; want a refusal", u)
		}
	}
}
