package tui

import "testing"

func TestSanitizeTailscalePath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "abc123", "/abc123"},
		{"with dash and underscore", "session-id_42", "/session-id_42"},
		{"strip slash separator", "a/b/c", "/abc"},
		{"strip dot", "v1.0.0", "/v100"},
		{"empty falls back", "", "/ocode"},
		{"only special chars falls back", "/./..", "/ocode"},
		{"uppercase preserved", "SessionID", "/SessionID"},
		{"unicode stripped", "sessioné", "/session"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeTailscalePath(c.in)
			if got != c.want {
				t.Errorf("sanitizeTailscalePath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
