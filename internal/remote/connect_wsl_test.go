package remote

import (
	"runtime"
	"testing"
)

func TestNewTransportForTargetSelectsWSL(t *testing.T) {
	transport, err := newTransportForTarget(Target{Kind: KindWSL, Distro: "Ubuntu"}, nil)
	if runtime.GOOS != "windows" {
		if err == nil {
			t.Fatal("expected OS-gate error on non-Windows")
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wsl, ok := transport.(*WSLTransport)
	if !ok {
		t.Fatalf("expected *WSLTransport, got %T", transport)
	}
	if wsl.Distro != "Ubuntu" {
		t.Errorf("Distro = %q, want %q", wsl.Distro, "Ubuntu")
	}
}

func TestNewTransportForTargetSelectsSSH(t *testing.T) {
	transport, err := newTransportForTarget(Target{Kind: KindSSH, Host: "h"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := transport.(*SSHTransport); !ok {
		t.Fatalf("expected *SSHTransport, got %T", transport)
	}
}
