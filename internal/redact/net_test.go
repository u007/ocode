package redact

import (
	"testing"
)

func TestNetHookDisabled(t *testing.T) {
	nh := NetHookDisabled()
	if nh.Enabled {
		t.Error("NetHook should be disabled")
	}

	text := "my password is AKIAIOSFODNN7EXAMPLE"
	result := nh.ScanText(text)
	if result != text {
		t.Error("Disabled NetHook should not modify text")
	}
}

func TestNetHookEnabled(t *testing.T) {
	reg := NewRegistry("a3f9c2")
	nh := NetHookEnabled(reg)

	text := "my password is AKIAIOSFODNN7EXAMPLE"
	result := nh.ScanText(text)

	if result == text {
		t.Error("Enabled NetHook should have modified text")
	}

	// Check that the secret was registered
	entries := reg.All()
	if len(entries) == 0 {
		t.Error("Expected at least one registered secret")
	}
}

func TestNetHookTripwire(t *testing.T) {
	reg := NewRegistry("a3f9c2")
	tripwireFired := false
	var firedKinds []string

	nh := &NetHook{
		Registry: reg,
		Enabled:  false, // Disabled - should fire tripwire
		OnTripwire: func(kinds []string) {
			tripwireFired = true
			firedKinds = kinds
		},
	}

	// Simulate detecting a secret
	nh.FireTripwire([]string{"aws_key"})

	if !tripwireFired {
		t.Error("Tripwire should have fired")
	}
	if len(firedKinds) != 1 || firedKinds[0] != "aws_key" {
		t.Errorf("Expected ['aws_key'], got %v", firedKinds)
	}

	// Fire again - should not fire tripwire a second time
	tripwireFired = false
	nh.FireTripwire([]string{"github_token"})
	if tripwireFired {
		t.Error("Tripwire should not fire twice")
	}
}

func TestNetHookResetTripwire(t *testing.T) {
	reg := NewRegistry("a3f9c2")
	callCount := 0

	nh := &NetHook{
		Registry: reg,
		Enabled:  false,
		OnTripwire: func(kinds []string) {
			callCount++
		},
	}

	nh.FireTripwire([]string{"test"})
	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	nh.ResetTripwire()
	nh.FireTripwire([]string{"test"})
	if callCount != 2 {
		t.Errorf("Expected 2 calls after reset, got %d", callCount)
	}
}

func TestNetHookRedactKnownFormats(t *testing.T) {
	reg := NewRegistry("a3f9c2")
	nh := NetHookEnabled(reg)

	text := "System prompt with AKIAIOSFODNN7EXAMPLE and ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234"
	result := nh.ScanText(text)

	// Both secrets should be redacted
	if result == text {
		t.Error("Text should have been redacted")
	}

	// Check that both secrets were registered
	entries := reg.All()
	if len(entries) != 2 {
		t.Errorf("Expected 2 registered secrets, got %d", len(entries))
	}

	// Verify we can resolve back
	resolved := reg.Resolve(result)
	if resolved != text {
		t.Errorf("Resolve mismatch:\n  original: %q\n  resolved: %q", text, resolved)
	}
}

func TestNetHookKeywordEntropyNotApplied(t *testing.T) {
	reg := NewRegistry("a3f9c2")
	nh := NetHookEnabled(reg)

	// Keyword+entropy should NOT be detected in net mode (file mode only)
	text := "password = AbC123456789012345678901234567890"
	result := nh.ScanText(text)

	// Should NOT be modified (keyword+entropy not detected in file mode)
	if result != text {
		t.Error("Keyword+entropy should not be detected in net mode")
	}
}
