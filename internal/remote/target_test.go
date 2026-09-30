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

func TestTargetValidatePort(t *testing.T) {
	if err := (Target{Kind: KindSSH, Host: "h", Port: 2222}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Target{Kind: KindSSH, Host: "h", Port: 65536}).Validate(); err == nil {
		t.Fatal("expected invalid port")
	}
	if err := (Target{Kind: KindWSL, Distro: "Ubuntu", Port: 22}).Validate(); err == nil {
		t.Fatal("expected WSL port rejection")
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

// A target string is passed to ssh as a bare argv element with no "--"
// separator, so a host beginning with "-" is parsed by ssh as an OPTION rather
// than a hostname. ParseTarget only rejected "/", whitespace and a leading "@",
// which left single-token option payloads reachable. Verified against real ssh:
// `ssh -oBatchMode=yes '-oProxyCommand=id>~/marker' 127.0.0.1` runs `id` and
// creates the marker, because ssh consumes the injected -o and executes its
// ProxyCommand through a shell.
//
// This is reachable from an authenticated caller via POST /api/projects
// (AddRemote) and POST /api/projects/duplicate: the target is PERSISTED, so one
// request plants a payload that re-fires on every later connect and git call.
// ParseTarget must reject it.
func TestParseTargetRejectsLeadingDashHost(t *testing.T) {
	for _, in := range []string{
		"-oProxyCommand=id",
		"-F/tmp/evil.conf",
		"-o",
		"--",
		"user@-oProxyCommand=id",
		"-J",
	} {
		if got, err := ParseTarget(in); err == nil {
			t.Errorf("ParseTarget(%q) = %+v, want error: ssh would parse this as an option", in, got)
		}
	}
}

// Validate must reject it too, and independently of ParseTarget: callers that
// build a Target field-by-field from a request body (HandleUpdateProject at
// handler_projects.go:402) never go through ParseTarget, so a guard in
// ParseTarget alone would leave that path open.
func TestTargetValidateRejectsLeadingDash(t *testing.T) {
	for _, tgt := range []Target{
		{Kind: KindSSH, Host: "-oProxyCommand=id"},
		{Kind: KindSSH, User: "u", Host: "-F/tmp/evil.conf"},
	} {
		if err := tgt.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want error: host is passed to ssh unseparated", tgt)
		}
	}
}

// The rejection must not be over-broad: ordinary targets, IPv6 literals
// (colons are legal in a host) and hosts merely CONTAINING a dash mid-string
// all have to keep working.
func TestTargetValidateAllowsLegitimateDashes(t *testing.T) {
	for _, tgt := range []Target{
		{Kind: KindSSH, Host: "host"},
		{Kind: KindSSH, User: "u", Host: "my-host.example.com"},
		{Kind: KindSSH, User: "u", Host: "a-b-c.d-e"},
		{Kind: KindSSH, Host: "fe80::1"},
		{Kind: KindSSH, Host: "host", Port: 2222},
	} {
		if err := tgt.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", tgt, err)
		}
	}
}
