package network

import (
	"net"
	"testing"
)

func mustIP(s string) net.IP { return net.ParseIP(s).To4() }

func TestPickBestIPPrefersLANOverTailscale(t *testing.T) {
	got := pickBestIP([]ifaceIP{
		{IP: mustIP("100.101.102.103")},
		{IP: mustIP("192.168.1.5")},
		{IP: mustIP("127.0.0.1")},
	})
	if got == nil || got.String() != "192.168.1.5" {
		t.Fatalf("got %v, want 192.168.1.5", got)
	}
}

func TestPickBestIPSkipsLoopbackAndLinkLocal(t *testing.T) {
	got := pickBestIP([]ifaceIP{
		{IP: mustIP("127.0.0.1")},
		{IP: mustIP("169.254.10.20")},
		{IP: mustIP("10.0.0.8")},
	})
	if got == nil || got.String() != "10.0.0.8" {
		t.Fatalf("got %v, want 10.0.0.8", got)
	}
}

func TestPickBestIPDeprioritizesVirtual(t *testing.T) {
	got := pickBestIP([]ifaceIP{
		{IP: mustIP("172.17.0.1"), Virtual: true},
		{IP: mustIP("192.168.1.9"), Virtual: false},
	})
	if got == nil || got.String() != "192.168.1.9" {
		t.Fatalf("got %v, want 192.168.1.9", got)
	}
}

func TestPickBestIPHandlesIPAddrForms(t *testing.T) {
	// collectInterfaceIPs handles *net.IPAddr as well as *net.IPNet; the
	// ranker itself only sees net.IP, so this guards the preference order
	// when only virtual + tailscale candidates exist.
	got := pickBestIP([]ifaceIP{
		{IP: mustIP("172.17.0.1"), Virtual: true},
		{IP: mustIP("100.64.0.1")},
	})
	if got == nil || got.String() != "172.17.0.1" {
		t.Fatalf("got %v, want 172.17.0.1", got)
	}
}

func TestIsShareableIPRejectsTailscale(t *testing.T) {
	if isShareableIP(mustIP("100.101.102.103")) {
		t.Fatal("tailscale CGNAT must not be shareable as LAN IP")
	}
	if !isShareableIP(mustIP("192.168.1.5")) {
		t.Fatal("LAN IP must be shareable")
	}
}

func TestIsVirtualInterface(t *testing.T) {
	for _, n := range []string{"docker0", "vethabc123", "br-1234", "virbr0"} {
		if !isVirtualInterface(n) {
			t.Fatalf("%s should be virtual", n)
		}
	}
	if isVirtualInterface("en0") || isVirtualInterface("eth0") {
		t.Fatal("en0/eth0 must not be virtual")
	}
}
