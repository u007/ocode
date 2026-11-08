package agent

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestNormalizeAllowedHost(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"example.com", "example.com"},
		{"  Example.COM  ", "example.com"},
		{"https://api.example.com", "api.example.com"},
		{"http://api.example.com:8080/v1/items?x=1#frag", "api.example.com"},
		{"api.example.com:443", "api.example.com"},
		{"user:pass@api.example.com", "api.example.com"},
		{"api.example.com/path/only", "api.example.com"},
		{"api.example.com.", "api.example.com"},
		{"*.Example.com", "*.example.com"},
		{"https://*.example.com:8443/x", "*.example.com"},
		{"[::1]:9090", "::1"},
		{"[::1]", "::1"},
		{"::1", "::1"},
		{"[::1", ""},
		{"", ""},
		{"   ", ""},
		{"*", ""},
		{"*.", ""},
		{"https://", ""},
		{"*.*.example.com", ""},
	}
	for _, c := range cases {
		if got := NormalizeAllowedHost(c.in); got != c.want {
			t.Errorf("NormalizeAllowedHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHostAllowedMatching(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetAllowedHosts([]string{"*.example.com", "api.test", "EXAMPLE.org", "https://docs.site:8443/guide"})
	cases := []struct {
		host string
		want bool
	}{
		{"api.example.com", true},
		{"a.b.example.com", true},
		{"API.EXAMPLE.COM", true},
		{"example.com", false},
		{"xexample.com", false},
		{"evil-example.com", false},
		{"api.example.com.evil.net", false},
		{"api.test", true},
		{"API.TEST", true},
		{"api.test:8443", true},
		{"api.test.evil.com", false},
		{"sub.api.test", false},
		{"example.org", true},
		{"docs.site", true},
		{"docs.site:9000", true},
		{"", false},
		{"*.example.com", false},
	}
	for _, c := range cases {
		if got := pm.HostAllowed(c.host); got != c.want {
			t.Errorf("HostAllowed(%q) = %t, want %t", c.host, got, c.want)
		}
	}
}

func TestSetAllowedHostsReplacesNormalisesAndSorts(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetAllowedHosts([]string{"old.test"})
	pm.SetAllowedHosts([]string{"https://Zed.test/x", "*.alpha.test", "", "zed.test", "*"})
	want := []string{"*.alpha.test", "zed.test"}
	if got := pm.AllowedHosts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedHosts() = %#v, want %#v", got, want)
	}
	if pm.HostAllowed("old.test") {
		t.Fatal("replace kept a host from the earlier list")
	}
}

func TestAllowedHostsNilSafe(t *testing.T) {
	var pm *PermissionManager
	pm.SetAllowedHosts([]string{"x.test"})
	if pm.HostAllowed("x.test") {
		t.Fatal("nil manager allowed a host")
	}
	if got := pm.AllowedHosts(); got != nil {
		t.Fatalf("nil manager AllowedHosts() = %#v", got)
	}
}

func TestAllowedHostsSurviveClone(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetAllowedHosts([]string{"clone.test"})
	clone := pm.Clone()
	if !clone.HostAllowed("clone.test") {
		t.Fatal("clone lost the allowed-hosts list")
	}
	pm.SetAllowedHosts(nil)
	if !clone.HostAllowed("clone.test") {
		t.Fatal("clone is not a snapshot: later edit to the source leaked in")
	}
}

func TestLoadFromOcodeAppliesAllowedHosts(t *testing.T) {
	pm := NewPermissionManager()
	pm.LoadFromOcode(config.PermissionConfig{AllowedHosts: []string{"Load.Test"}})
	if !pm.HostAllowed("load.test") {
		t.Fatal("LoadFromOcode did not apply allowed_hosts")
	}
}

func TestDecideWebfetchAllowedHostAllows(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	pm.SetAllowedHosts([]string{"*.example.com"})

	args := json.RawMessage(`{"url":"https://api.example.com/v1/items"}`)
	if dec := pm.Decide("webfetch", args); dec.Level != PermissionAllow {
		t.Fatalf("webfetch to allowed host = %s, want allow", dec.Level)
	}
	unlisted := json.RawMessage(`{"url":"https://unlisted.test/page"}`)
	if dec := pm.Decide("webfetch", unlisted); dec.Level != PermissionAsk {
		t.Fatalf("webfetch to unlisted host = %s, want ask", dec.Level)
	}
}

// TestDecideWebfetchAllowedHostBeatsSessionDeny pins that the persisted list
// is checked before the in-session domain cache: a cached deny for a listed
// host does not win.
func TestDecideWebfetchAllowedHostBeatsSessionDeny(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	pm.SetWebfetchDomain("api.test", PermissionDeny)
	args := json.RawMessage(`{"url":"https://api.test/x"}`)
	if dec := pm.Decide("webfetch", args); dec.Level != PermissionDeny {
		t.Fatalf("precondition: cached deny = %s, want deny", dec.Level)
	}
	pm.SetAllowedHosts([]string{"api.test"})
	if dec := pm.Decide("webfetch", args); dec.Level != PermissionAllow {
		t.Fatalf("listed host with cached deny = %s, want allow", dec.Level)
	}
}

func TestDecideBrowserNavigateAndOpenAllowedHost(t *testing.T) {
	for _, action := range []string{"navigate", "open"} {
		pm := NewPermissionManager()
		pm.SetAllowedHosts([]string{"app.example.com"})
		args := json.RawMessage(`{"action":"` + action + `","url":"https://app.example.com/dashboard"}`)
		if dec := pm.Decide("browser", args); dec.Level != PermissionAllow {
			t.Errorf("browser %s to allowed host = %s, want allow", action, dec.Level)
		}
		unlisted := json.RawMessage(`{"action":"` + action + `","url":"https://other.test/"}`)
		if dec := pm.Decide("browser", unlisted); dec.Level == PermissionAllow {
			t.Errorf("browser %s to unlisted host = allow, want not allow", action)
		}
	}
}

// TestDecideBrowserExplicitToolDenyWinsOverAllowedHost pins that a tools.browser
// deny rule still applies to a listed host.
func TestDecideBrowserExplicitToolDenyWinsOverAllowedHost(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetAllowedHosts([]string{"app.example.com"})
	pm.SetRule("browser", PermissionDeny)
	args := json.RawMessage(`{"action":"navigate","url":"https://app.example.com/"}`)
	if dec := pm.Decide("browser", args); dec.Level != PermissionDeny {
		t.Fatalf("browser deny rule with listed host = %s, want deny", dec.Level)
	}
}

// TestDecideBrowserMetadataStaysRefusedWhenListed pins that the hard refusal
// for a metadata address runs before the allowed-hosts check, so listing the
// address does not open it in any mode.
func TestDecideBrowserMetadataStaysRefusedWhenListed(t *testing.T) {
	for _, mode := range []PermissionMode{PermissionModeNormal, PermissionModeYOLO} {
		pm := NewPermissionManager()
		pm.SetMode(mode)
		pm.SetAllowedHosts([]string{"169.254.169.254"})
		args := json.RawMessage(`{"action":"navigate","url":"http://169.254.169.254/"}`)
		dec := pm.Decide("browser", args)
		if dec.Level != PermissionDeny || !dec.HardDeny {
			t.Errorf("mode=%s metadata listed = %s hard=%t, want hard deny", mode, dec.Level, dec.HardDeny)
		}
	}
}

// TestNewAgentFromConfigAllowsListedHost pins that a session built from a config
// that already lists a host allows it from its first call, with no PUT needed.
// It goes through NewAgent, so it covers the construction path, not only the
// loader called directly.
func TestNewAgentFromConfigAllowsListedHost(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.AllowedHosts = []string{"Fresh.Example.com"}
	a := NewAgent(nil, nil, cfg, nil)
	if !a.Permissions().HostAllowed("fresh.example.com") {
		t.Fatalf("fresh agent did not load allowed_hosts: %#v", a.Permissions().AllowedHosts())
	}
	args := json.RawMessage(`{"url":"https://fresh.example.com/page"}`)
	if dec := a.Permissions().Decide("webfetch", args); dec.Level != PermissionAllow {
		t.Fatalf("webfetch on a fresh agent = %s, want allow", dec.Level)
	}
}

// TestDecideWebfetchExplicitDenyBeatsAllowedHost pins that an explicit
// tools.webfetch deny wins over the allowed-hosts list: a listed host is not
// allowed while the tool rule says deny.
func TestDecideWebfetchExplicitDenyBeatsAllowedHost(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	pm.SetAllowedHosts([]string{"api.test"})
	pm.SetRule("webfetch", PermissionDeny)
	args := json.RawMessage(`{"url":"https://api.test/x"}`)
	if dec := pm.Decide("webfetch", args); dec.Level == PermissionAllow {
		t.Fatalf("listed host under tools.webfetch deny = %s, want not allow", dec.Level)
	}
}
