package remote

import "testing"

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in       string
		wantUser string
		wantHost string
		wantErr  bool
	}{
		{"host", "", "host", false},
		{"user@host", "user", "host", false},
		{"user@sub.example.com", "user", "sub.example.com", false},
		{"", "", "", true},
		{"   ", "", "", true},
		{"user@", "", "", true},
		{"@host", "", "", true},
		{"has space", "", "", true},
		{"has/slash", "", "", true},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseTarget(%q): expected error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseTarget(%q): unexpected error: %v", c.in, err)
		}
		if got.Kind != KindSSH || got.User != c.wantUser || got.Host != c.wantHost {
			t.Errorf("ParseTarget(%q) = %+v, want user=%q host=%q", c.in, got, c.wantUser, c.wantHost)
		}
	}
}

func TestParseTargetWSLRejectedInPhase1(t *testing.T) {
	for _, in := range []string{"wsl:Ubuntu", "wsl:"} {
		if _, err := ParseTarget(in); err == nil {
			t.Errorf("ParseTarget(%q): expected phase-1 rejection error, got nil", in)
		}
	}
}

func TestTargetString(t *testing.T) {
	if got := (Target{Kind: KindSSH, Host: "h"}).String(); got != "h" {
		t.Errorf("got %q, want %q", got, "h")
	}
	if got := (Target{Kind: KindSSH, User: "u", Host: "h"}).String(); got != "u@h" {
		t.Errorf("got %q, want %q", got, "u@h")
	}
	if got := (Target{Kind: KindWSL, Distro: "Ubuntu"}).String(); got != "wsl:Ubuntu" {
		t.Errorf("got %q, want %q", got, "wsl:Ubuntu")
	}
}
