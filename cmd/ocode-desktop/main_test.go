package main

import (
	"testing"

	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

func TestDesktopCodexPluginRegistered(t *testing.T) {
	plugin, ok := providerplugin.Get("openai")
	if !ok {
		t.Fatal("openai provider plugin not registered; desktop main.go is missing the blank import of internal/plugin/codex")
	}
	if !plugin.ModelAllowed("gpt-5.6-luna") {
		t.Error(`ModelAllowed("gpt-5.6-luna") = false; Codex routing would be skipped for this model`)
	}
}

func TestSessionIDFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"none", []string{"ocode-desktop"}, ""},
		{"short", []string{"ocode-desktop", "-session", "abc"}, "abc"},
		{"long", []string{"ocode-desktop", "--session", "xyz"}, "xyz"},
		{"missing value", []string{"ocode-desktop", "--session"}, ""},
		{"empty value", []string{"ocode-desktop", "--session", ""}, ""},
		{"later flag wins", []string{"ocode-desktop", "-session", "a", "-session", "b"}, "b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionIDFromArgs(tc.args); got != tc.want {
				t.Fatalf("sessionIDFromArgs(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestIsAllowedExternalURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"https with path query and fragment", "https://example.com/a?b=c#d", true},
		{"http localhost with port", "http://127.0.0.1:8080/x", true},
		{"uppercase scheme", "HTTPS://example.com", true},
		{"file scheme", "file:///etc/passwd", false},
		{"javascript scheme", "javascript:alert(1)", false},
		{"custom scheme (editor links)", "ocode-file://open?d=x", false},
		{"scheme with no host", "https://", false},
		{"relative path", "/session/abc", false},
		{"bare word", "example.com", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAllowedExternalURL(tc.raw); got != tc.want {
				t.Fatalf("isAllowedExternalURL(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestMsgOpenExternalPrefixIsNotWailsReserved(t *testing.T) {
	// The bridge only reaches RawMessageHandler for messages that do NOT start
	// with "wails:" (framework messages are consumed before it).
	if len(msgOpenExternal) >= 6 && msgOpenExternal[:6] == "wails:" {
		t.Fatalf("msgOpenExternal %q must not start with the reserved %q prefix", msgOpenExternal, "wails:")
	}
}
