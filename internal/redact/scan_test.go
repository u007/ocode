package redact

import (
	"fmt"
	"strings"
	"testing"
)

// mockScanner is a test double for the Scanner interface.
type mockScanner struct {
	spans []Span
	err   error
}

func (m *mockScanner) Scan(maskedText string) ([]Span, error) {
	return m.spans, m.err
}

// TestScanAndMask_InPlaceSubstitution is the Phase 2 anchor test.
// It proves that ScanAndMask registers AND substitutes a novel secret
// value that the tier-1 Detect regex cannot match, without depending
// on the safety net's Detect pass.
func TestScanAndMask_InPlaceSubstitution(t *testing.T) {
	reg := NewRegistry(NewNonce())

	// A completely novel secret value — no known prefix, no keyword,
	// no entropy pattern that Detect would catch.
	novelSecret := "CUSTOM_NAME=xA7kQ9mBw2pL"
	content := "Here is the config: " + novelSecret + " and some trailing text."

	// Mock scanner that finds the value "xA7kQ9mBw2pL" at the expected span.
	// The value starts at character 32, ends at 44 (length 12).
	scanner := &mockScanner{
		spans: []Span{{Start: 32, End: 44, Kind: "custom"}},
	}

	masked, err := ScanAndMask(content, scanner, reg)
	if err != nil {
		t.Fatalf("ScanAndMask returned error: %v", err)
	}

	// The novel secret value must be masked — no raw value in output.
	if strings.Contains(masked, "xA7kQ9mBw2pL") {
		t.Errorf("novel secret not masked in output: %q", masked)
	}

	// The masked output should contain an OCSEC token.
	if !TokenPattern.MatchString(masked) {
		t.Errorf("expected OCSEC token in output, got: %q", masked)
	}

	// Verify the registry has the entry.
	entries := reg.All()
	found := false
	for _, e := range entries {
		if e.Value == "xA7kQ9mBw2pL" {
			found = true
			if e.Kind != "custom" {
				t.Errorf("expected kind=custom, got %q", e.Kind)
			}
			if e.Source != "scanner" {
				t.Errorf("expected source=scanner, got %q", e.Source)
			}
			break
		}
	}
	if !found {
		t.Error("novel secret not found in registry")
	}
}

// TestScanAndMask_NilScanner verifies no-op on nil scanner.
func TestScanAndMask_NilScanner(t *testing.T) {
	reg := NewRegistry(NewNonce())
	content := "some text"
	masked, err := ScanAndMask(content, nil, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if masked != content {
		t.Errorf("expected unchanged content, got %q", masked)
	}
}

// TestScanAndMask_NilRegistry verifies no-op on nil registry.
func TestScanAndMask_NilRegistry(t *testing.T) {
	scanner := &mockScanner{}
	content := "some text"
	masked, err := ScanAndMask(content, scanner, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if masked != content {
		t.Errorf("expected unchanged content, got %q", masked)
	}
}

// TestScanAndMask_EmptyContent verifies no-op on empty content.
func TestScanAndMask_EmptyContent(t *testing.T) {
	reg := NewRegistry(NewNonce())
	scanner := &mockScanner{}
	masked, err := ScanAndMask("", scanner, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if masked != "" {
		t.Errorf("expected empty output, got %q", masked)
	}
}

// TestScanAndMask_SkipsTokens verifies that already-registered tokens are
// not double-registered.
func TestScanAndMask_SkipsTokens(t *testing.T) {
	reg := NewRegistry(NewNonce())
	// Pre-register a value.
	reg.GetOrAssign("pre-existing", "api_key", "session")
	masked := reg.Substitute("pre-existing")

	// Scanner tries to report the token itself — should be skipped.
	scanner := &mockScanner{
		spans: []Span{{Start: 0, End: len(masked), Kind: "token"}},
	}

	result, err := ScanAndMask(masked, scanner, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != masked {
		t.Errorf("expected unchanged masked content, got %q", result)
	}
}

// TestScanAndMask_ScannerError verifies error propagation.
func TestScanAndMask_ScannerError(t *testing.T) {
	reg := NewRegistry(NewNonce())
	scanner := &mockScanner{err: fmt.Errorf("connection refused")}
	_, err := ScanAndMask("some text", scanner, reg)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestScanAndMask_BoundsChecking verifies out-of-bounds spans are skipped.
func TestScanAndMask_BoundsChecking(t *testing.T) {
	reg := NewRegistry(NewNonce())
	scanner := &mockScanner{
		spans: []Span{
			{Start: -1, End: 5, Kind: "bad"},  // negative start
			{Start: 0, End: 100, Kind: "bad"}, // end exceeds content
			{Start: 5, End: 5, Kind: "bad"},   // start == end (empty)
		},
	}
	content := "hello"
	masked, err := ScanAndMask(content, scanner, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No valid spans, so no tokens registered.
	if TokenPattern.MatchString(masked) {
		t.Errorf("unexpected token in output: %q", masked)
	}
}
