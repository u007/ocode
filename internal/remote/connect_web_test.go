package remote

import (
	"net"
	"strconv"
	"testing"
	"time"
)

// TestConnectWebRequiresResolvedPath mirrors Connect's own internal-error
// guard — cheap to test without any transport at all.
func TestConnectWebRequiresResolvedPath(t *testing.T) {
	err := ConnectWeb(ConnectOptions{Target: Target{Kind: KindSSH, Host: "h"}})
	if err == nil {
		t.Fatal("expected error for empty Path")
	}
}

func withShrunkTunnelReadyTimings(t *testing.T, attempts int, interval time.Duration) {
	t.Helper()
	origAttempts, origInterval := tunnelReadyAttempts, tunnelReadyInterval
	tunnelReadyAttempts, tunnelReadyInterval = attempts, interval
	t.Cleanup(func() {
		tunnelReadyAttempts, tunnelReadyInterval = origAttempts, origInterval
	})
}

func TestWaitForTunnelReadySucceedsImmediatelyWhenPortIsOpen(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test listener: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	withShrunkTunnelReadyTimings(t, 25, 10*time.Millisecond)
	if err := waitForTunnelReady(port); err != nil {
		t.Fatalf("expected success dialing an open port, got: %v", err)
	}
}

func TestWaitForTunnelReadyFailsAfterExhaustingAttempts(t *testing.T) {
	// A free port that nothing is listening on — dial should fail every
	// attempt and waitForTunnelReady must report that, bounded in time by
	// the shrunk attempts/interval below rather than the real 5s default.
	freePort, err := FreeLocalPort()
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}

	withShrunkTunnelReadyTimings(t, 3, 5*time.Millisecond)
	if err := waitForTunnelReady(freePort); err == nil {
		t.Fatal("expected an error dialing a port nothing is listening on")
	}
}

func TestWaitForTunnelReadySucceedsOncePortOpensPartwayThroughPolling(t *testing.T) {
	freePort, err := FreeLocalPort()
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}

	// Start listening on the same port only after a short delay, modeling a
	// tunnel that takes a couple of polling intervals to establish.
	go func() {
		time.Sleep(20 * time.Millisecond)
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort)))
		if err != nil {
			return
		}
		defer ln.Close()
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	withShrunkTunnelReadyTimings(t, 25, 10*time.Millisecond)
	if err := waitForTunnelReady(freePort); err != nil {
		t.Fatalf("expected success once the listener came up, got: %v", err)
	}
}
