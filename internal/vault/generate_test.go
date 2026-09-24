package vault

import (
	"strings"
	"testing"
)

func TestGeneratePasswordRespectsOptions(t *testing.T) {
	pw := GeneratePassword(GenOptions{Length: 32, Upper: true, Digits: true, Symbols: true})
	if len(pw) != 32 {
		t.Fatalf("length = %d, want 32", len(pw))
	}
	if !strings.ContainsAny(pw, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("no uppercase in %q", pw)
	}
	if !strings.ContainsAny(pw, "0123456789") {
		t.Errorf("no digit in %q", pw)
	}
	if !strings.ContainsAny(pw, "!@#$%^&*") {
		t.Errorf("no symbol in %q", pw)
	}
}

func TestGeneratePasswordDefaultsLength(t *testing.T) {
	if got := len(GeneratePassword(GenOptions{})); got != 20 {
		t.Fatalf("default length = %d, want 20", got)
	}
}

func TestGeneratePasswordLowerOnlyHasNoSymbols(t *testing.T) {
	pw := GeneratePassword(GenOptions{Upper: false, Digits: false, Symbols: false})
	for _, r := range pw {
		if r < 'a' || r > 'z' {
			t.Fatalf("lower-only password contains %q in %q", r, pw)
		}
	}
}
