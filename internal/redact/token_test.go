package redact

import (
	"testing"
)

func TestFormatToken(t *testing.T) {
	token := FormatToken("a3f9c2", 1)
	expected := "[[OCSEC:a3f9c2:1]]"
	if token != expected {
		t.Errorf("FormatToken = %q, want %q", token, expected)
	}
}

func TestTokenPattern(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		shouldMatch bool
	}{
		{"valid token", "[[OCSEC:a3f9c2:1]]", true},
		{"valid token with higher index", "[[OCSEC:a3f9c2:42]]", true},
		{"wrong length nonce (5 chars)", "[[OCSEC:a3f9c:1]]", false},
		{"wrong length nonce (7 chars)", "[[OCSEC:a3f9c2d:1]]", false},
		{"non-hex nonce", "[[OCSEC:a3f9cg:1]]", false},
		{"uppercase nonce", "[[OCSEC:A3F9C2:1]]", false},
		{"missing closing brackets", "[[OCSEC:a3f9c2:1]", false},
		{"missing opening brackets", "OCSEC:a3f9c2:1]]", false},
		{"empty text", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := TokenPattern.FindAllString(tt.text, -1)
			matched := len(matches) > 0
			if matched != tt.shouldMatch {
				t.Errorf("TokenPattern.Match(%q) = %v, want %v", tt.text, matched, tt.shouldMatch)
			}
			if matched && matches[0] != tt.text {
				t.Errorf("TokenPattern.Match(%q) captured %q, want exact match", tt.text, matches[0])
			}
		})
	}
}

func TestTokensForNonce(t *testing.T) {
	nonce := "a3f9c2"
	otherNonce := "b4e8d1"

	text := "Here is a token [[OCSEC:a3f9c2:1]] and another [[OCSEC:a3f9c2:2]] and a foreign one [[OCSEC:b4e8d1:1]]"
	tokens, indexes := TokensForNonce(text, nonce)

	if len(tokens) != 2 {
		t.Errorf("TokensForNonce returned %d tokens, want 2", len(tokens))
	}
	if tokens[0] != "[[OCSEC:a3f9c2:1]]" {
		t.Errorf("first token = %q, want [[OCSEC:a3f9c2:1]]", tokens[0])
	}
	if tokens[1] != "[[OCSEC:a3f9c2:2]]" {
		t.Errorf("second token = %q, want [[OCSEC:a3f9c2:2]]", tokens[1])
	}
	if indexes[0] != 1 {
		t.Errorf("first index = %d, want 1", indexes[0])
	}
	if indexes[1] != 2 {
		t.Errorf("second index = %d, want 2", indexes[1])
	}

	// Test with other nonce - should return empty
	tokens2, _ := TokensForNonce(text, otherNonce)
	if len(tokens2) != 1 || tokens2[0] != "[[OCSEC:b4e8d1:1]]" {
		t.Errorf("TokensForNonce with other nonce = %v, want [[OCSEC:b4e8d1:1]]", tokens2)
	}

	// Test with non-existent nonce
	tokens3, _ := TokensForNonce(text, "deadbe")
	if len(tokens3) != 0 {
		t.Errorf("TokensForNonce with non-existent nonce = %v, want empty", tokens3)
	}
}

func TestNewNonce(t *testing.T) {
	nonce := NewNonce()
	if len(nonce) != 6 {
		t.Errorf("NewNonce length = %d, want 6", len(nonce))
	}
	// Verify it's valid hex
	for _, c := range nonce {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("NewNonce contains non-hex char: %c", c)
		}
	}
	// Should be different each time
	nonce2 := NewNonce()
	if nonce == nonce2 {
		t.Error("NewNonce returned same value twice")
	}
}