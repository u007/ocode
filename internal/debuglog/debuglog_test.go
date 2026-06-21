package debuglog

import "testing"

func TestKindDiscoveryExists(t *testing.T) {
	Log.Clear()
	Log.Append(Entry{Kind: KindDiscovery, Message: "rank: 3/12 attached"})
	snap := Log.Snapshot()
	found := false
	for _, e := range snap {
		if e.Kind == KindDiscovery && e.Message == "rank: 3/12 attached" {
			found = true
		}
	}
	if !found {
		t.Fatal("KindDiscovery entry must round-trip through the log")
	}
	if KindDiscovery != "DISCOVERY" {
		t.Fatalf("KindDiscovery value = %q", KindDiscovery)
	}
}

// TestUserFacingFlagRoundTrips ensures the UserFacing boolean survives a
// snapshot — the TUI's chat-transcript promoter depends on seeing the
// original flag, not a zeroed copy.
func TestUserFacingFlagRoundTrips(t *testing.T) {
	Log.Clear()
	Log.Append(Entry{Kind: KindDiscovery, Message: "downloading llama-server …", UserFacing: true})
	Log.Append(Entry{Kind: KindDiscovery, Message: "internal warm log", UserFacing: false})
	snap := Log.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(snap))
	}
	if !snap[0].UserFacing {
		t.Fatal("first entry should be user-facing")
	}
	if snap[1].UserFacing {
		t.Fatal("second entry should NOT be user-facing")
	}
}
