package vault

import (
	"errors"
	"testing"
)

func TestMatchForURLHostAndPath(t *testing.T) {
	v, _ := newTestVault(t)
	mustInit(t, v, "master")
	mustCreate(t, v, Item{Site: "Generic", URL: "https://example.com/", Username: "g"})
	mustCreate(t, v, Item{Site: "Portal", URL: "https://example.com/portal", Username: "p"})
	mustCreate(t, v, Item{Site: "Sub", URL: "https://sub.example.com/", Username: "s"})

	// A path prefix outranks a bare host match, and a parent domain is
	// included for a subdomain request.
	got, err := v.MatchForURL("https://api.example.com/portal/login")
	if err != nil {
		t.Fatalf("MatchForURL: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("MatchForURL returned no matches, want the host's items")
	}
	if got[0].Site != "Portal" {
		t.Fatalf("first match = %q, want Portal (path prefix ranks first): %+v", got[0].Site, got)
	}
	wantSites := map[string]bool{"Portal": true, "Generic": true}
	for _, m := range got {
		delete(wantSites, m.Site)
	}
	if len(wantSites) != 0 {
		t.Fatalf("missing parent-domain items %v in %+v", wantSites, got)
	}

	// An exact subdomain match is included too.
	sub, err := v.MatchForURL("https://sub.example.com/x")
	if err != nil {
		t.Fatalf("MatchForURL(sub): %v", err)
	}
	if len(sub) == 0 || sub[0].Site != "Sub" {
		t.Fatalf("subdomain match = %+v, want Sub first", sub)
	}
}

func TestMatchForURLNoMatch(t *testing.T) {
	v, _ := newTestVault(t)
	mustInit(t, v, "master")
	mustCreate(t, v, Item{Site: "Other", URL: "https://other.test/", Username: "o"})

	got, err := v.MatchForURL("https://example.com/")
	if err != nil {
		t.Fatalf("MatchForURL: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("MatchForURL = %#v, want empty non-nil slice", got)
	}

	// Malformed / hostless input is an empty result, not an error.
	if got, err := v.MatchForURL("not a url"); err != nil || len(got) != 0 {
		t.Fatalf("MatchForURL(malformed) = (%+v, %v), want empty, nil", got, err)
	}
}

func TestMatchForURLRequiresUnlocked(t *testing.T) {
	v, _ := newTestVault(t)
	if _, err := v.MatchForURL("https://example.com/"); !errors.Is(err, ErrLocked) {
		t.Fatalf("MatchForURL on locked vault = %v, want ErrLocked", err)
	}
}
