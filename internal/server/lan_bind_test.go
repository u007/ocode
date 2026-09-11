package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/u007/ocode/internal/network"
)

// TestAllInterfaceBindIsReachableViaLANIP proves the advertised share address
// connects: a server bound to 0.0.0.0 (the desktop boot posture, so Share
// dialog URLs work) must answer on the LAN IP returned by network.GetIP.
// A loopback-only bind would pass the 127.0.0.1 check below but fail the LAN
// check — which is exactly the bug this guards against.
func TestAllInterfaceBindIsReachableViaLANIP(t *testing.T) {
	srv := New("0.0.0.0:0", "", "", nil)
	ln, err := srv.Listen()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	port := serverPort(ln.Addr().String())
	if port == 0 {
		t.Fatalf("bound listener %q has no port", ln.Addr().String())
	}

	client := &http.Client{Timeout: 2 * time.Second}
	get := func(host string) int {
		url := fmt.Sprintf("http://%s:%d/api/health", host, port)
		resp, err := client.Get(url)
		if err != nil {
			return -1
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := get("127.0.0.1"); code != http.StatusOK {
		t.Fatalf("loopback GET /api/health = %d, want 200", code)
	}

	if lan := network.GetIP(); lan != "" && lan != "localhost" {
		if code := get(lan); code != http.StatusOK {
			t.Fatalf("LAN GET /api/health via %s = %d, want 200 (listener must bind all interfaces, not loopback)", lan, code)
		}
	} else {
		t.Skipf("no LAN IP available (GetIP=%q), loopback check already passed", lan)
	}
}
