package tool

import "testing"

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
