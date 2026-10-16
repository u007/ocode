package tts

import (
	"strings"
	"testing"
)

// TestKyutaiManifestRegisteredAndCarriesLicenseDisclosure pins Kyutai Pocket TTS
// (the engine the users increasingly ask about) to the same disclosure contract
// as every other local server engine: a manifest, an authoritative license
// name/text/hash, a non-empty license URL that the operator can hand to the
// user, and a usable runtime on this host.
func TestKyutaiManifestRegisteredAndCarriesLicenseDisclosure(t *testing.T) {
	m, ok := ManifestFor(EngineKyutai)
	if !ok {
		t.Fatal("kyutai manifest is not registered")
	}
	if m.Engine != EngineKyutai {
		t.Fatalf("manifest engine mismatch: %q", m.Engine)
	}
	if m.LicenseName == "" || m.LicenseText == "" || m.LicenseHash() == "" || m.LicenseURL == "" {
		t.Fatalf("kyutai must carry authoritative license disclosure (name/text/hash/url): %#v", m)
	}
	if !strings.HasPrefix(strings.ToLower(m.LicenseURL), "https://") {
		t.Fatalf("kyutai license URL is not an https link: %q", m.LicenseURL)
	}
	// The Pocket TTS core is MIT, but the disclosure must not hide that the
	// voice weights have their own, per-voice licenses.
	if !strings.Contains(m.LicenseText, "kyutai.org/pocket-tts") {
		t.Fatalf("kyutai license text must point at the voice license cards: %q", m.LicenseText)
	}
	rt, ok := m.Runtime[Host()]
	// Hosts with no pinned runtime (e.g. darwin/amd64) are allowed to be
	// unsupported, but any supported host must expose a range.
	if ok {
		if rt.MinPython == [2]int{0, 0} {
			t.Fatalf("kyutai runtime on %s has no python range", Host())
		}
		for _, req := range rt.Requirements {
			if !strings.Contains(req, "==") {
				t.Errorf("requirement %q is not pinned", req)
			}
		}
	}
}

// TestKyutaiCatalogEntryPropagatesLicenseLink guards the wiring that actually
// reaches the UI/client: the catalog Engine for kyutai must carry the same
// LicenseURL the manifest pins, so the settings form can render a clickable
// "view license" link.
func TestKyutaiCatalogEntryPropagatesLicenseLink(t *testing.T) {
	e, ok := EngineForHost(EngineKyutai)
	if !ok {
		t.Fatal("kyutai is missing from the catalog")
	}
	m, _ := ManifestFor(EngineKyutai)
	if e.LicenseURL != m.LicenseURL || e.LicenseURL == "" {
		t.Fatalf("catalog kyutai license URL = %q, manifest = %q (must be non-empty and propagated)", e.LicenseURL, m.LicenseURL)
	}
	if e.LicenseHash != m.LicenseHash() || e.LicenseName != m.LicenseName {
		t.Fatalf("catalog kyutai disclosure does not match manifest: catalog=%#v manifest=%#v", e, m)
	}
}
