package tts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKyutaiPython writes a venv whose interpreter records the script handed to
// `-c` and exits with exitCode. It returns the venv root and the record path.
func fakeKyutaiPython(t *testing.T, exitCode int) (string, string) {
	t.Helper()
	venv := t.TempDir()
	if err := os.MkdirAll(filepath.Join(venv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(venv, "script.py")
	fake := fmt.Sprintf(`#!/bin/sh
[ "$1" = "-c" ] || exit 2
printf '%%s' "$2" > %s
exit %d
`, record, exitCode)
	if err := os.WriteFile(filepath.Join(venv, "bin", "python"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	return venv, record
}

// The resident worker runs with HF_HUB_OFFLINE=1, so every voice it may be asked
// for must be prefetched at install time. A voice the prefetch skips fails its
// first utterance offline.
func TestKyutaiPrefetchLoadsEveryVoice(t *testing.T) {
	venv, record := fakeKyutaiPython(t, 0)
	if _, err := kyutaiVerifyRuntime(context.Background(), venv, kyutaiVoices); err != nil {
		t.Fatalf("kyutaiVerifyRuntime: %v", err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read recorded script: %v", err)
	}
	script := string(data)
	for _, voice := range kyutaiVoices {
		if !strings.Contains(script, fmt.Sprintf("%q", voice)) {
			t.Errorf("prefetch script does not load voice %q:\n%s", voice, script)
		}
	}
	if !strings.Contains(script, "model.get_state_for_audio_prompt(voice)") {
		t.Errorf("prefetch script does not load voice prompts:\n%s", script)
	}
}

// A failed prefetch must surface as an install error, never as a silent skip.
func TestKyutaiPrefetchFailureIsReported(t *testing.T) {
	venv, _ := fakeKyutaiPython(t, 1)
	if _, err := kyutaiVerifyRuntime(context.Background(), venv, kyutaiVoices); err == nil {
		t.Fatal("kyutaiVerifyRuntime returned nil for a failing prefetch")
	}
}

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
