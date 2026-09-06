package remote

import "testing"

// TestConnectWebRequiresResolvedPath mirrors Connect's own internal-error
// guard — cheap to test without any transport at all.
func TestConnectWebRequiresResolvedPath(t *testing.T) {
	err := ConnectWeb(ConnectOptions{Target: Target{Kind: KindSSH, Host: "h"}})
	if err == nil {
		t.Fatal("expected error for empty Path")
	}
}
