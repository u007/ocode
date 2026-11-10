package remote

import (
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestValidateTunnelDescriptor(t *testing.T) {
	cases := []struct {
		name string
		t    *config.DBTunnel
		ok   bool
	}{
		{"nil", nil, false},
		{"empty host", &config.DBTunnel{Host: "", RemoteHost: "db", RemotePort: 5432}, false},
		{"empty remote host", &config.DBTunnel{Host: "bastion", RemoteHost: "", RemotePort: 5432}, false},
		{"zero remote port", &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 0}, false},
		{"bad remote port", &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 70000}, false},
		{"leading dash host", &config.DBTunnel{Host: "-evil", RemoteHost: "db", RemotePort: 5432}, false},
		{"remote host whitespace", &config.DBTunnel{Host: "bastion", RemoteHost: "db host", RemotePort: 5432}, false},
		{"remote host leading dash", &config.DBTunnel{Host: "bastion", RemoteHost: "-evil", RemotePort: 5432}, false},
		{"bad local port", &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, LocalPort: 70000}, false},
		{"relative identity", &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, IdentityFile: "relative/key"}, false},
		{"valid", &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432}, true},
		{"valid with identity", &config.DBTunnel{Host: "bastion", Port: 22, RemoteHost: "db", RemotePort: 5432, IdentityFile: "/keys/id_rsa", LocalPort: 12345}, true},
		{"ipv6 remote bracketed", &config.DBTunnel{Host: "bastion", RemoteHost: "[::1]", RemotePort: 5432}, true},
	}
	for _, c := range cases {
		err := ValidateTunnelDescriptor(c.t)
		if c.ok && err != nil {
			t.Errorf("%s: expected ok, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestTunnelKeyDistinct(t *testing.T) {
	a := &config.DBTunnel{Host: "h", RemoteHost: "r", RemotePort: 1, IdentityFile: "/k", LocalPort: 2}
	b := &config.DBTunnel{Host: "h", RemoteHost: "r", RemotePort: 1, IdentityFile: "/k2", LocalPort: 2}
	if tunnelKey("n", a) == tunnelKey("n", b) {
		t.Fatal("tunnelKey should distinguish identity files")
	}
}
