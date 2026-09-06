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

func TestParseTargetWSL(t *testing.T) {
	cases := []struct {
		in         string
		wantDistro string
	}{
		{"wsl:Ubuntu", "Ubuntu"},
		{"wsl:", ""},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if err != nil {
			t.Fatalf("ParseTarget(%q): unexpected error: %v", c.in, err)
		}
		if got.Kind != KindWSL || got.Distro != c.wantDistro {
			t.Errorf("ParseTarget(%q) = %+v, want Kind=KindWSL Distro=%q", c.in, got, c.wantDistro)
		}
	}
}

func TestValidateTargetOS(t *testing.T) {
	if err := validateTargetOS(KindSSH, "linux"); err != nil {
		t.Errorf("ssh target should be valid on any OS, got %v", err)
	}
	if err := validateTargetOS(KindSSH, "windows"); err != nil {
		t.Errorf("ssh target should be valid on any OS, got %v", err)
	}
	if err := validateTargetOS(KindWSL, "windows"); err != nil {
		t.Errorf("wsl target should be valid on windows, got %v", err)
	}
	for _, goos := range []string{"linux", "darwin"} {
		if err := validateTargetOS(KindWSL, goos); err == nil {
			t.Errorf("wsl target should be rejected on %s", goos)
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
