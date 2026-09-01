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
