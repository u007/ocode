package tailscale

import "testing"

func TestSanitizePath(t *testing.T) {
	if got := SanitizePath("ses_abc-123_X"); got != "/ses_abc-123_X" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizePath("a/b.c"); got != "/abc" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizePath(""); got != "/ocode" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizePath("desktop"); got != "/desktop" {
		t.Fatalf("got %q", got)
	}
}

func TestURLWithPathPrefixReplacesStalePrefix(t *testing.T) {
	got := URLWithPathPrefix("https://host.ts.net/stale", "/ours")
	if got != "https://host.ts.net/ours" {
		t.Fatalf("got %q", got)
	}
	if got := URLWithPathPrefix("", "/ours"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := URLWithPathPrefix("https://host.ts.net", ""); got != "https://host.ts.net" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildSessionURL(t *testing.T) {
	got := BuildSessionURL("https://host.ts.net/desktop/", "ses_1", "tok")
	if got != "https://host.ts.net/desktop/session/ses_1?token=tok" {
		t.Fatalf("got %q", got)
	}
}
